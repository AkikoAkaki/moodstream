package redis

// luaFetchAndHold atomically moves ready tasks from pending ZSet to running Hash.
const luaFetchAndHold = `
local pending_key = KEYS[1]
local running_key = KEYS[2]
local max_score = ARGV[1]
local limit = ARGV[2]
local now = ARGV[3]

local raw_tasks = redis.call('ZRANGEBYSCORE', pending_key, 0, max_score, 'LIMIT', 0, limit)

if #raw_tasks == 0 then
	return {}
end

for _, raw_json in ipairs(raw_tasks) do
	local task = cjson.decode(raw_json)
	local id = task.id
	local running_data = cjson.encode({start = tonumber(now), task = task})

	redis.call('ZREM', pending_key, raw_json)
	redis.call('HSET', running_key, id, running_data)
end

return raw_tasks
`

// luaEnqueueWithIdempotency enqueues a task with two dedup layers:
// 1) idempotency_key dedup (optional)
// 2) task id dedup via SETNX for caller-provided custom IDs
const luaEnqueueWithIdempotency = `
local pending_key = KEYS[1]
local idempotency_prefix = KEYS[2]
local task_id_prefix = KEYS[3]

local task_json = ARGV[1]
local score = tonumber(ARGV[2])
local task_id = ARGV[3]
local idempotency_key = ARGV[4]
local ttl = tonumber(ARGV[5])

if idempotency_key ~= "" then
	local idempotency_redis_key = idempotency_prefix .. idempotency_key
	local existing_id = redis.call('GET', idempotency_redis_key)
	if existing_id then
		return existing_id
	end
end

local task_id_key = task_id_prefix .. task_id
local reserved = redis.call('SETNX', task_id_key, '1')
if reserved == 0 then
	return task_id
end

redis.call('ZADD', pending_key, score, task_json)

if idempotency_key ~= "" then
	local idempotency_redis_key = idempotency_prefix .. idempotency_key
	redis.call('SET', idempotency_redis_key, task_id, 'EX', ttl)
end

return task_id
`

// luaAck removes a running task and releases its task-id reservation.
const luaAck = `
local running_key = KEYS[1]
local task_id = ARGV[1]
local task_id_prefix = ARGV[2]

local removed = redis.call('HDEL', running_key, task_id)
if removed == 1 then
	redis.call('DEL', task_id_prefix .. task_id)
end
return removed
`

// luaNack retries or dead-letters a task. Reservation is released only for dead-lettered tasks.
const luaNack = `
local running_key = KEYS[1]
local pending_key = KEYS[2]
local dlq_key = KEYS[3]

local id = ARGV[1]
local task_json = ARGV[2]
local score = ARGV[3]
local is_dead = tonumber(ARGV[4])
local task_id_prefix = ARGV[5]

redis.call('HDEL', running_key, id)

if is_dead == 1 then
	redis.call('LPUSH', dlq_key, task_json)
	redis.call('DEL', task_id_prefix .. id)
else
	redis.call('ZADD', pending_key, score, task_json)
end

return 1
`

// luaDelete atomically removes a task from pending/running and releases task-id reservation.
const luaDelete = `
local pending_key = KEYS[1]
local running_key = KEYS[2]
local task_id = ARGV[1]
local task_id_prefix = ARGV[2]

local deleted = 0
local all_tasks = redis.call('ZRANGE', pending_key, 0, -1)
for _, task_json in ipairs(all_tasks) do
	local task = cjson.decode(task_json)
	if task.id == task_id then
		redis.call('ZREM', pending_key, task_json)
		deleted = 1
		break
	end
end

local removed = redis.call('HDEL', running_key, task_id)
if removed == 1 then
	deleted = 1
end

if deleted == 1 then
	redis.call('DEL', task_id_prefix .. task_id)
end

return deleted
`

// luaRecover requeues or dead-letters expired running tasks.
const luaRecover = `
local running_key = KEYS[1]
local pending_key = KEYS[2]
local dlq_key = KEYS[3]
local task_id_prefix = KEYS[4]
local now = tonumber(ARGV[1])
local timeout = tonumber(ARGV[2])
local max_retries = tonumber(ARGV[3])

local all_running = redis.call('HGETALL', running_key)
for i = 1, #all_running, 2 do
	local id = all_running[i]
	local val_str = all_running[i+1]
	local entry = cjson.decode(val_str)
	local start_time = tonumber(entry.start)
	local task = entry.task

	if (now - start_time) >= timeout then
		task.retry_count = (task.retry_count or 0) + 1
		local task_json = cjson.encode(task)

		redis.call('HDEL', running_key, id)

		if task.retry_count >= max_retries then
			redis.call('LPUSH', dlq_key, task_json)
			redis.call('DEL', task_id_prefix .. id)
		else
			redis.call('ZADD', pending_key, now, task_json)
		end
	end
end

return 1
`

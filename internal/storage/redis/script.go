package redis

// luaPeekAndRem 实现了分布式延时队列的“消费并删除”原子操作。
// @Logic
// 1. ZRANGEBYSCORE: 基于当前系统时间戳，在有序集合(ZSet)中检索所有已到期的任务 ID。
// 2. ZREM: 同步从 ZSet 中剔除上述命中的任务，防止任务被并发节点重复拉取。
// 3. Return: 将命中的任务 ID 列表返回给调用方进行后续的业务处理。
//
// @Constraints
// - 原子性保障：通过 Lua 脚本执行，确保读取与删除之间不被其他命令插入。
// - 性能限制：调用方需合理控制 ARGV[2] (limit)，避免大批量删除导致 Redis 阻塞。
//
// @Parameters
// KEYS[1] - string: 延时队列的 ZSet 键名 (e.g., "dq_tasks_zset")
// ARGV[1] - int64 : 当前 Unix 时间戳 (Score)，用于判定任务是否到期
// ARGV[2] - int   : 单词拉取的最大任务数量 (Limit)，用于流量削峰
//
// @Returns
// table: 返回包含任务 Payload (ID) 的数组；若无到期任务则返回空 Table。
const luaFetchAndHold = `
local pending_key = KEYS[1]
local running_key = KEYS[2]
local max_score = ARGV[1]
local limit = ARGV[2]
local now = ARGV[3]

-- 1. 检索所有 Score 小于等于当前时间戳的任务
local raw_tasks = redis.call('ZRANGEBYSCORE', pending_key, 0, max_score, 'LIMIT', 0, limit)

if #raw_tasks > 0 then
    for i, raw_json in ipairs(raw_tasks) do
        -- 2. 解析 TaskID (Redis 内置 cjson 库)
        -- 注意：这里假设 raw_json 是合法的 JSON 字符串
        local task = cjson.decode(raw_json)
        local id = task.id

        -- 3. 从 Pending 移除
        redis.call('ZREM', pending_key, raw_json)

        -- 4. 构造 Running 记录 (包装一下，记录开始时间)
        -- 格式: {"start": 1700000000, "task": {...}}
        local running_data = cjson.encode({start = tonumber(now), task = task})
        
        -- 5. 写入 Running Hash
        redis.call('HSET', running_key, id, running_data)
    end
    return raw_tasks
else
    return {}
end
`

// luaEnqueueWithIdempotency 支持幂等性的任务入队操作
// @Logic
// 1. 如果提供了 idempotency_key，先检查是否已存在
// 2. 如果存在，直接返回已有的 task_id（幂等）
// 3. 如果不存在，创建新任务并保存幂等性映射
//
// @Parameters
// KEYS[1]: Pending ZSet (ddq:tasks)
// KEYS[2]: Idempotency key prefix (ddq:idempotency:)
// ARGV[1]: Task JSON
// ARGV[2]: Execute time (score)
// ARGV[3]: Task ID
// ARGV[4]: Idempotency key (空字符串表示不启用幂等性)
// ARGV[5]: TTL for idempotency key (seconds, e.g., 86400 for 24 hours)
//
// @Returns
// string: task_id (新创建的或已存在的)
const luaEnqueueWithIdempotency = `
local pending_key = KEYS[1]
local idempotency_prefix = KEYS[2]
local task_json = ARGV[1]
local score = tonumber(ARGV[2])
local task_id = ARGV[3]
local idempotency_key = ARGV[4]
local ttl = tonumber(ARGV[5])

-- 1. 如果提供了幂等性 key，先检查是否已存在
if idempotency_key ~= "" then
    local idempotency_redis_key = idempotency_prefix .. idempotency_key
    local existing_id = redis.call('GET', idempotency_redis_key)
    
    if existing_id then
        -- 任务已存在，直接返回已有的 task_id（幂等）
        return existing_id
    end
end

-- 2. 任务不存在，创建新任务
redis.call('ZADD', pending_key, score, task_json)

-- 3. 如果提供了幂等性 key，保存映射关系
if idempotency_key ~= "" then
    local idempotency_redis_key = idempotency_prefix .. idempotency_key
    redis.call('SET', idempotency_redis_key, task_id, 'EX', ttl)
end

return task_id
`

// luaAck 确认任务完成
// KEYS[1]: Running Hash (ddq:running)
// ARGV[1]: TaskID
const luaAck = `
return redis.call('HDEL', KEYS[1], ARGV[1])
`

// luaNack 任务失败重试
// @Logic
// 1. 从 Running 移除
// 2. 判断是否超过最大重试次数
// 3. 没超过 -> 更新 retry_count -> ZADD 回 Pending
// 4. 超过了 -> LPUSH 到 DLQ (死信队列)
//
// @Parameters
// KEYS[1]: Running Hash (ddq:running)
// KEYS[2]: Pending ZSet (ddq:tasks)
// KEYS[3]: Dead Letter Queue (ddq:dlq)
// ARGV[1]: TaskID
// ARGV[2]: JSON Payload (包含更新后的 retry_count 的完整 task 结构)
// ARGV[3]: Next Execute Time (重试的执行时间，通常是现在)
// ARGV[4]: Is Dead (1=进死信, 0=重试)
const luaNack = `
local running_key = KEYS[1]
local pending_key = KEYS[2]
local dlq_key = KEYS[3]

local id = ARGV[1]
local task_json = ARGV[2]
local score = ARGV[3]
local is_dead = tonumber(ARGV[4])

-- 1. 无论如何，先从正在运行列表移除
redis.call('HDEL', running_key, id)

if is_dead == 1 then
    -- 2. 超过重试次数，进死信队列
    redis.call('LPUSH', dlq_key, task_json)
else
    -- 3. 没超过，放回等待队列重试
    redis.call('ZADD', pending_key, score, task_json)
end

return 1
`

// luaDelete 删除任务（支持从 Pending 或 Running 删除）
// @Logic
// 1. 扫描 Pending ZSet，找到匹配 ID 的任务并删除
// 2. 如果在 Running Hash 中，也删除
// 3. 返回删除的任务数量（0或1）
//
// @Parameters
// KEYS[1]: Pending ZSet (ddq:tasks)
// KEYS[2]: Running Hash (ddq:running)
// ARGV[1]: TaskID
//
// @Returns
// int: 删除的任务数量（0=任务不存在，1=成功删除）
const luaDelete = `
local pending_key = KEYS[1]
local running_key = KEYS[2]
local task_id = ARGV[1]

local deleted = 0

-- 1. 从 Pending 中查找并删除
-- 由于 ZSet 的成员是 JSON 字符串，我们需要遍历找到包含该 ID 的元素
local all_tasks = redis.call('ZRANGE', pending_key, 0, -1)
for i, task_json in ipairs(all_tasks) do
    local task = cjson.decode(task_json)
    if task.id == task_id then
        redis.call('ZREM', pending_key, task_json)
        deleted = 1
        break
    end
end

-- 2. 从 Running 中删除（如果存在）
local removed = redis.call('HDEL', running_key, task_id)
if removed == 1 then
    deleted = 1
end

return deleted
`


// luaRecover 扫描并恢复超时任务
// 逻辑：
// 1. 获取所有 Running 任务 (HGETALL)
// 2. 遍历检查：如果 (now - start_time) > visibility_timeout
// 3. 执行 NACK 逻辑 (retry++ -> ZADD/LPUSH -> HDEL)
//
// KEYS[1]: Running Hash
// KEYS[2]: Pending ZSet
// KEYS[3]: Dead Letter Queue
// ARGV[1]: Now Timestamp
// ARGV[2]: Visibility Timeout
// ARGV[3]: Max Retries
const luaRecover = `
local running_key = KEYS[1]
local pending_key = KEYS[2]
local dlq_key = KEYS[3]
local now = tonumber(ARGV[1])
local timeout = tonumber(ARGV[2])
local max_retries = tonumber(ARGV[3])

-- 1. 获取所有正在运行的任务 (注意：生产环境若 Hash 巨大，应用 HSCAN 代替)
local all_running = redis.call('HGETALL', running_key)

-- HGETALL 返回的是 [key1, val1, key2, val2...] 的数组
for i = 1, #all_running, 2 do
    local id = all_running[i]
    local val_str = all_running[i+1]
    
    local entry = cjson.decode(val_str)
    local start_time = tonumber(entry.start)
    local task = entry.task

    -- 2. 检查是否超时
    if (now - start_time) > timeout then
        -- 3. 超时了！执行恢复逻辑
        
        -- a. 更新元数据
        task.retry_count = (task.retry_count or 0) + 1
        -- 为了存储，重新 encode task
        local task_json = cjson.encode(task)

        -- b. 从 Running 移除
        redis.call('HDEL', running_key, id)

        -- c. 判断去向
        if task.retry_count >= max_retries then
            -- 进死信
            redis.call('LPUSH', dlq_key, task_json)
        else
            -- 重新进队列 (立即重试，Score = Now)
            redis.call('ZADD', pending_key, now, task_json)
        end
    end
end

return 1
`

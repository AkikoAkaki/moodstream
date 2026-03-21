package redis

// luaFetchWindow atomically fetches and removes all events in [fromMs, toMs].
// Returns the raw JSON members that were removed.
// ZREMRANGEBYSCORE on an empty range is a safe no-op in Redis.
const luaFetchWindow = `
local key     = KEYS[1]
local from_ms = ARGV[1]
local to_ms   = ARGV[2]
local events  = redis.call('ZRANGEBYSCORE', key, from_ms, to_ms)
redis.call('ZREMRANGEBYSCORE', key, from_ms, to_ms)
return events
`

package common

import "context"

// UserQuotaDeltaScript applies a signed delta to the cached user quota hash,
// guarded by id and CacheSchema so stale or mismatched hashes are skipped.
const UserQuotaDeltaScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[3])
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
redis.call('HINCRBY', KEYS[1], 'Quota', tonumber(ARGV[1]))
return 1`

// ApplyUserQuotaDeltaInCache atomically applies a signed delta to the cached
// user quota hash. Returns true when the cache was updated, false when the
// cache entry was missing/stale.
func ApplyUserQuotaDeltaInCache(userID int, delta int64, cacheKey string, schemaVersion int) (bool, error) {
	if !RedisEnabled {
		return false, nil
	}
	result, err := RDB.Eval(context.Background(), UserQuotaDeltaScript,
		[]string{cacheKey}, delta, userID, schemaVersion).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

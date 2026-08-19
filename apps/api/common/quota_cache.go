package common

import (
	"context"
	"fmt"
)

// CacheQuotaResult 表示 Redis 配额脚本的返回值。
type CacheQuotaResult int

const (
	CacheQuotaInsufficient CacheQuotaResult = iota
	CacheQuotaOK
	CacheQuotaMiss
)

// 用户缓存 schema 版本号，与 user_cache.go 中的定义保持一致；调整后必须
// 清空历史缓存，否则旧版本哈希仍会命中 Lua 守卫并返回 miss。
const UserCacheSchemaVersion = 2

// GetUserCacheKey 返回用户缓存的 Redis 键名。
func GetUserCacheKey(userId int) string {
	return fmt.Sprintf("user:%d", userId)
}

// GetTokenCacheKey 返回 token 缓存的 Redis 键名。token key 通过 HMAC 摘要
// 防止明文泄露到 Redis 监控/备份中。
func GetTokenCacheKey(key string) string {
	return fmt.Sprintf("token:%s", GenerateHMAC(key))
}

const userQuotaReserveScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[3])
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
local quota = tonumber(redis.call('HGET', KEYS[1], 'Quota'))
if quota == nil or quota < tonumber(ARGV[1]) then
  return 0
end
redis.call('HINCRBY', KEYS[1], 'Quota', -tonumber(ARGV[1]))
return 1`

const userQuotaDeltaScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[3])
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
redis.call('HINCRBY', KEYS[1], 'Quota', tonumber(ARGV[1]))
return 1`

const tokenQuotaReserveScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or redis.call('HEXISTS', KEYS[1], 'RemainQuota') == 0
  or redis.call('HEXISTS', KEYS[1], 'UsedQuota') == 0 then
  return -1
end
local remain = tonumber(redis.call('HGET', KEYS[1], 'RemainQuota'))
if remain == nil or remain < tonumber(ARGV[1]) then
  return 0
end
redis.call('HINCRBY', KEYS[1], 'RemainQuota', -tonumber(ARGV[1]))
redis.call('HINCRBY', KEYS[1], 'UsedQuota', tonumber(ARGV[1]))
redis.call('HSET', KEYS[1], 'AccessedTime', ARGV[3])
return 1`

const tokenQuotaDeltaScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or redis.call('HEXISTS', KEYS[1], 'RemainQuota') == 0
  or redis.call('HEXISTS', KEYS[1], 'UsedQuota') == 0 then
  return -1
end
redis.call('HINCRBY', KEYS[1], 'RemainQuota', tonumber(ARGV[1]))
redis.call('HINCRBY', KEYS[1], 'UsedQuota', -tonumber(ARGV[1]))
redis.call('HSET', KEYS[1], 'AccessedTime', ARGV[3])
return 1`

func quotaResultFromLua(result int, err error) (CacheQuotaResult, error) {
	if err != nil {
		return CacheQuotaMiss, err
	}
	switch result {
	case 1:
		return CacheQuotaOK, nil
	case 0:
		return CacheQuotaInsufficient, nil
	default:
		return CacheQuotaMiss, nil
	}
}

// CacheTryReserveUserQuota 原子地校验并扣减用户配额。
func CacheTryReserveUserQuota(userID int, amount int64) (CacheQuotaResult, error) {
	result, err := RDB.Eval(context.Background(), userQuotaReserveScript,
		[]string{GetUserCacheKey(userID)}, amount, userID, UserCacheSchemaVersion).Int()
	return quotaResultFromLua(result, err)
}

// CacheApplyUserQuotaDelta 原子地把一个（可正可负）增量应用到用户配额缓存。
// 返回值仅供调用方决策是否需要补偿；不要把它当作扣减结果。
func CacheApplyUserQuotaDelta(userID int, delta int64) (CacheQuotaResult, error) {
	result, err := RDB.Eval(context.Background(), userQuotaDeltaScript,
		[]string{GetUserCacheKey(userID)}, delta, userID, UserCacheSchemaVersion).Int()
	return quotaResultFromLua(result, err)
}

// CacheTryReserveTokenQuota 原子地校验并扣减 token 配额。
func CacheTryReserveTokenQuota(id int, key string, amount int64) (CacheQuotaResult, error) {
	result, err := RDB.Eval(context.Background(), tokenQuotaReserveScript,
		[]string{GetTokenCacheKey(key)}, amount, id, GetTimestamp()).Int()
	return quotaResultFromLua(result, err)
}

// CacheApplyTokenQuotaDelta 原子地把增量应用到 token 配额缓存。
func CacheApplyTokenQuotaDelta(id int, key string, delta int64) (CacheQuotaResult, error) {
	result, err := RDB.Eval(context.Background(), tokenQuotaDeltaScript,
		[]string{GetTokenCacheKey(key)}, delta, id, GetTimestamp()).Int()
	return quotaResultFromLua(result, err)
}

// CacheIncrUserQuota 原子地给用户配额加 delta（可正可负）。哈希不存在时跳过，
// 避免像裸 HINCRBY 那样创建只含 Quota 字段的残缺哈希。
func CacheIncrUserQuota(userId int, delta int64) error {
	if !RedisEnabled {
		return nil
	}
	_, err := CacheApplyUserQuotaDelta(userId, delta)
	return err
}

// CacheDecrUserQuota 等价于 CacheIncrUserQuota(userId, -delta)。
func CacheDecrUserQuota(userId int, delta int64) error {
	return CacheIncrUserQuota(userId, -delta)
}

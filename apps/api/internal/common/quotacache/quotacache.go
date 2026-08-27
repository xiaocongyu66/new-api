// Package quotacache owns the Redis representation of user and token quota:
// the cache keys, the schema version, and the guarded Lua scripts that mutate
// balances atomically.
//
// It exists as a leaf package because both the identity domain (which owns the
// user and token records) and the billing domain (which reserves and settles
// against them) need these primitives. Keeping them in one place lets both
// import downwards instead of reaching into each other.
//
// Everything here takes primitive arguments — ids, keys, deltas — and never the
// User or Token structs. That is deliberate: the moment this package needed a
// record type it would have to import the domain that owns it, and the shared
// leaf would become a cycle.
package quotacache

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/internal/common"
)

// UserSchemaVersion is stamped into the cached user hash. Bump it whenever the
// cached field set changes so stale hashes written by an older build are
// treated as a miss rather than being read with missing fields.
const UserSchemaVersion = 2

// Result reports what a guarded quota script did.
type Result int

const (
	// Insufficient means the cached balance was present but too low.
	Insufficient Result = iota
	// OK means the cached balance was present and the delta was applied.
	OK
	// Miss means no usable cache entry existed; the caller must fall back to
	// the database, which will rehydrate the cache on the next read.
	Miss
)

// UserKey returns the Redis key holding a user's cached fields.
func UserKey(userID int) string {
	return fmt.Sprintf("user:%d", userID)
}

// TokenKey returns the Redis key holding a token's cached fields. The raw API
// key is HMAC'd so it never appears in Redis.
func TokenKey(key string) string {
	return fmt.Sprintf("token:%s", common.GenerateHMAC(key))
}

// Each script is guarded: it verifies the hash belongs to the expected id (and
// schema, for users) and that the balance field exists before touching it. A
// bare HINCRBY would otherwise create a partial hash containing only the quota
// field, which later reads would mistake for a valid cache entry.

const userReserveScript = `
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

const userDeltaScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[3])
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
redis.call('HINCRBY', KEYS[1], 'Quota', tonumber(ARGV[1]))
return 1`

const tokenReserveScript = `
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

const tokenDeltaScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or redis.call('HEXISTS', KEYS[1], 'RemainQuota') == 0
  or redis.call('HEXISTS', KEYS[1], 'UsedQuota') == 0 then
  return -1
end
redis.call('HINCRBY', KEYS[1], 'RemainQuota', tonumber(ARGV[1]))
redis.call('HINCRBY', KEYS[1], 'UsedQuota', -tonumber(ARGV[1]))
redis.call('HSET', KEYS[1], 'AccessedTime', ARGV[3])
return 1`

func resultFromLua(result int, err error) (Result, error) {
	if err != nil {
		return Miss, err
	}
	switch result {
	case 1:
		return OK, nil
	case 0:
		return Insufficient, nil
	default:
		return Miss, nil
	}
}

// TryReserveUser deducts amount from the user's cached balance, refusing if the
// cached balance is lower than amount.
func TryReserveUser(userID int, amount int64) (Result, error) {
	result, err := common.RDB.Eval(context.Background(), userReserveScript,
		[]string{UserKey(userID)}, amount, userID, UserSchemaVersion).Int()
	return resultFromLua(result, err)
}

// ApplyUserDelta adds delta (which may be negative) to the user's cached
// balance without a sufficiency check.
func ApplyUserDelta(userID int, delta int64) (Result, error) {
	result, err := common.RDB.Eval(context.Background(), userDeltaScript,
		[]string{UserKey(userID)}, delta, userID, UserSchemaVersion).Int()
	return resultFromLua(result, err)
}

// IncrUser adds delta to the cached balance, doing nothing when Redis is off.
func IncrUser(userID int, delta int64) error {
	if !common.RedisEnabled {
		return nil
	}
	_, err := ApplyUserDelta(userID, delta)
	return err
}

// DecrUser subtracts delta from the cached balance.
func DecrUser(userID int, delta int64) error {
	return IncrUser(userID, -delta)
}

// SyncCredit folds a committed credit (top-up, redemption) into the cached
// balance. Pre-consumption reads the cached value while it exists, so a credit
// that skipped the cache would be invisible until the entry expired. A miss
// needs no handling: the next read rehydrates from the committed row.
func SyncCredit(userID int, quota int, operation string) {
	if quota <= 0 {
		return
	}
	if err := IncrUser(userID, int64(quota)); err != nil {
		common.SysLog(fmt.Sprintf("failed to sync %s credit to user quota cache: %s", operation, err.Error()))
	}
}

// TryReserveToken deducts amount from the token's cached remaining quota,
// refusing if it is lower than amount.
func TryReserveToken(id int, key string, amount int64) (Result, error) {
	result, err := common.RDB.Eval(context.Background(), tokenReserveScript,
		[]string{TokenKey(key)}, amount, id, common.GetTimestamp()).Int()
	return resultFromLua(result, err)
}

// ApplyTokenDelta adds delta (which may be negative) to the token's cached
// remaining quota, moving the same amount the other way on used quota.
func ApplyTokenDelta(id int, key string, delta int64) (Result, error) {
	result, err := common.RDB.Eval(context.Background(), tokenDeltaScript,
		[]string{TokenKey(key)}, delta, id, common.GetTimestamp()).Int()
	return resultFromLua(result, err)
}

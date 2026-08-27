package dbx

import (
	"errors"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/internal/common"
	"gorm.io/gorm"
)

// Batch-update queue kinds. Quota and request-count deltas are accumulated in
// memory and flushed periodically instead of writing a row per request.
const (
	BatchUpdateTypeUserQuota = iota
	BatchUpdateTypeTokenQuota
	BatchUpdateTypeUsedQuota
	BatchUpdateTypeChannelUsedQuota
	BatchUpdateTypeRequestCount
	BatchUpdateTypeCount // adding a kind requires a new map and lock below
)

var (
	batchUpdateStores []map[int]int
	batchUpdateLocks  []sync.Mutex
)

func init() {
	for range BatchUpdateTypeCount {
		batchUpdateStores = append(batchUpdateStores, make(map[int]int))
		batchUpdateLocks = append(batchUpdateLocks, sync.Mutex{})
	}
}

// AddNewRecord accumulates a delta for one id in the given queue.
//
// The queue lives here rather than beside the records because both the identity
// records (user and token quota) and the catalog records (channel used quota)
// enqueue into it, while the flush step needs all of them. Enqueue is pure
// bookkeeping over ints, so it carries no record dependency.
func AddNewRecord(kind int, id int, value int) {
	batchUpdateLocks[kind].Lock()
	defer batchUpdateLocks[kind].Unlock()
	batchUpdateStores[kind][id] += value
}

// DrainBatchQueues atomically takes every queued delta and resets the queues,
// returning one map per kind indexed by BatchUpdateType*. The caller performs the
// writes, which is where the record types live.
func DrainBatchQueues() []map[int]int {
	stores := make([]map[int]int, BatchUpdateTypeCount)
	for i := range BatchUpdateTypeCount {
		batchUpdateLocks[i].Lock()
		stores[i] = batchUpdateStores[i]
		batchUpdateStores[i] = make(map[int]int)
		batchUpdateLocks[i].Unlock()
	}
	return stores
}

// BatchQueuesPending reports whether any queue holds work, so the flusher can
// skip logging an empty pass.
func BatchQueuesPending() bool {
	for i := range BatchUpdateTypeCount {
		batchUpdateLocks[i].Lock()
		pending := len(batchUpdateStores[i]) > 0
		batchUpdateLocks[i].Unlock()
		if pending {
			return true
		}
	}
	return false
}

// ShouldUpdateRedis reports whether a value just read from the database should be
// written back to the cache.
func ShouldUpdateRedis(fromDB bool, err error) bool {
	return common.RedisEnabled && fromDB && err == nil
}

// RecordExist folds GORM's not-found sentinel into a boolean, keeping real
// errors distinct from a legitimate miss.
func RecordExist(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, err
}

// SanitizeLikePattern validates and escapes a user-supplied LIKE search pattern.
//
//  1. escapes ! and _ using ! as the ESCAPE character, which behaves the same on
//     MySQL, PostgreSQL and SQLite (backslash would collide with MySQL string
//     escaping)
//  2. rejects consecutive %
//  3. allows at most two %
//  4. requires at least 2 non-% characters when % is present
//  5. treats a pattern without % as an exact match
func SanitizeLikePattern(input string) (string, error) {
	input = strings.ReplaceAll(input, "!", "!!")
	input = strings.ReplaceAll(input, `_`, `!_`)

	if err := ValidateLikePattern(input); err != nil {
		return "", err
	}
	return input, nil
}

// ValidateLikePattern enforces the wildcard rules without escaping, for callers
// that build the pattern themselves.
func ValidateLikePattern(input string) error {
	if strings.Contains(input, "%%") {
		return errors.New("搜索模式中不允许包含连续的 % 通配符")
	}

	count := strings.Count(input, "%")
	if count > 2 {
		return errors.New("搜索模式中最多允许包含 2 个 % 通配符")
	}

	if count > 0 {
		stripped := strings.ReplaceAll(input, "%", "")
		if len(stripped) < 2 {
			return errors.New("使用模糊搜索时，关键词长度至少为 2 个字符")
		}
	}

	return nil
}

package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/samber/hot"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/cachex"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// Response-cache TTL tiers. A cached response never outlives its tier: the cache
// absorbs repeated identical reads of a global aggregate, it does not serve
// stale data past a few seconds.
const (
	CacheTTLShort  = 5 * time.Second
	CacheTTLMedium = 10 * time.Second
	CacheTTLLong   = 15 * time.Second
)

// readonlyCacheMaxBody bounds what may be stored. A larger response is served
// but never cached, so one runaway payload cannot evict the rest.
const readonlyCacheMaxBody = 2 << 20

// readonlyCacheEntry is the replayable form of a cached response.
type readonlyCacheEntry struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	Body        string `json:"body"`
}

// ReadOnlyCacheConfig describes one cached endpoint.
type ReadOnlyCacheConfig struct {
	// Namespace isolates this endpoint's keys; use one value per endpoint.
	Namespace string
	// TTL is the staleness bound. Use the CacheTTL* constants.
	TTL time.Duration
}

var (
	readonlyResponseCacheOnce sync.Once
	readonlyResponseCache     *cachex.HybridCache[readonlyCacheEntry]
)

// getReadonlyResponseCache builds the cache lazily: common.RDB is assigned
// during startup, after package initialization, so a cache constructed at init
// time would pin a nil client and silently stay memory-only.
func getReadonlyResponseCache() *cachex.HybridCache[readonlyCacheEntry] {
	readonlyResponseCacheOnce.Do(func() {
		readonlyResponseCache = cachex.NewHybridCache[readonlyCacheEntry](cachex.HybridCacheConfig[readonlyCacheEntry]{
			Namespace:    cachex.Namespace("http_readonly:v1"),
			Redis:        common.RDB,
			RedisEnabled: func() bool { return common.RedisEnabled && common.RDB != nil },
			RedisCodec:   cachex.JSONCodec[readonlyCacheEntry]{},
			Memory: func() *hot.HotCache[string, readonlyCacheEntry] {
				return hot.NewHotCache[string, readonlyCacheEntry](hot.LRU, 4096).
					WithTTL(CacheTTLLong).
					WithJanitor().
					Build()
			},
		})
	})
	return readonlyResponseCache
}

// ReadOnlyCache caches the response of a global, read-only GET endpoint.
//
// Mount it AFTER authentication: a hit replays whatever the first caller
// received, so an endpoint whose output depends on the caller must never be
// mounted here. Only GET is served from or stored into the cache; every other
// method passes through untouched, and only a 200 response is stored.
//
// Streamed responses are never cached. Server-Sent Events and raw response
// streams have no buffered body, so CaptureResponse reports empty and the entry
// is skipped. This middleware must not be mounted on a relay or streamed route.
func ReadOnlyCache(cfg ReadOnlyCacheConfig) contract.Middleware {
	if cfg.TTL <= 0 {
		cfg.TTL = CacheTTLMedium
	}
	return func(c contract.Context) {
		if c.Method() != http.MethodGet {
			c.Next()
			return
		}

		key := cfg.Namespace + ":" + c.RequestURI()
		cache := getReadonlyResponseCache()
		if entry, found, err := cache.Get(key); err == nil && found {
			// Abort before writing: the chain loop keeps advancing after a
			// handler returns, so a hit that only wrote and returned would
			// still run the downstream handler and append its body.
			c.Abort()
			_ = c.Data(entry.Status, entry.ContentType, []byte(entry.Body))
			return
		}

		capture := c.CaptureResponse(readonlyCacheMaxBody)
		c.Next()
		if capture == nil || c.ResponseStatus() != http.StatusOK {
			return
		}
		body := capture.Body()
		if len(body) == 0 || len(body) > readonlyCacheMaxBody {
			return
		}
		_ = cache.SetWithTTL(key, readonlyCacheEntry{
			Status:      http.StatusOK,
			ContentType: "application/json; charset=utf-8",
			Body:        string(body),
		}, cfg.TTL)
	}
}

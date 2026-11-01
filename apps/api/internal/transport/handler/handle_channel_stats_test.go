package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"
	"github.com/QuantumNous/new-api/internal/usage"
)

func withChannelStatsLogDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousLogDB := dbx.LogDB
	// ":memory:" would give every pooled connection its own empty database, so
	// the aggregate SELECT could land on a connection that never saw the seeded
	// rows. A shared-cache in-memory DSN with the pool pinned to one connection
	// fixes that; a unique DSN per call keeps another test's copy of the same
	// table from leaking rows into this one.
	dsn := fmt.Sprintf("file:channel_stats_%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.Exec(`CREATE TABLE logs (
		id INTEGER PRIMARY KEY,
		type INTEGER,
		channel_id INTEGER,
		prompt_tokens INTEGER,
		completion_tokens INTEGER,
		use_time INTEGER,
		quota INTEGER,
		created_at INTEGER
	)`).Error)
	dbx.LogDB = db

	t.Cleanup(func() {
		dbx.LogDB = previousLogDB
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func seedChannelStatsLogsHandler(t *testing.T, db *gorm.DB, now int64) {
	t.Helper()
	require.NoError(t, db.Exec(
		`INSERT INTO logs (type, channel_id, prompt_tokens, completion_tokens, use_time, quota, created_at) VALUES
		 (2, 1, 100, 50, 2, 10, ?),
		 (2, 1, 200, 300, 4, 20, ?),
		 (5, 1, 0, 0, 0, 5, ?),
		 (5, 2, 0, 0, 0, 3, ?),
		 (5, 2, 0, 0, 0, 4, ?),
		 (2, 1, 10, 10, 1, 1, ?)`,
		now-100, now-200, now-300, now-400, now-500, now-200000,
	).Error)
}

func TestGetChannelRequestStatsHandler(t *testing.T) {
	db := withChannelStatsLogDB(t)
	// Seed immediately before the subtests run: the handler derives its window
	// from its own time.Now(), so any gap between seeding and the request can
	// push the newest rows outside the window.
	seedChannelStatsLogsHandler(t, db, time.Now().Unix())

	// Other tests in this package run first and may have registered the real
	// display-currency hook; pin it off so quota assertions check the raw sum.
	previousHook := usage.OnQuotaToDisplayAmount
	usage.OnQuotaToDisplayAmount = nil
	defer func() { usage.OnQuotaToDisplayAmount = previousHook }()

	doRequest := func(query string) *httptest.ResponseRecorder {
		ctx, rec := fiberadapter.NewSyntheticContext(
			httptest.NewRequest(http.MethodGet, "/api/channel/request_stats?"+query, nil))
		GetChannelRequestStats(ctx)
		return rec
	}

	unmarshal := func(t *testing.T, rec *httptest.ResponseRecorder) (int, map[string]struct {
		RequestCount  int64   `json:"request_count"`
		SuccessCount  int64   `json:"success_count"`
		ErrorCount    int64   `json:"error_count"`
		Tokens        int64   `json:"tokens"`
		AvgDurationMs int64   `json:"avg_duration_ms"`
		QuotaDisplay  float64 `json:"quota_display"`
		LastRequestAt int64   `json:"last_request_at"`
	}) {
		t.Helper()
		require.Equal(t, http.StatusOK, rec.Code)
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				Hours    int `json:"hours"`
				Channels map[string]struct {
					RequestCount  int64   `json:"request_count"`
					SuccessCount  int64   `json:"success_count"`
					ErrorCount    int64   `json:"error_count"`
					Tokens        int64   `json:"tokens"`
					AvgDurationMs int64   `json:"avg_duration_ms"`
					QuotaDisplay  float64 `json:"quota_display"`
					LastRequestAt int64   `json:"last_request_at"`
				} `json:"channels"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		assert.True(t, response.Success)
		return response.Data.Hours, response.Data.Channels
	}

	t.Run("默认 24 小时并按渠道聚合", func(t *testing.T) {
		rec := doRequest("hours=24&channel_ids=1,2")
		hours, channels := unmarshal(t, rec)
		assert.Equal(t, 24, hours)
		// Aggregate values are asserted in the usage store test; here we only
		// pin the HTTP contract: one keyed entry per requested channel.
		require.Len(t, channels, 2)
		for _, id := range []string{"1", "2"} {
			entry, ok := channels[id]
			require.True(t, ok, "channel %s missing from response", id)
			assert.NotZero(t, entry.RequestCount)
		}
	})

	t.Run("hours 被 clamp 到 [1,168]", func(t *testing.T) {
		rec := doRequest("hours=0&channel_ids=1")
		hours, _ := unmarshal(t, rec)
		assert.Equal(t, 1, hours)

		rec = doRequest("hours=1000&channel_ids=1")
		hours, _ = unmarshal(t, rec)
		assert.Equal(t, 168, hours)

		rec = doRequest("hours=notanumber&channel_ids=1")
		hours, _ = unmarshal(t, rec)
		assert.Equal(t, 24, hours)
	})

	t.Run("channel_ids 为空返回空 map", func(t *testing.T) {
		rec := doRequest("hours=24")
		require.Equal(t, http.StatusOK, rec.Code)
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				Hours    int                    `json:"hours"`
				Channels map[string]interface{} `json:"channels"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		assert.True(t, response.Success)
		assert.Equal(t, 24, response.Data.Hours)
		assert.NotNil(t, response.Data.Channels)
		assert.Empty(t, response.Data.Channels)

		rec = doRequest("hours=24&channel_ids=")
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		assert.Empty(t, response.Data.Channels)
	})

	t.Run("指定渠道过滤", func(t *testing.T) {
		// Verify the fixtures are what we think before blaming the handler:
		// channel 2 must hold exactly two error rows in a wide window.
		type rowCount struct {
			Type  int `gorm:"column:type"`
			Count int `gorm:"column:count"`
		}
		var counts []rowCount
		wideStart := time.Now().Unix() - 3*86400
		wideEnd := time.Now().Unix() + 3600
		require.NoError(t, db.Raw(
			`SELECT type, count(*) AS count FROM logs WHERE channel_id = ? AND created_at BETWEEN ? AND ? GROUP BY type`,
			2, wideStart, wideEnd,
		).Scan(&counts).Error)
		require.Len(t, counts, 1, "channel 2 should only hold error rows")
		assert.Equal(t, 5, counts[0].Type)
		require.Equal(t, 2, counts[0].Count, "fixture drift: error row count")

		rec := doRequest("channel_ids=2")
		_, channels := unmarshal(t, rec)
		assert.Len(t, channels, 1)
		ch2, ok := channels["2"]
		require.True(t, ok)
		// Channel 2 seeded two error rows and no consume rows.
		assert.Equal(t, int64(2), ch2.RequestCount)
		assert.Equal(t, int64(0), ch2.SuccessCount)
	})

	t.Run("非数字渠道 ID 拒绝", func(t *testing.T) {
		rec := doRequest("hours=24&channel_ids=1,not-an-id,3")
		// The repo's business error convention answers 200 with success:false,
		// so assert the payload rather than a 4xx status.
		assert.Equal(t, http.StatusOK, rec.Code)
		var response struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		assert.False(t, response.Success)
		assert.NotEmpty(t, response.Message)
	})
}

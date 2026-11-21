package usage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRecentSeriesSources pins that every hour is backed by a real sample or
// left empty. Filling it with 100% success and 0 ms made every model look
// perfectly healthy outside its measured hours.
func TestRecentSeriesSources(t *testing.T) {
	latest := bucketStart(time.Now().Unix())
	step := GetBucketSeconds()
	if step <= 0 {
		step = 3600
	}
	emptyTs := latest - 2*step
	probeTs := latest - step
	requestTs := latest

	points := recentSeries(
		map[int64]counters{
			requestTs: {requestCount: 2, successCount: 1, totalLatencyMs: 1800},
		},
		map[int64]ProbeBucket{
			probeTs:   {TotalCount: 3, SuccessCount: 1, TotalLatencyMs: 900},
			requestTs: {TotalCount: 5, SuccessCount: 5, TotalLatencyMs: 25},
		},
		nil, nil,
		3,
	)

	require.Len(t, points, 3)

	assert.Equal(t, emptyTs, points[0].Ts)
	assert.Equal(t, "none", points[0].Source)
	assert.Nil(t, points[0].LatencyMs)
	assert.Nil(t, points[0].SuccessRate)

	assert.Equal(t, probeTs, points[1].Ts)
	assert.Equal(t, "probe", points[1].Source)
	require.NotNil(t, points[1].LatencyMs)
	assert.Equal(t, int64(300), *points[1].LatencyMs)
	require.NotNil(t, points[1].SuccessRate)
	assert.InDelta(t, 33.33, *points[1].SuccessRate, 0.001)

	assert.Equal(t, requestTs, points[2].Ts)
	assert.Equal(t, "request", points[2].Source)
	require.NotNil(t, points[2].LatencyMs)
	assert.Equal(t, int64(900), *points[2].LatencyMs)
	require.NotNil(t, points[2].SuccessRate)
	assert.InDelta(t, 50, *points[2].SuccessRate, 0.001)
}

// TestRecentSeriesUpstreamFallback pins that an unmeasured hour shows the
// request-derived upstream rate with honest latency, never a fake 100%/0ms.
func TestRecentSeriesUpstreamFallback(t *testing.T) {
	rate := 42.0
	latency := int64(310)

	points := recentSeries(nil, nil, &rate, &latency, 2)
	require.Len(t, points, 2)
	for _, point := range points {
		assert.Equal(t, "upstream", point.Source)
		require.NotNil(t, point.SuccessRate)
		assert.InDelta(t, 42, *point.SuccessRate, 0.001)
		require.NotNil(t, point.LatencyMs)
		assert.Equal(t, int64(310), *point.LatencyMs)
	}

	points = recentSeries(nil, nil, &rate, nil, 1)
	require.Len(t, points, 1)
	assert.Equal(t, "upstream", points[0].Source)
	require.NotNil(t, points[0].SuccessRate)
	assert.Nil(t, points[0].LatencyMs)
}

func stubSummaryBuckets(t *testing.T, buckets ...PerfMetricSummaryBucket) {
	t.Helper()
	previous := QuerySummaryBucketsFn
	t.Cleanup(func() { QuerySummaryBucketsFn = previous })
	QuerySummaryBucketsFn = func(startTs, endTs int64, groups []string) ([]PerfMetricSummaryBucket, error) {
		return buckets, nil
	}
}

// TestQuerySummaryAllPoolsProbeCounts covers a model with no relay requests:
// its chart must still appear, with one rate pooled across probe samples.
func TestQuerySummaryAllPoolsProbeCounts(t *testing.T) {
	stubSummaryBuckets(t)
	latest := bucketStart(time.Now().Unix())

	result, err := QuerySummaryAll(24, nil, []ProbeBucket{
		{Model: "probe-only", BucketTs: latest - 3600, TotalCount: 4, SuccessCount: 3, TotalLatencyMs: 800},
		{Model: "probe-only", BucketTs: latest, TotalCount: 2, SuccessCount: 0, TotalLatencyMs: 400},
	}, nil)
	require.NoError(t, err)
	require.Len(t, result.Models, 1)

	model := result.Models[0]
	assert.Equal(t, "probe-only", model.ModelName)
	assert.InDelta(t, 50, model.SuccessRate, 0.001)
	require.NotNil(t, model.AvgLatencyMs)
	assert.Equal(t, int64(200), *model.AvgLatencyMs)
	assert.Equal(t, "probe", model.Source)
}

// TestQuerySummaryAllBlendsSources pins the blend: upstream health always
// participates, joined by real requests and probes when those are measured.
func TestQuerySummaryAllBlendsSources(t *testing.T) {
	now := time.Now().Unix()
	latest := bucketStart(now)

	t.Run("real, probe and upstream all count", func(t *testing.T) {
		stubSummaryBuckets(t, PerfMetricSummaryBucket{
			ModelName: "with-requests", BucketTs: latest,
			RequestCount: 2, SuccessCount: 2, TotalLatencyMs: 400,
		})
		result, err := QuerySummaryAll(24, nil, []ProbeBucket{{
			Model: "with-requests", BucketTs: latest,
			TotalCount: 2, SuccessCount: 0, TotalLatencyMs: 100,
		}}, []UpstreamHealthPoint{{Model: "with-requests", Rate: 0, ObservedAt: now}})
		require.NoError(t, err)
		require.Len(t, result.Models, 1)
		assert.Equal(t, "request+probe", result.Models[0].Source)
		// real=100, probe=0, upstream=0 → all three blended.
		assert.InDelta(t, 100.0/3.0, result.Models[0].SuccessRate, 0.01)
	})

	t.Run("real and upstream without probe", func(t *testing.T) {
		stubSummaryBuckets(t, PerfMetricSummaryBucket{
			ModelName: "with-requests", BucketTs: latest,
			RequestCount: 2, SuccessCount: 2, TotalLatencyMs: 400,
		})
		result, err := QuerySummaryAll(24, nil, nil,
			[]UpstreamHealthPoint{{Model: "with-requests", Rate: 0, ObservedAt: now}})
		require.NoError(t, err)
		require.Len(t, result.Models, 1)
		assert.Equal(t, "request", result.Models[0].Source)
		// real=100, upstream=0 → upstream participates even with no probe.
		assert.InDelta(t, 50, result.Models[0].SuccessRate, 0.01)
	})

	t.Run("probe and upstream", func(t *testing.T) {
		stubSummaryBuckets(t)
		result, err := QuerySummaryAll(24, nil, []ProbeBucket{{
			Model: "probe-only", BucketTs: latest,
			TotalCount: 2, SuccessCount: 2, TotalLatencyMs: 100,
		}}, []UpstreamHealthPoint{{Model: "probe-only", Rate: 0, ObservedAt: now}})
		require.NoError(t, err)
		require.Len(t, result.Models, 1)
		assert.Equal(t, "probe+upstream", result.Models[0].Source)
		assert.InDelta(t, 50, result.Models[0].SuccessRate, 0.001)
	})

	t.Run("stale upstream ignored", func(t *testing.T) {
		stubSummaryBuckets(t)
		result, err := QuerySummaryAll(24, nil, nil,
			[]UpstreamHealthPoint{{Model: "stale", Rate: 0, ObservedAt: now - 48*3600}})
		require.NoError(t, err)
		assert.Empty(t, result.Models, "an observation older than the window is not a signal")
	})
}

// TestUpstreamRateWeightedByStaticWeight pins Σ(rate×weight)/Σweight. Two
// channel rates 20 and 100 with weights 1 and 3 must give 80, not the plain
// mean 60; a zero-weight channel is excluded from the prior.
func TestUpstreamRateWeightedByStaticWeight(t *testing.T) {
	points := []UpstreamHealthPoint{
		{Model: "m", ChannelID: 1, Rate: 20, Weight: 1},
		{Model: "m", ChannelID: 2, Rate: 100, Weight: 3},
		{Model: "m", ChannelID: 3, Rate: 0, Weight: 0},
	}
	assert.InDelta(t, 80, UpstreamRateWeighted(points), 0.001)
	assert.InDelta(t, 20, UpstreamRateWeighted(points[:1]), 0.001)
	assert.InDelta(t, 0, UpstreamRateWeighted(nil), 0.001)
}

// TestQuerySummaryAllWeightsUpstreamChannels proves the endpoint aggregates
// multiple channels of one model by static weight rather than a plain mean.
func TestQuerySummaryAllWeightsUpstreamChannels(t *testing.T) {
	stubSummaryBuckets(t)
	now := time.Now().Unix()
	result, err := QuerySummaryAll(24, nil, nil, []UpstreamHealthPoint{
		{Model: "weighted", ChannelID: 1, Rate: 20, Weight: 1, ObservedAt: now},
		{Model: "weighted", ChannelID: 2, Rate: 100, Weight: 3, ObservedAt: now},
	})
	require.NoError(t, err)
	require.Len(t, result.Models, 1)
	assert.InDelta(t, 80, result.Models[0].SuccessRate, 0.001)
	assert.Equal(t, "upstream", result.Models[0].Source)
}

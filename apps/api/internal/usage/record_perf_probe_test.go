package usage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRecentSeriesSources pins that every hour is backed by a real sample or
// left empty. Filling it with 100% success and 0 ms made every model look
// perfectly healthy outside its measured hours. Probe hours no longer exist:
// the card is fed by real requests and weighted upstream health only.
func TestRecentSeriesSources(t *testing.T) {
	step := GetBucketSeconds()
	if step <= 0 {
		step = 3600
	}
	latest := bucketStart(time.Now().Unix())
	requestTs := latest

	points := recentSeries(
		map[int64]counters{
			requestTs: {requestCount: 2, successCount: 1, totalLatencyMs: 1800},
		},
		nil, nil, nil,
		2,
	)

	require.Len(t, points, 2)

	// Compare against the series' own anchor: the clock may tick over an hour
	// boundary between building the fixture and calling recentSeries.
	assert.Equal(t, points[1].Ts-step, points[0].Ts)
	assert.Equal(t, "none", points[0].Source)
	assert.Nil(t, points[0].LatencyMs)
	assert.Nil(t, points[0].SuccessRate)

	assert.Equal(t, requestTs, points[1].Ts)
	assert.Equal(t, "request", points[1].Source)
	require.NotNil(t, points[1].LatencyMs)
	assert.Equal(t, int64(900), *points[1].LatencyMs)
	require.NotNil(t, points[1].SuccessRate)
	assert.InDelta(t, 50, *points[1].SuccessRate, 0.001)
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

// TestQuerySummaryAllBlendsSources pins the blend: the model-square rate is
// upstream health (static-weight averaged) averaged with real request
// outcomes. Probe requests no longer exist, so a probe bucket alone must not
// outrank a measured upstream signal.
func TestQuerySummaryAllBlendsSources(t *testing.T) {
	now := time.Now().Unix()
	latest := bucketStart(now)

	t.Run("real requests averaged with upstream", func(t *testing.T) {
		stubSummaryBuckets(t, PerfMetricSummaryBucket{
			ModelName: "with-requests", BucketTs: latest,
			RequestCount: 2, SuccessCount: 2, TotalLatencyMs: 400,
		})
		result, err := QuerySummaryAll(24, nil, nil,
			[]UpstreamHealthPoint{{Model: "with-requests", Rate: 0, ObservedAt: now}})
		require.NoError(t, err)
		require.Len(t, result.Models, 1)
		assert.Equal(t, "request+upstream", result.Models[0].Source)
		// real=100, upstream=0 → plain average.
		assert.InDelta(t, 50, result.Models[0].SuccessRate, 0.01)
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

// TestAtomicBucketDropsImpossibleTtft pins the sample-validity guard: a first
// token cannot arrive after the request has already finished. Retries reuse the
// original StartTime, so an occasional sample reports TTFT > total latency;
// admitting it would make the displayed average exceed the latency average and
// look impossible to the operator.
func TestAtomicBucketDropsImpossibleTtft(t *testing.T) {
	var b atomicBucket
	b.add(Sample{LatencyMs: 2000, TtftMs: 500, HasTtft: true})
	b.add(Sample{LatencyMs: 1000, TtftMs: 5000, HasTtft: true}) // corrupt
	b.add(Sample{LatencyMs: 0, TtftMs: 800, HasTtft: true})     // unknown latency, keep

	got := b.snapshot()
	assert.Equal(t, int64(2), got.ttftCount, "the impossible sample must be dropped")
	assert.Equal(t, int64(1300), got.ttftSumMs)
}

// TestRecentSeriesProbeHoursShowVariation 钉住：探活结果必须按小时出现在
// 时间线上——失败的小时低、成功的小时高——而不是一条被继承值填平的直线。
// 每小时的成功率取该小时探活的池化比例；延迟只从带延迟的结果里平均。
func TestRecentSeriesProbeHoursShowVariation(t *testing.T) {
	latest := bucketStart(time.Now().Unix())
	step := GetBucketSeconds()
	if step <= 0 {
		step = 3600
	}
	failTs := latest - 2*step
	okTs := latest - step

	probes := map[int64]ProbeBucket{
		failTs: {TotalCount: 3, SuccessCount: 0, TotalLatencyMs: 0},
		okTs:   {TotalCount: 2, SuccessCount: 2, TotalLatencyMs: 300},
	}

	points := recentSeries(nil, probes, nil, nil, 3)
	require.Len(t, points, 3)

	byTs := map[int64]ModelRecentPoint{}
	for _, point := range points {
		byTs[point.Ts] = point
	}

	// Locate by absolute hour: recentSeries re-derives its own anchor, so an
	// hour boundary crossing between fixture and call must not break ordering
	// assumptions.
	if p, ok := byTs[failTs]; ok {
		assert.Equal(t, "upstream", p.Source, "probe hours surface as upstream evidence")
		require.NotNil(t, p.SuccessRate)
		assert.InDelta(t, 0, *p.SuccessRate, 0.001)
		assert.Nil(t, p.LatencyMs, "outcomes without latency stay a gap")
	}

	p, ok := byTs[okTs]
	require.True(t, ok, "the successful probe hour must be in the series")
	assert.Equal(t, "upstream", p.Source)
	require.NotNil(t, p.SuccessRate)
	assert.InDelta(t, 100, *p.SuccessRate, 0.001)
	require.NotNil(t, p.LatencyMs)
	assert.Equal(t, int64(150), *p.LatencyMs, "latency averages only measured outcomes")
}

package record_perf

import "github.com/QuantumNous/new-api/model"

// Persistence hooks for flushing and querying aggregated metric buckets.
// They are wired by the usage capability (see internal/usage),
// which owns the perf_metrics store; this package must not import it to
// avoid an import cycle. Callers may assume the hooks are set before the
// first flush or query: both are triggered after application startup.
var (
	UpsertMetricFn        func(metric *model.PerfMetric) error
	DeleteMetricsBeforeFn func(cutoffTs int64) error
	QueryMetricRowsFn     func(modelName, group string, startTs, endTs int64) ([]model.PerfMetric, error)
	QuerySummaryBucketsFn func(startTs, endTs int64, groups []string) ([]model.PerfMetricSummaryBucket, error)
)

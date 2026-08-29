package usage

import (
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/internal/usage/record_perf"
)

func init() {
	// Wire the perf_metrics infrastructure package to this store. The pkg
	// must not import usage (it is imported back from here), so the
	// persistence entry points are injected as function hooks.
	record_perf.UpsertMetricFn = UpsertPerfMetric
	record_perf.DeleteMetricsBeforeFn = DeletePerfMetricsBefore
	record_perf.QueryMetricRowsFn = GetPerfMetricsInternal
	record_perf.QuerySummaryBucketsFn = func(startTs, endTs int64, groups []string) ([]model.PerfMetricSummaryBucket, error) {
		return GetPerfMetricsSummaryBucketsAll(startTs, endTs, groups)
	}
}

func UpsertPerfMetric(metric *model.PerfMetric) error {
	if metric == nil || metric.RequestCount == 0 {
		return nil
	}
	return dbx.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "model_name"}, {Name: "group"}, {Name: "bucket_ts"}},
		DoUpdates: clause.AssignmentColumns([]string{"request_count", "success_count", "total_latency_ms", "ttft_sum_ms", "ttft_count", "output_tokens", "generation_ms"}),
	}).Create(metric).Error
}

func GetPerfMetricsInternal(modelName string, group string, startTs int64, endTs int64) ([]model.PerfMetric, error) {
	var metrics []model.PerfMetric
	query := dbx.DB.Model(&model.PerfMetric{}).
		Where("model_name = ? AND bucket_ts >= ? AND bucket_ts <= ?", modelName, startTs, endTs)
	if group != "" {
		query = query.Where("`group` = ?", group)
	}
	err := query.Order("bucket_ts ASC").Find(&metrics).Error
	return metrics, err
}

type PerfMetricSummary = model.PerfMetricSummary
type PerfMetricSummaryBucket = model.PerfMetricSummaryBucket

func GetPerfMetricsSummaryBucketsAll(startTs int64, endTs int64, groups []string) ([]PerfMetricSummaryBucket, error) {
	var buckets []PerfMetricSummaryBucket
	query := dbx.DB.Table("perf_metrics").
		Select("model_name, `group`, sum(request_count) as request_count, sum(success_count) as success_count, sum(total_latency_ms) as total_latency_ms, sum(ttft_sum_ms) as ttft_sum_ms, sum(ttft_count) as ttft_count, sum(output_tokens) as output_tokens, sum(generation_ms) as generation_ms, bucket_ts").
		Where("bucket_ts >= ? AND bucket_ts <= ?", startTs, endTs)
	if len(groups) > 0 {
		query = query.Where("`group` IN ?", groups)
	}
	err := query.Group("model_name, `group`, bucket_ts").Order("bucket_ts ASC").Find(&buckets).Error
	return buckets, err
}

func DeletePerfMetricsBefore(cutoffTs int64) error {
	return dbx.DB.Where("bucket_ts < ?", cutoffTs).Delete(&model.PerfMetric{}).Error
}

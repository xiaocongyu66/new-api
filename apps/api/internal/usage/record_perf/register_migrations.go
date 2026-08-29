package record_perf

import "github.com/QuantumNous/new-api/internal/common/dbx"

// Records owned by this domain register themselves for AutoMigrate.
func init() {
	dbx.RegisterMigrations(
		dbx.Migration{Model: &PerfMetric{}, Name: "PerfMetric"},
	)
}

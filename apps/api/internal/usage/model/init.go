package model

import (
	rootmodel "github.com/QuantumNous/new-api/model"
)

func init() {
	rootmodel.RegisterEntities(
		&QuotaData{},
		&PerfMetric{},
	)
	rootmodel.RegisterLogEntities(&Log{})
}

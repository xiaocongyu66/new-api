package service

import (
	"github.com/QuantumNous/new-api/internal/capabilities/administration"
)

// SystemInstanceInfo matches the administration.SystemInstanceInfo type used for
// system instance reporting. Kept as an alias for backward compatibility.
type SystemInstanceInfo = administration.SystemInstanceInfo

// SystemInstanceRoleInfo is an alias for the administration type.
type SystemInstanceRoleInfo = administration.SystemInstanceRoleInfo

// SystemInstanceRuntimeInfo is an alias for the administration type.
type SystemInstanceRuntimeInfo = administration.SystemInstanceRuntimeInfo

// SystemInstanceHostInfo is an alias for the administration type.
type SystemInstanceHostInfo = administration.SystemInstanceHostInfo

// SystemInstanceResources is an alias for the administration type.
type SystemInstanceResources = administration.SystemInstanceResources

// SystemInstanceResourceUsage is an alias for the administration type.
type SystemInstanceResourceUsage = administration.SystemInstanceResourceUsage

// SystemInstanceStorageMetrics is an alias for the administration type.
type SystemInstanceStorageMetrics = administration.SystemInstanceStorageMetrics

// StartSystemInstanceReporter starts the background system instance reporter.
func StartSystemInstanceReporter() {
	administration.StartSystemInstanceReporter()
}

// ReportCurrentSystemInstance reports the current system instance info to DB.
func ReportCurrentSystemInstance() error {
	return administration.ReportCurrentSystemInstance()
}

package common

// Log type IDs. Defined as plain constants (not iota) so the underlying
// numeric values stay stable even if the constants are reordered.
//
// These values are persisted in the logs table, so renumbering them would
// silently break historical log filtering. Add new types at the end.
const (
	LogTypeUnknown = 0
	LogTypeTopup   = 1
	LogTypeConsume = 2
	LogTypeManage  = 3
	LogTypeSystem  = 4
	LogTypeError   = 5
	LogTypeRefund  = 6
	LogTypeLogin   = 7
)

package routestats

import (
	"sync"
)

// Audit outcome codes recorded per attempt.
const (
	AuditOutcomeSuccess   = 0 // attempt succeeded (newAPIError == nil)
	AuditOutcomeThrottled = 1 // 429 / throttle
	AuditOutcomeFatal     = 2 // fatal upstream/channel error
	AuditOutcomeNeutral   = 3 // caller 4xx, client cancel, transport failure
)

// AuditAttempt is one recorded attempt in the ring buffer.
type AuditAttempt struct {
	RequestID      string `json:"request_id"`
	ClientRequestID string `json:"client_request_id,omitempty"`
	Attempt        int    `json:"attempt"`
	Group          string `json:"group"`
	Alias          string `json:"alias"`
	ChannelID      int    `json:"channel_id"`
	KeyIndex       int    `json:"key_index"`
	UpstreamModel  string `json:"upstream_model"`
	// Path is the selection path that chose this route unit: "weighted",
	// "affinity" or "specific". Empty when the request took no labelled path.
	Path    string `json:"path,omitempty"`
	Outcome int    `json:"outcome"`
}
const auditRingCapacity = 32768 // 2^15; fits 13,000 requests + retries
// AuditAttempt is 128 bytes (6 strings × 16B + 4 ints × 8B on amd64).
// Backing array ≈ 128 × 32768 = 4,194,304 bytes (~4 MiB), well under 20 MiB.
// AuditOutcomeFromRouteStats maps the internal routeStatsOutcome (0=neutral, 1=throttled, 2=fatal)
// to the corresponding audit outcome code. Success (0) is handled separately by the caller.
func AuditOutcomeFromRouteStats(routeStatsOutcome int) int {
	switch routeStatsOutcome {
	case 1: // throttled
		return AuditOutcomeThrottled
	case 2: // fatal
		return AuditOutcomeFatal
	default: // 0 = neutral
		return AuditOutcomeNeutral
	}
}
var auditRing struct {
	mu   sync.Mutex
	buf  []AuditAttempt
	head int
	size int
}

func init() {
	auditRing.buf = make([]AuditAttempt, auditRingCapacity)
}

// AuditRingCapacity returns the ring buffer's fixed capacity.
func AuditRingCapacity() int {
	return auditRingCapacity
}
// RecordAttempt appends one attempt to the audit ring buffer. When the buffer
// is full, the oldest entry is overwritten (FIFO eviction).
// clientRequestID is the client-sent X-Request-Id header (may be empty).
// path is the selection path label (may be empty when unlabelled).
func RecordAttempt(requestID string, attempt int, key RouteKey, outcome int, clientRequestID string, path string) {
	auditRing.mu.Lock()
	defer auditRing.mu.Unlock()

	entry := AuditAttempt{
		RequestID:        requestID,
		ClientRequestID:  clientRequestID,
		Attempt:          attempt,
		Group:            key.Group,
		Alias:            key.PublicModelAlias,
		ChannelID:        key.ChannelID,
		KeyIndex:         key.KeyIndex,
		UpstreamModel:    key.UpstreamModel,
		Path:             path,
		Outcome:          outcome,
	}

	auditRing.buf[auditRing.head] = entry
	auditRing.head = (auditRing.head + 1) % auditRingCapacity
	if auditRing.size < auditRingCapacity {
		auditRing.size++
	}
}

// SnapshotAttempts returns a copy of all recorded attempts in insertion order
// (oldest first). The slice length equals the current number of entries.
func SnapshotAttempts() []AuditAttempt {
	auditRing.mu.Lock()
	defer auditRing.mu.Unlock()

	out := make([]AuditAttempt, auditRing.size)
	if auditRing.size == 0 {
		return out
	}
	if auditRing.size < auditRingCapacity {
		// Buffer not yet full: entries are in buf[0:head] in insertion order.
		copy(out, auditRing.buf[:auditRing.head])
	} else {
		// Buffer full: oldest is at head, newest at head-1 (wrapping).
		n := copy(out, auditRing.buf[auditRing.head:])
		copy(out[n:], auditRing.buf[:auditRing.head])
	}
	return out
}

// ResetAudit clears the audit ring buffer. For testing and kill-switch toggles.
func ResetAudit() {
	auditRing.mu.Lock()
	auditRing.head = 0
	auditRing.size = 0
	auditRing.mu.Unlock()
}

// ShareTarget is one candidate's target share at the time of selection. The
// route identity is flattened into JSON-serialisable fields because Go's
// encoding/json cannot marshal a struct (RouteID) as a map key.
type ShareTarget struct {
	ChannelID     int     `json:"channel_id"`
	KeyIndex      int     `json:"key_index"`
	UpstreamModel string  `json:"upstream_model"`
	Target        float64 `json:"target"`
}

// ShareWindowEntry is one recorded selection in a pool's share window.
type ShareWindowEntry struct {
	Selected      ShareTarget   `json:"selected"`
	Targets       []ShareTarget `json:"targets"`
}

// SharePoolSnapshot is the window contents for one pool.
type SharePoolSnapshot struct {
	Pool   PoolKey            `json:"pool"`
	Window []ShareWindowEntry `json:"window"`
}

// ShareSnapshot returns a copy of every pool's share window, ordered by pool.
func ShareSnapshot() []SharePoolSnapshot {
	shareStore.mu.RLock()
	pools := make([]SharePoolSnapshot, 0, len(shareStore.pools))
	for pk, w := range shareStore.pools {
		w.mu.Lock()
		window := make([]ShareWindowEntry, w.size)
		for i := range w.size {
			e := w.entries[(w.head+i)%len(w.entries)]
			targets := make([]ShareTarget, 0, len(e.targets))
			for id, s := range e.targets {
				targets = append(targets, ShareTarget{
					ChannelID:     id.ChannelID,
					KeyIndex:      id.KeyIndex,
					UpstreamModel: id.UpstreamModel,
					Target:        s,
				})
			}
			window[i] = ShareWindowEntry{
				Selected: ShareTarget{
					ChannelID:     e.selected.ChannelID,
					KeyIndex:       e.selected.KeyIndex,
					UpstreamModel:  e.selected.UpstreamModel,
				},
				Targets: targets,
			}
		}
		w.mu.Unlock()
		pools = append(pools, SharePoolSnapshot{Pool: pk, Window: window})
	}
	shareStore.mu.RUnlock()
	return pools
}
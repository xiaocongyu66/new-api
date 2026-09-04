package routestats

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditRingBufferBasic(t *testing.T) {
	ResetAudit()
	defer ResetAudit()

	RecordAttempt("req-1", 0, RouteKey{Group: "g1", PublicModelAlias: "alias1", ChannelID: 1, KeyIndex: 0, UpstreamModel: "up1"}, AuditOutcomeSuccess, "client-1", "weighted")

	attempts := SnapshotAttempts()
	require.Len(t, attempts, 1)
	assert.Equal(t, "req-1", attempts[0].RequestID)
	assert.Equal(t, "client-1", attempts[0].ClientRequestID)
	assert.Equal(t, 0, attempts[0].Attempt)
	assert.Equal(t, "g1", attempts[0].Group)
	assert.Equal(t, "alias1", attempts[0].Alias)
	assert.Equal(t, 1, attempts[0].ChannelID)
	assert.Equal(t, 0, attempts[0].KeyIndex)
	assert.Equal(t, "up1", attempts[0].UpstreamModel)
	assert.Equal(t, "weighted", attempts[0].Path)
	assert.Equal(t, AuditOutcomeSuccess, attempts[0].Outcome)
}

func TestAuditClientRequestIDEmptyOmitted(t *testing.T) {
	ResetAudit()
	defer ResetAudit()

	// Empty clientRequestID should be omitted from JSON (omitempty)
	RecordAttempt("req-2", 0, RouteKey{Group: "g2", PublicModelAlias: "alias2", ChannelID: 2, KeyIndex: 1, UpstreamModel: "up2"}, AuditOutcomeSuccess, "", "")

	attempts := SnapshotAttempts()
	require.Len(t, attempts, 1)
	assert.Equal(t, "", attempts[0].ClientRequestID)

	// Verify JSON omitempty: marshal and check field absent
	data, err := common.Marshal(attempts[0])
	require.NoError(t, err)
	assert.NotContains(t, string(data), "client_request_id")
}

// TestAuditPathJSONContract pins the wire contract the stress runner relies on:
// an unlabelled attempt must not carry a "path" key at all (so a reader can tell
// "no label" from a real label), while a labelled one must expose it verbatim.
func TestAuditPathJSONContract(t *testing.T) {
	ResetAudit()
	defer ResetAudit()

	key := RouteKey{Group: "g", PublicModelAlias: "a", ChannelID: 1, KeyIndex: 0, UpstreamModel: "u"}
	RecordAttempt("req-unlabelled", 0, key, AuditOutcomeSuccess, "", "")
	RecordAttempt("req-affinity", 0, key, AuditOutcomeSuccess, "", "affinity")

	attempts := SnapshotAttempts()
	require.Len(t, attempts, 2)

	unlabelled, err := common.Marshal(attempts[0])
	require.NoError(t, err)
	assert.NotContains(t, string(unlabelled), "path",
		"an empty path must be omitted so absent and labelled are distinguishable")

	labelled, err := common.Marshal(attempts[1])
	require.NoError(t, err)
	assert.Contains(t, string(labelled), `"path":"affinity"`)
}

func TestAuditRingBufferFIFO(t *testing.T) {
	ResetAudit()
	defer ResetAudit()

	// Fill exactly to capacity
	for i := 0; i < auditRingCapacity; i++ {
		RecordAttempt(string(rune('a'+i%26)), i, RouteKey{Group: "g", PublicModelAlias: "a", ChannelID: i, KeyIndex: 0, UpstreamModel: "u"}, AuditOutcomeSuccess, "", "")
	}

	attempts := SnapshotAttempts()
	require.Len(t, attempts, auditRingCapacity)
	// Oldest entry should be i=0
	assert.Equal(t, "a", attempts[0].RequestID)
	assert.Equal(t, 0, attempts[0].Attempt)
	// Newest entry should be i=capacity-1
	assert.Equal(t, auditRingCapacity-1, attempts[auditRingCapacity-1].Attempt)

	// Overwrite one more -> oldest (i=0) is evicted, i=capacity added
	RecordAttempt("new", auditRingCapacity, RouteKey{Group: "g", PublicModelAlias: "a", ChannelID: 99999, KeyIndex: 0, UpstreamModel: "u"}, AuditOutcomeSuccess, "", "")

	attempts = SnapshotAttempts()
	require.Len(t, attempts, auditRingCapacity)
	// Oldest should now be i=1
	assert.Equal(t, 1, attempts[0].Attempt)
	// Newest should be i=capacity
	assert.Equal(t, auditRingCapacity, attempts[auditRingCapacity-1].Attempt)
}

func TestAuditRingBufferConcurrent(t *testing.T) {
	ResetAudit()
	defer ResetAudit()

	var wg sync.WaitGroup
	concurrency := 50
	iterations := 700 // 50 * 700 = 35000 > auditRingCapacity (32768)

	wg.Add(concurrency)
	for range concurrency {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				RecordAttempt("req", i, RouteKey{Group: "g", PublicModelAlias: "a", ChannelID: i, KeyIndex: 0, UpstreamModel: "u"}, AuditOutcomeSuccess, "", "")
			}
		}()
	}

	wg.Wait()

	attempts := SnapshotAttempts()
	// Total records = concurrency * iterations = 35000 > capacity
	// Buffer should be full, no data race panic
	require.Len(t, attempts, auditRingCapacity)
}

func TestShareSnapshotEmpty(t *testing.T) {
	ResetShares()
	snap := ShareSnapshot()
	assert.Empty(t, snap)
}

func TestShareSnapshotStructure(t *testing.T) {
	ResetShares()
	defer ResetShares()

	cfg := DefaultRouteStatsSetting()
	cfg.ShareWindowSize = 10
	cfg.Enabled = true
	SetRouteStatsSetting(cfg)

	pool := PoolKey{Group: "g1", PublicModelAlias: "alias1"}
	id1 := RouteID{ChannelID: 1, KeyIndex: 0, UpstreamModel: "up1"}
	id2 := RouteID{ChannelID: 2, KeyIndex: 0, UpstreamModel: "up2"}

	targets := map[RouteID]float64{id1: 0.6, id2: 0.4}

	RecordSelection(pool, id1, targets, cfg)
	RecordSelection(pool, id2, targets, cfg)

	snap := ShareSnapshot()
	require.Len(t, snap, 1)
	assert.Equal(t, pool, snap[0].Pool)
	require.Len(t, snap[0].Window, 2)

	// First entry selected id1
	assert.Equal(t, id1.ChannelID, snap[0].Window[0].Selected.ChannelID)
	assert.Equal(t, id1.KeyIndex, snap[0].Window[0].Selected.KeyIndex)
	assert.Equal(t, id1.UpstreamModel, snap[0].Window[0].Selected.UpstreamModel)

	// Targets slice has both candidates with correct shares
	require.Len(t, snap[0].Window[0].Targets, 2)
	targetMap := map[RouteID]float64{}
	for _, t := range snap[0].Window[0].Targets {
		targetMap[RouteID{ChannelID: t.ChannelID, KeyIndex: t.KeyIndex, UpstreamModel: t.UpstreamModel}] = t.Target
	}
	assert.InDelta(t, 0.6, targetMap[id1], 1e-9)
	assert.InDelta(t, 0.4, targetMap[id2], 1e-9)

	// Second entry selected id2
	assert.Equal(t, id2.ChannelID, snap[0].Window[1].Selected.ChannelID)

	// Verify JSON serialisable (no panic on map key)
	data, err := common.Marshal(snap)
	require.NoError(t, err)
	assert.Contains(t, string(data), "channel_id")
}

func TestShareSnapshotMultiplePools(t *testing.T) {
	ResetShares()
	defer ResetShares()

	cfg := DefaultRouteStatsSetting()
	cfg.ShareWindowSize = 5
	cfg.Enabled = true
	SetRouteStatsSetting(cfg)

	pool1 := PoolKey{Group: "g1", PublicModelAlias: "a1"}
	pool2 := PoolKey{Group: "g2", PublicModelAlias: "a2"}
	id1 := RouteID{ChannelID: 1, KeyIndex: 0, UpstreamModel: "u1"}
	id2 := RouteID{ChannelID: 2, KeyIndex: 0, UpstreamModel: "u2"}

	RecordSelection(pool1, id1, map[RouteID]float64{id1: 1.0}, cfg)
	RecordSelection(pool2, id2, map[RouteID]float64{id2: 1.0}, cfg)

	snap := ShareSnapshot()
	require.Len(t, snap, 2)

	// Both pools present
	poolKeys := map[PoolKey]bool{}
	for _, ps := range snap {
		poolKeys[ps.Pool] = true
	}
	assert.True(t, poolKeys[pool1])
	assert.True(t, poolKeys[pool2])

	// Each has 1 entry
	for _, ps := range snap {
		require.Len(t, ps.Window, 1)
	}
}

func TestResetAudit(t *testing.T) {
	ResetAudit()
	defer ResetAudit()

	for i := 0; i < 5; i++ {
		RecordAttempt("req", i, RouteKey{Group: "g", PublicModelAlias: "a", ChannelID: i, KeyIndex: 0, UpstreamModel: "u"}, AuditOutcomeSuccess, "", "")
	}
	assert.Len(t, SnapshotAttempts(), 5)

	ResetAudit()
	assert.Len(t, SnapshotAttempts(), 0)
}

// TestAuditRingCapacity verifies the ring buffer capacity is sufficient for
// S1-S3 scenarios (13,000 requests + retries) and that FIFO eviction works
// correctly when writing beyond capacity.
func TestAuditRingCapacity(t *testing.T) {
	ResetAudit()
	defer ResetAudit()

	// Capacity must be >= 13000 to hold S1-S3 scenario without truncation
	assert.GreaterOrEqual(t, auditRingCapacity, 13000, "capacity must hold S1-S3 scenario")
	// AuditRingCapacity() exported function returns the same constant
	assert.Equal(t, auditRingCapacity, AuditRingCapacity())

	// Write capacity + 100 entries -> oldest 100 should be evicted (FIFO)
	overwrite := 100
	total := auditRingCapacity + overwrite
	for i := 0; i < total; i++ {
		RecordAttempt(string(rune('a'+i%26)), i, RouteKey{Group: "g", PublicModelAlias: "a", ChannelID: i, KeyIndex: 0, UpstreamModel: "u"}, AuditOutcomeSuccess, "", "")
	}

	attempts := SnapshotAttempts()
	require.Len(t, attempts, auditRingCapacity, "snapshot length equals capacity when full")

	// Oldest entry should be i=overwrite (first 100 evicted)
	assert.Equal(t, overwrite, attempts[0].Attempt)
	// Newest entry should be i=total-1
	assert.Equal(t, total-1, attempts[auditRingCapacity-1].Attempt)

	// Verify strict FIFO order: attempts[i].Attempt == overwrite + i
	for i := range auditRingCapacity {
		assert.Equal(t, overwrite+i, attempts[i].Attempt, "FIFO order at index %d", i)
	}
}

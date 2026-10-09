package channel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetConcurrencyGateForTest hands a case a fresh process gate so pool
// state cannot leak between cases: the production accessor is lazy, and
// these cases assert exact slot counts.
func resetConcurrencyGateForTest(t *testing.T) *ConcurrencyGate {
	t.Helper()
	concurrencyGateMu.Lock()
	concurrencyGate = NewConcurrencyGate()
	concurrencyGateMu.Unlock()
	return concurrencyGate
}

// globalFree reads the global pool's free slots for assertions, failing
// the case when the pool is unmounted.
func globalFree(t *testing.T, gate *ConcurrencyGate) int {
	t.Helper()
	free, mounted := gate.GlobalAvailable()
	if !mounted {
		t.Fatalf("expected the global pool to be mounted")
	}
	return free
}

// channelFree reads one key's free slots for assertions, failing the
// case when the key is unregistered.
func channelFree(t *testing.T, gate *ConcurrencyGate, key ChannelKey) int {
	t.Helper()
	free, registered := gate.AvailablePermits(key)
	if !registered {
		t.Fatalf("expected key %+v to be registered", key)
	}
	return free
}

func TestChannelSemaphoreTryAcquireAccounting(t *testing.T) {
	sem := newChannelSemaphore(3)
	permits := make([]*channelPermit, 0, 3)
	for range 3 {
		permit := sem.tryAcquire()
		require.NotNil(t, permit, "a free pool must hand out a slot")
		permits = append(permits, permit)
	}
	assert.Equal(t, 0, sem.available())
	assert.Nil(t, sem.tryAcquire(), "the full pool must reject, not queue")

	permits[0].Release()
	assert.Equal(t, 1, sem.available())
	require.NotNil(t, sem.tryAcquire(), "a released slot must be acquirable")
}

func TestChannelPermitReleaseIsIdempotent(t *testing.T) {
	sem := newChannelSemaphore(2)
	permits := make([]*channelPermit, 2)
	for i := range permits {
		permits[i] = sem.tryAcquire()
		require.NotNil(t, permits[i])
	}
	permits[0].Release()
	permits[0].Release() // the retry path's explicit release plus the exit defer
	assert.Equal(t, 1, sem.available(),
		"a double release must return the slot exactly once")
	reacquired := sem.tryAcquire()
	require.NotNil(t, reacquired, "the released slot is acquirable again")
	assert.Nil(t, sem.tryAcquire(), "the pool is full while permits[1] and the reacquired permit are held")
	reacquired.Release()
	assert.Equal(t, 1, sem.available())
	permits[1].Release()
	assert.Equal(t, 2, sem.available(), "the full pool must be free after both permits release")
}

func TestRegisterChannelLimitNeverReplacesExistingPool(t *testing.T) {
	state := NewConcurrencyState()
	key := ChannelKey{ChannelId: 7, KeyIndex: 0}

	state.RegisterChannelLimit(key, 2)
	release, acquired, registered := state.TryAcquire(key)
	require.True(t, acquired)
	require.True(t, registered)

	// An operator shrink/expansion re-running registration must not swap
	// the pool out from under the held permit.
	state.RegisterChannelLimit(key, 8)
	available, _ := state.AvailablePermits(key)
	assert.Equal(t, 1, available, "the original capacity must govern the held permit")

	release()
	available, _ = state.AvailablePermits(key)
	assert.Equal(t, 2, available,
		"the permit must return to the ORIGINAL pool, not a replacement")
}

func TestRegisterChannelLimitIsIdempotent(t *testing.T) {
	state := NewConcurrencyState()
	key := ChannelKey{ChannelId: 9, KeyIndex: 1}
	state.RegisterChannelLimit(key, 4)
	state.RegisterChannelLimit(key, 4)
	available, registered := state.AvailablePermits(key)
	assert.True(t, registered)
	assert.Equal(t, 4, available, "a duplicate registration must not double the capacity")
}

func TestRegisterChannelLimitZeroInstallsNothing(t *testing.T) {
	state := NewConcurrencyState()
	key := ChannelKey{ChannelId: 11, KeyIndex: 0}
	state.RegisterChannelLimit(key, 0)
	available, registered := state.AvailablePermits(key)
	assert.False(t, registered,
		"a zero limit is the unlimited convention and must never install a pool: a zero-capacity pool would wedge the key's first request")
	assert.Zero(t, available)
}

func TestChannelConcurrencyLimitReportsInstalledCapacity(t *testing.T) {
	state := NewConcurrencyState()
	limited := ChannelKey{ChannelId: 2, KeyIndex: 0}
	unregistered := ChannelKey{ChannelId: 1, KeyIndex: 0}

	state.RegisterChannelLimit(limited, 5)
	// Re-registration at a larger limit must not replace the pool: the
	// posterior must keep reading the capacity the live permits were
	// issued under.
	state.RegisterChannelLimit(limited, 9)
	installedCap, installedOK := state.ChannelConcurrencyLimit(limited)
	assert.Equal(t, [2]any{5, true}, [2]any{installedCap, installedOK},
		"the installed capacity is what the running permits were issued under")

	skippedCap, skippedOK := state.ChannelConcurrencyLimit(unregistered)
	assert.Equal(t, [2]any{0, false}, [2]any{skippedCap, skippedOK},
		"an unregistered key must be skipped by the posterior, not read as a zero-capacity pool")
}

func TestChannelConcurrencyLimitThroughGate(t *testing.T) {
	gate := resetConcurrencyGateForTest(t)
	key := ChannelKey{ChannelId: 20, KeyIndex: 0}
	gate.RegisterChannelLimit(key, 3)
	capOn, okOn := gate.ChannelConcurrencyLimit(key)
	assert.Equal(t, [2]any{3, true}, [2]any{capOn, okOn})
	capOff, okOff := gate.ChannelConcurrencyLimit(ChannelKey{ChannelId: 21, KeyIndex: 0})
	assert.Equal(t, [2]any{0, false}, [2]any{capOff, okOff})
	sharedCap, sharedOK := ChannelConcurrencyLimit(key)
	assert.Equal(t, [2]any{3, true}, [2]any{sharedCap, sharedOK},
		"the posterior reads the process-wide gate, the same object the relay loop acquires from")
}

func TestAvailablePermitsDistinguishesUnregisteredFromFull(t *testing.T) {
	state := NewConcurrencyState()
	unregistered := ChannelKey{ChannelId: 1, KeyIndex: 0}
	limited := ChannelKey{ChannelId: 2, KeyIndex: 0}
	state.RegisterChannelLimit(limited, 1)

	available, registered := state.AvailablePermits(unregistered)
	assert.Equal(t, [2]any{0, false}, [2]any{available, registered},
		"None: unregistered keys are unlimited, not 'zero free'")

	release, acquired, acquiredRegistered := state.TryAcquire(limited)
	require.True(t, acquired)
	require.True(t, acquiredRegistered)
	available, registered = state.AvailablePermits(limited)
	assert.Equal(t, [2]any{0, true}, [2]any{available, registered},
		"Some(0): registered but saturated")

	release()
	available, registered = state.AvailablePermits(limited)
	assert.Equal(t, [2]any{1, true}, [2]any{available, registered})
}

// TestTryAcquireUnregisteredPassesThroughUnlimited pins mono's unregistered
// arm: the attempt proceeds with a no-op release and the registered flag
// down.
func TestTryAcquireUnregisteredPassesThroughUnlimited(t *testing.T) {
	state := NewConcurrencyState()
	key := ChannelKey{ChannelId: 41, KeyIndex: 0}

	release, acquired, registered := state.TryAcquire(key)
	require.True(t, acquired, "unregistered must pass through")
	assert.False(t, registered)
	release()
	assert.NotPanics(t, release, "the no-op release must be safe to call twice")

	available, registeredAgain := state.AvailablePermits(key)
	assert.False(t, registeredAgain, "a pass-through must not register the key")
	assert.Zero(t, available)
}

// TestTryAcquireFullRejects pins mono's RateLimited arm: full pool ⇒ not
// acquired, and the registry is otherwise untouched.
func TestTryAcquireFullRejects(t *testing.T) {
	state := NewConcurrencyState()
	key := ChannelKey{ChannelId: 42, KeyIndex: 0}
	state.RegisterChannelLimit(key, 1)

	holder, ok, reg := state.TryAcquire(key)
	require.True(t, ok)
	require.True(t, reg)

	release, acquired, registered := state.TryAcquire(key)
	assert.False(t, acquired, "a full pool must reject the attempt")
	assert.True(t, registered, "the rejection must be attributed to a REGISTERED full pool, not a pass-through")
	assert.Nil(t, release, "a rejected attempt holds nothing")

	holder()
	_, ok, _ = state.TryAcquire(key)
	assert.True(t, ok, "the released slot must be acquirable again")
}

func TestGlobalLimitLifecycle(t *testing.T) {
	gate := resetConcurrencyGateForTest(t)
	available, mounted := gate.GlobalAvailable()
	assert.Equal(t, [2]any{0, false}, [2]any{available, mounted},
		"fresh gate: global pool unmounted (unlimited)")

	release, ok := gate.AcquireRelaySlots(ChannelKey{ChannelId: 5, KeyIndex: 0})
	require.True(t, ok, "unmounted global + unregistered key must pass through")
	release()

	gate.SetGlobalLimit(2)
	available, mounted = gate.GlobalAvailable()
	assert.Equal(t, [2]any{2, true}, [2]any{available, mounted})

	permits := make([]func(), 0, 2)
	for range 2 {
		p, ok := gate.AcquireRelaySlots(ChannelKey{ChannelId: 5, KeyIndex: 0})
		require.True(t, ok)
		permits = append(permits, p)
	}
	_, ok = gate.AcquireRelaySlots(ChannelKey{ChannelId: 6, KeyIndex: 0})
	assert.False(t, ok, "the full global pool must reject even a fresh key")

	// A hot-update swap keeps the held permits alive on the old pool:
	// the new capacity shows up immediately for new attempts, and the
	// old permits must NOT inflate it on release.
	gate.SetGlobalLimit(4)
	available, mounted = gate.GlobalAvailable()
	assert.Equal(t, [2]any{4, true}, [2]any{available, mounted})
	for _, p := range permits {
		p()
	}
	available, _ = gate.GlobalAvailable()
	assert.Equal(t, 4, available,
		"releasing permits of the replaced pool must not touch the new pool")

	// 0 unmounts: everything unlimited again.
	gate.SetGlobalLimit(0)
	available, mounted = gate.GlobalAvailable()
	assert.Equal(t, [2]any{0, false}, [2]any{available, mounted})
	_, ok = gate.AcquireRelaySlots(ChannelKey{ChannelId: 5, KeyIndex: 0})
	assert.True(t, ok, "an unmounted global pool must pass through unlimited")
}

func TestAcquireRelaySlotsComposesGlobalAndKeyPools(t *testing.T) {
	gate := resetConcurrencyGateForTest(t)
	key := ChannelKey{ChannelId: 77, KeyIndex: 0}
	gate.SetGlobalLimit(2)
	gate.RegisterChannelLimit(key, 1)

	// Happy path: both pools take a slot, one idempotent composite release
	// returns both.
	release, ok := gate.AcquireRelaySlots(key)
	require.True(t, ok)
	assert.Equal(t, 1, globalFree(t, gate))
	assert.Equal(t, 0, channelFree(t, gate, key))

	release()
	release() // double release must not over-return
	assert.Equal(t, 2, globalFree(t, gate),
		"the global pool must hold exactly one slot per acquired attempt")
	assert.Equal(t, 1, channelFree(t, gate, key))

	// Key pool full: the attempt is rejected AND the already-taken global
	// slot is returned, so a rejection leaks nothing.
	holder, ok, _ := gate.TryAcquire(key)
	require.True(t, ok)
	_, ok = gate.AcquireRelaySlots(key)
	assert.False(t, ok, "a full key pool must reject the attempt")
	assert.Equal(t, 2, globalFree(t, gate),
		"a key-pool rejection must not strand the global slot")
	holder()

	// Same shape on a fresh gate, global pool full.
	g2 := resetConcurrencyGateForTest(t)
	g2.SetGlobalLimit(1)
	full, ok := g2.AcquireRelaySlots(ChannelKey{ChannelId: 78, KeyIndex: 0})
	require.True(t, ok)
	_, ok = g2.AcquireRelaySlots(ChannelKey{ChannelId: 78, KeyIndex: 0})
	assert.False(t, ok, "the full global pool must reject the attempt")
	full()
	_, ok = g2.AcquireRelaySlots(ChannelKey{ChannelId: 78, KeyIndex: 0})
	assert.True(t, ok, "the released slot must be reusable")
}

// TestRegisterChannelConcurrencyLimits pins the snapshot-rebuild hook:
// the limit stays off at 0 (the default), and >0 installs one pool per
// enumerated upstream key.
func TestRegisterChannelConcurrencyLimits(t *testing.T) {
	gate := resetConcurrencyGateForTest(t)
	previous := GetConcurrencyLimitSetting()
	t.Cleanup(func() { RestoreConcurrencyLimitSetting(previous) })

	snapshot := []*Channel{
		{Id: 900, Key: "sk-single"},
		{Id: 901, Key: "sk-a\nsk-b\nsk-c", ChannelInfo: ChannelInfo{IsMultiKey: true}},
		{Id: 902, Key: ""}, // keyless: no route can select it
	}

	RestoreConcurrencyLimitSetting(&ConcurrencyLimitSetting{})
	RegisterChannelConcurrencyLimits(snapshot)
	_, registered := gate.AvailablePermits(ChannelKey{ChannelId: 900, KeyIndex: 0})
	assert.False(t, registered, "a zero limit must register nothing (the default stays unlimited)")

	RestoreConcurrencyLimitSetting(&ConcurrencyLimitSetting{ChannelMaxConcurrency: 3})
	RegisterChannelConcurrencyLimits(snapshot)
	assert.Equal(t, 3, channelFree(t, gate, ChannelKey{ChannelId: 900, KeyIndex: 0}))
	assert.Equal(t, 3, channelFree(t, gate, ChannelKey{ChannelId: 901, KeyIndex: 0}))
	assert.Equal(t, 3, channelFree(t, gate, ChannelKey{ChannelId: 901, KeyIndex: 1}))
	assert.Equal(t, 3, channelFree(t, gate, ChannelKey{ChannelId: 901, KeyIndex: 2}))
	_, registered = gate.AvailablePermits(ChannelKey{ChannelId: 902, KeyIndex: 0})
	assert.False(t, registered, "a keyless channel must not register a pool")

	// A later rebuild with the limit dropped back to 0 leaves the
	// installed pools in place: they keep gating, nothing is replaced.
	RestoreConcurrencyLimitSetting(&ConcurrencyLimitSetting{})
	RegisterChannelConcurrencyLimits(snapshot)
	assert.Equal(t, 3, channelFree(t, gate, ChannelKey{ChannelId: 900, KeyIndex: 0}),
		"a 0-limit rebuild must not uninstall the live pools")
}

// TestApplyGlobalConcurrencyLimitHotSwap drives the option apply path:
// SetGlobalLimit through the package accessor; unchanged capacity is a
// no-op, a changed capacity swaps the pool, 0 unmounts.
func TestApplyGlobalConcurrencyLimitHotSwap(t *testing.T) {
	gate := resetConcurrencyGateForTest(t)

	ApplyGlobalConcurrencyLimit(5)
	available, mounted := gate.GlobalAvailable()
	assert.True(t, mounted)
	assert.Equal(t, 5, available)

	ApplyGlobalConcurrencyLimit(5)
	available, mounted = gate.GlobalAvailable()
	assert.True(t, mounted, "re-applying the same capacity must keep the mounted pool")
	assert.Equal(t, 5, available)

	ApplyGlobalConcurrencyLimit(0)
	available, mounted = gate.GlobalAvailable()
	assert.False(t, mounted, "0 must unmount the pool (unlimited)")
	assert.Zero(t, available)
}

// TestProcessGateAccessors pins the process-wide surface the P2C phase
// consumes: AvailableChannelPermits against the shared gate, and
// AcquireRelaySlots acquiring/releasing through it.
func TestProcessGateAccessors(t *testing.T) {
	gate := resetConcurrencyGateForTest(t)
	key := ChannelKey{ChannelId: 321, KeyIndex: 0}
	gate.RegisterChannelLimit(key, 2)

	release, ok := AcquireRelaySlots(key)
	require.True(t, ok)
	available, registered := AvailableChannelPermits(key)
	assert.True(t, registered)
	assert.Equal(t, 1, available, "a taken slot must drop the free count the posterior reads")

	release()
	available, _ = AvailableChannelPermits(key)
	assert.Equal(t, 2, available)
}

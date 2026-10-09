package channel

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/QuantumNous/new-api/internal/logger"
)

// Concurrency gate: the in-flight slot pools of the relay loop
// (port of mono crates/gateway/gate/src/concurrency.rs +
// crates/gateway/forward/src/stage_concurrency.rs).
//
// Two pools gate each attempt, in mono's pipeline order:
//
//   - the GLOBAL pool: one process-wide slot pool sized by the
//     GatewayForwardMaxConcurrency option; 0 = unmounted (unlimited).
//   - the CHANNEL pools: one slot pool per upstream key (ChannelKey:
//     channel id + key index — deliberately NOT RouteKey, whose Model
//     field would slice one key's capacity across the models routed to
//     it), installed at channel snapshot rebuild from the
//     GatewayChannelMaxConcurrency option.
//
// Both pools are TRY-ONLY: a full pool rejects the attempt with a local
// 429 (the relay loop's slot-full branch) and never queues — the loop
// switches candidates instead of blocking (mono "从不阻塞排队").
//
// An unregistered upstream key is UNLIMITED: the attempt passes through
// with a no-op release and a warn logged once per key ever (mono
// "每渠道 warn 一次"). The registry deliberately never inserts a
// zero-capacity pool: one would wedge the first request of that key
// forever.
//
// ## permit lifetime
//
// AcquireRelaySlots returns an idempotent release func (sync.Once): the
// relay loop defers it for terminal paths (success return, terminal
// break — the slots are held while the attempt's handler consumes the
// upstream response synchronously) and calls it explicitly when it keeps
// switching candidates. Neither path can double-release.

// ChannelKey is the gate's upstream-key identity: one API key of one
// channel. Model-independent by design: the key's in-flight capacity is
// the same no matter which model the attempt relays (mono's channel_key
// index).
type ChannelKey struct {
	ChannelId int
	KeyIndex  int
}

// channelSemaphore is a fixed-capacity slot pool: a buffered channel with
// try-acquire-only semantics. A full pool rejects; it never queues.
type channelSemaphore struct {
	capacity int
	tokens   chan struct{}
}

func newChannelSemaphore(capacity int) *channelSemaphore {
	return &channelSemaphore{capacity: capacity, tokens: make(chan struct{}, capacity)}
}

// available reports the free slots. len(chan) is race-free.
func (s *channelSemaphore) available() int {
	return s.capacity - len(s.tokens)
}

// tryAcquire takes one slot without blocking, or nil when the pool is full.
func (s *channelSemaphore) tryAcquire() *channelPermit {
	select {
	case s.tokens <- struct{}{}:
		return &channelPermit{sem: s}
	default:
		return nil
	}
}

// channelPermit is one held slot. Release is idempotent, so a permit
// released explicitly (candidate switch) and again by the function-exit
// defer (terminal path) returns the slot exactly once.
type channelPermit struct {
	sem  *channelSemaphore
	once sync.Once
}

// Release returns the slot to the pool that issued it, at most once.
func (p *channelPermit) Release() {
	p.once.Do(func() {
		<-p.sem.tokens
	})
}

// ConcurrencyState is the per-upstream-key slot registry (mono
// ConcurrencyState). The keys are ChannelKey, not RouteKey: capacity
// belongs to the upstream key, not to a model.
type ConcurrencyState struct {
	mu    sync.RWMutex
	pools map[ChannelKey]*channelSemaphore
	// warned dedupes the unregistered-key pass-through warn to once per
	// key ever (mono's DashSet), so a busy key cannot log-spam.
	warned map[ChannelKey]struct{}
}

// NewConcurrencyState builds an empty registry: every key unregistered,
// i.e. unlimited.
func NewConcurrencyState() *ConcurrencyState {
	return &ConcurrencyState{
		pools:  make(map[ChannelKey]*channelSemaphore),
		warned: make(map[ChannelKey]struct{}),
	}
}

// RegisterChannelLimit idempotently installs the key's pool. An existing
// pool is NEVER replaced: a replacement would orphan the permits running
// requests already hold, so they would return slots to a pool nobody
// reads anymore (mono register_channel; runtime shrinkage belongs to the
// upper rebuild flow). 0 (the unlimited convention) installs nothing —
// a zero-capacity pool must never exist, it would wedge the key's first
// request.
func (s *ConcurrencyState) RegisterChannelLimit(key ChannelKey, maxConcurrency int) {
	if maxConcurrency <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.pools[key]; !exists {
		s.pools[key] = newChannelSemaphore(maxConcurrency)
	}
}

// AvailablePermits reports the key's free slots. (0, false) = unregistered
// (unlimited); (0, true) = registered but full — the distinction mono's
// None vs Some(0) draws, and the input the P2C posterior later reads:
// its concurrency factor is (permits+1)/(max+1) over the eligible pool's
// max available, and unregistered keys are skipped in both terms (mono
// selector.rs: unregistered factor is 1.0).
func (s *ConcurrencyState) AvailablePermits(key ChannelKey) (int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sem, ok := s.pools[key]
	if !ok {
		return 0, false
	}
	return sem.available(), true
}

// ChannelConcurrencyLimit reports the key's pool capacity (max in-flight),
// the other half of the P2C posterior's (permits+1)/(max+1) factor:
// AvailablePermits is the numerator's input, this is the denominator's.
// (0, false) = unregistered (the posterior skips the key entirely: its
// factor is 1.0); (max, true) = registered, the capacity the pool was
// installed with — an operator rebuild that re-registered the key keeps
// the original capacity (a pool is never replaced).
func (s *ConcurrencyState) ChannelConcurrencyLimit(key ChannelKey) (int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sem, ok := s.pools[key]
	if !ok {
		return 0, false
	}
	return sem.capacity, true
}

// TryAcquire takes the key's slot without queueing.
//
//   - unregistered: unlimited pass-through — acquired=true with a no-op
//     release, registered=false; the key's once-ever warn is logged here;
//   - registered and free: acquired=true, the returned release is the
//     permit's idempotent Release;
//   - registered and full: acquired=false — the caller rejects this
//     attempt (mono StageError::RateLimited → 429).
func (s *ConcurrencyState) TryAcquire(key ChannelKey) (release func(), acquired bool, registered bool) {
	s.mu.RLock()
	sem, exists := s.pools[key]
	var permit *channelPermit
	if exists {
		permit = sem.tryAcquire()
	}
	s.mu.RUnlock()
	switch {
	case !exists:
		s.warnUnregistered(key)
		return func() {}, true, false
	case permit == nil:
		return nil, false, true
	default:
		return permit.Release, true, true
	}
}

func (s *ConcurrencyState) warnUnregistered(key ChannelKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, done := s.warned[key]; done {
		return
	}
	s.warned[key] = struct{}{}
	// Context-less by design: the warn is a once-per-process
	// registry-visibility notice, not tied to any request.
	logger.LogWarn(context.Background(), fmt.Sprintf(
		"concurrency gate: upstream key (channel %d, key index %d) is not registered, passing through unlimited",
		key.ChannelId, key.KeyIndex))
}

// ConcurrencyGate is the process-wide gate: the channel registry plus one
// global slot pool.
type ConcurrencyGate struct {
	channels *ConcurrencyState

	globalMu sync.RWMutex
	global   *channelSemaphore // nil = unmounted (unlimited)
}

// NewConcurrencyGate builds a gate with the global pool unmounted and an
// empty channel registry: everything unlimited.
func NewConcurrencyGate() *ConcurrencyGate {
	return &ConcurrencyGate{channels: NewConcurrencyState()}
}

// RegisterChannelLimit installs one upstream key's pool (see
// ConcurrencyState.RegisterChannelLimit).
func (g *ConcurrencyGate) RegisterChannelLimit(key ChannelKey, maxConcurrency int) {
	g.channels.RegisterChannelLimit(key, maxConcurrency)
}

// AvailablePermits is the posterior selector's input point: the key's
// free slots and whether the key is registered at all (see
// ConcurrencyState.AvailablePermits).
func (g *ConcurrencyGate) AvailablePermits(key ChannelKey) (int, bool) {
	return g.channels.AvailablePermits(key)
}

// ChannelConcurrencyLimit is the posterior selector's capacity input:
// the key's installed pool capacity and whether the key is registered
// (see ConcurrencyState.ChannelConcurrencyLimit).
func (g *ConcurrencyGate) ChannelConcurrencyLimit(key ChannelKey) (int, bool) {
	return g.channels.ChannelConcurrencyLimit(key)
}

// TryAcquire takes the key's slot (see ConcurrencyState.TryAcquire).
func (g *ConcurrencyGate) TryAcquire(key ChannelKey) (release func(), acquired bool, registered bool) {
	return g.channels.TryAcquire(key)
}

// SetGlobalLimit installs the global pool (0 = unmounted: unlimited).
// Changing the capacity swaps the pool: permits already issued keep
// releasing into the pool that issued them, so no slot leaks — the new
// capacity governs new attempts from the next acquire on. (mono mounts
// its pool once at assembly; newapi's hot option applies it at write
// time instead.)
func (g *ConcurrencyGate) SetGlobalLimit(max int) {
	if max < 0 {
		max = 0
	}
	g.globalMu.Lock()
	defer g.globalMu.Unlock()
	if max == 0 {
		g.global = nil
		return
	}
	if g.global != nil && g.global.capacity == max {
		return
	}
	g.global = newChannelSemaphore(max)
}

// GlobalAvailable reports the global pool's free slots; (0, false) =
// unmounted (unlimited), (0, true) = mounted and full.
func (g *ConcurrencyGate) GlobalAvailable() (int, bool) {
	g.globalMu.RLock()
	defer g.globalMu.RUnlock()
	if g.global == nil {
		return 0, false
	}
	return g.global.available(), true
}

// acquireGlobalSlot takes the global pool's slot; an unmounted pool
// passes through unlimited with a no-op release, mirroring the registry's
// unregistered pass-through.
func (g *ConcurrencyGate) acquireGlobalSlot() (release func(), ok bool) {
	g.globalMu.RLock()
	sem := g.global
	g.globalMu.RUnlock()
	if sem == nil {
		return func() {}, true
	}
	permit := sem.tryAcquire()
	if permit == nil {
		return nil, false
	}
	return permit.Release, true
}

// AcquireRelaySlots takes the attempt's slots in mono's pipeline order —
// global pool first, then the upstream key's pool. The returned release
// is idempotent and covers both pools.
//
// ok=false: a pool is full; the caller rejects the attempt with the local
// 429. Pools already taken before the full one are returned before the
// false is reported, so a rejection leaks no slot.
func (g *ConcurrencyGate) AcquireRelaySlots(key ChannelKey) (release func(), ok bool) {
	globalRelease, globalOk := g.acquireGlobalSlot()
	if !globalOk {
		return nil, false
	}
	keyRelease, keyOk, _ := g.channels.TryAcquire(key)
	if !keyOk {
		globalRelease()
		return nil, false
	}
	return func() {
		keyRelease()
		globalRelease()
	}, true
}

// ConcurrencyLimitSetting is the option-backed half of the gate, kept in
// its own atomic view rather than on UnitHealthSetting: the limits govern
// the slot pools, not the health state machine. The zero value is the
// default: both pools unlimited.
type ConcurrencyLimitSetting struct {
	// Global process-wide slot pool size; 0 = unlimited.
	GlobalMaxConcurrency int
	// Per-upstream-key slot pool size, installed at channel snapshot
	// rebuild; 0 = unlimited (no registration).
	ChannelMaxConcurrency int
}

var concurrencyLimitSetting atomic.Pointer[ConcurrencyLimitSetting]

func init() {
	concurrencyLimitSetting.Store(&ConcurrencyLimitSetting{})
}

// GetConcurrencyLimitSetting reads the live limits; 0 = unlimited.
func GetConcurrencyLimitSetting() *ConcurrencyLimitSetting {
	if s := concurrencyLimitSetting.Load(); s != nil {
		return s
	}
	// Package init ordering: a sibling init() may read before the store
	// above ran.
	return &ConcurrencyLimitSetting{}
}

// RestoreConcurrencyLimitSetting swaps the live limits (tests, option
// apply).
func RestoreConcurrencyLimitSetting(s *ConcurrencyLimitSetting) {
	concurrencyLimitSetting.Store(s)
}

// GetGatewayForwardMaxConcurrency reads the global pool limit (0 =
// unlimited).
func GetGatewayForwardMaxConcurrency() int {
	return GetConcurrencyLimitSetting().GlobalMaxConcurrency
}

// GetGatewayChannelMaxConcurrency reads the per-upstream-key limit
// (0 = unlimited).
func GetGatewayChannelMaxConcurrency() int {
	return GetConcurrencyLimitSetting().ChannelMaxConcurrency
}

// ApplyGlobalConcurrencyLimit installs the global pool from the limit:
// the option apply path calls it on every GatewayForwardMaxConcurrency
// write so a hot update takes effect at write time, not at restart.
func ApplyGlobalConcurrencyLimit(max int) {
	GetConcurrencyGate().SetGlobalLimit(max)
}

// RegisterChannelConcurrencyLimits installs the live per-upstream-key
// limit on every key of one channel snapshot rebuild (the call in
// InitChannelCache). It registers only when the limit is > 0: at 0 the
// gate stays dormant and previously installed pools are left in place —
// they keep gating live traffic, and nothing in this design may replace
// an installed pool.
func RegisterChannelConcurrencyLimits(channels []*Channel) {
	max := GetGatewayChannelMaxConcurrency()
	if max <= 0 {
		return
	}
	gate := GetConcurrencyGate()
	for _, channel := range channels {
		if channel == nil || channel.Id <= 0 {
			continue
		}
		// The same key enumeration the route table uses: a single-key
		// channel is one key (index 0), a multi-key channel is len(keys)
		// indexed 0..len-1; a keyless channel registers nothing because
		// no route can select it.
		for keyIndex := range channel.GetKeys() {
			gate.RegisterChannelLimit(ChannelKey{ChannelId: channel.Id, KeyIndex: keyIndex}, max)
		}
	}
}

var (
	concurrencyGateMu sync.Mutex
	concurrencyGate   *ConcurrencyGate
)

// GetConcurrencyGate returns the process-wide gate. The relay loop and
// the later P2C posterior read the SAME object — that shared reference
// is what makes the slot signal visible to selection (mono's shared-Arc
// property in stage_concurrency).
func GetConcurrencyGate() *ConcurrencyGate {
	concurrencyGateMu.Lock()
	defer concurrencyGateMu.Unlock()
	if concurrencyGate == nil {
		concurrencyGate = NewConcurrencyGate()
	}
	return concurrencyGate
}

// AvailableChannelPermits is the posterior API P2C consumes: the
// process-wide registry's free slots for one upstream key, with the
// registered/unregistered distinction. Pair it with
// ChannelConcurrencyLimit for the denominator of the (permits+1)/(max+1)
// factor.
func AvailableChannelPermits(key ChannelKey) (int, bool) {
	return GetConcurrencyGate().AvailablePermits(key)
}

// ChannelConcurrencyLimit is the posterior's capacity input: the
// process-wide registry's installed pool capacity for one upstream key,
// with the registered/unregistered distinction. Unregistered keys report
// (0, false) and are skipped by the posterior (factor 1.0).
func ChannelConcurrencyLimit(key ChannelKey) (int, bool) {
	return GetConcurrencyGate().ChannelConcurrencyLimit(key)
}

// AcquireRelaySlots is the relay loop's acquire point: one global slot
// plus the upstream key's slot (mono stage order).
func AcquireRelaySlots(key ChannelKey) (release func(), ok bool) {
	return GetConcurrencyGate().AcquireRelaySlots(key)
}

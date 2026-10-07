package billing

import (
	"testing"

	ratio_setting "github.com/QuantumNous/new-api/internal/catalog/configure_ratio"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"
	hosttypes "github.com/QuantumNous/new-api/internal/types"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

// recordingSettler captures the actualQuota each Settle call received, standing
// in for a BillingSession so the realtime settle arithmetic can be asserted
// without building a full funding stack.
type recordingSettler struct {
	preConsumed int
	settledTo   []int
}

func (s *recordingSettler) Settle(actualQuota int) error {
	s.settledTo = append(s.settledTo, actualQuota)
	return nil
}

func (s *recordingSettler) Refund(c contract.Context) {}

func (s *recordingSettler) NeedsRefund() bool { return false }

func (s *recordingSettler) GetPreConsumedQuota() int { return s.preConsumed }

func (s *recordingSettler) Reserve(targetQuota int) error { return nil }

// realtimeTextUsage builds a usage event whose billed text-token quota is
// exactly 2*units at ratio 1 (quota sums text in + text out 1:1).
func realtimeTextUsage(units int) *dto.RealtimeUsage {
	u := &dto.RealtimeUsage{}
	u.InputTokens = units
	u.OutputTokens = units
	u.TotalTokens = 2 * units
	u.InputTokenDetails.TextTokens = units
	u.OutputTokenDetails.TextTokens = units
	return u
}

func realtimeRelayInfo(model string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId:          91,
		TokenId:         92,
		TokenKey:        "sk-realtime-test",
		OriginModelName: model,
		UsingGroup:      "default",
		UserGroup:       "default",
		// ChannelMeta is an embedded pointer: PostWssConsumeQuota and the
		// log-info builders read ChannelId/IsModelMapped through it, so it
		// must be non-nil like every production relay path leaves it.
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 1},
		// Ratio maps replace wholesale via Update*ByJSONString, so pinning the
		// test model + default group keeps batch quotas deterministic (1:1).
		PriceData: hosttypes.PriceData{
			ModelRatio: 1,
			// PostWssConsumeQuota takes the group multiplier from PriceData
			// (production fills it during pricing); zero would bill nothing.
			GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
		UserQuota: 1_000_000_000, // keeps the async low-quota notify inert
	}
}

func setupRealtimeRatioSettings(t *testing.T, model string) {
	t.Helper()
	savedModelRatios := ratio_setting.ModelRatio2JSONString()
	savedCompletionRatios := ratio_setting.CompletionRatio2JSONString()
	savedGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		_ = ratio_setting.UpdateModelRatioByJSONString(savedModelRatios)
		_ = ratio_setting.UpdateCompletionRatioByJSONString(savedCompletionRatios)
		_ = ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatios)
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"`+model+`":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"`+model+`":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
}

func seedRealtimeFixtures(t *testing.T, walletQuota int) {
	t.Helper()
	seedUser(t, 91, walletQuota)
	require.NoError(t, dbx.DB.Create(&identity.Token{
		Id: 92, UserId: 91, Key: "realtime-test", Name: "realtime-test",
		Status: common.TokenStatusEnabled, UnlimitedQuota: true,
	}).Error)
}

func scanUserQuota(t *testing.T) int {
	t.Helper()
	var quota int
	require.NoError(t, dbx.DB.Raw("SELECT quota FROM users WHERE id = ?", 91).Scan(&quota).Error)
	return quota
}

// TestRealtimeBatchesSettleOnceAgainstCumulativeUsage pins the F6 invariant:
// quota charged per response.done batch via PreWssConsumeQuota must be
// subtracted from the cumulative-usage settlement, so a session pays its total
// usage exactly once instead of batches + full cumulative (~2x).
func TestRealtimeBatchesSettleOnceAgainstCumulativeUsage(t *testing.T) {
	setupIdentityTestDB(t)
	const model = "realtime-test-model"
	setupRealtimeRatioSettings(t, model)
	seedRealtimeFixtures(t, 500_000)

	info := realtimeRelayInfo(model)
	ctxRaw, _ := fiberadapter.NewSyntheticContext(nil)

	require.NoError(t, PreWssConsumeQuota(ctxRaw, info, realtimeTextUsage(10)))
	require.NoError(t, PreWssConsumeQuota(ctxRaw, info, realtimeTextUsage(20)))
	require.Equal(t, 60, info.RealtimeChargedQuota)

	walletAfterBatches := scanUserQuota(t)
	require.Equal(t, 500_000-60, walletAfterBatches)

	// Cumulative usage equals the two batches; the actual handed to the
	// session must be the batch-credited net (60-60=0), never raw cumulative.
	settler := &recordingSettler{preConsumed: 500}
	info.Billing = settler

	PostWssConsumeQuota(ctxRaw, info, model, realtimeTextUsage(30), "")

	require.Equal(t, []int{0}, settler.settledTo)
	require.Equal(t, walletAfterBatches, scanUserQuota(t),
		"settlement must not re-charge batch quota to the wallet")
}

// TestRealtimeTieredCheaperCumulativeRefundsBatchOvercharge pins the negative
// leg: when the final (e.g. tiered) cumulative billing is cheaper than what
// the batches already charged, the refund reaches the funding source instead
// of being swallowed by clamping at zero.
func TestRealtimeTieredCheaperCumulativeRefundsBatchOvercharge(t *testing.T) {
	setupIdentityTestDB(t)
	const model = "realtime-test-model"
	setupRealtimeRatioSettings(t, model)
	seedRealtimeFixtures(t, 500_000)

	info := realtimeRelayInfo(model)
	ctxRaw, _ := fiberadapter.NewSyntheticContext(nil)

	require.NoError(t, PreWssConsumeQuota(ctxRaw, info, realtimeTextUsage(40)))
	require.Equal(t, 80, info.RealtimeChargedQuota)
	require.Equal(t, 500_000-80, scanUserQuota(t))

	settler := &recordingSettler{preConsumed: 500}
	info.Billing = settler

	// Final cumulative billing totals 50 units of quota (25 in + 25 out).
	PostWssConsumeQuota(ctxRaw, info, model, realtimeTextUsage(25), "")

	// Net actual handed to the session: 50 - 80 = -30 (refund the overcharge),
	// never clamped at zero; the session turns it into a -530 funding delta.
	require.Equal(t, []int{-30}, settler.settledTo)
}

// TestPreWssConsumeQuotaSubscriptionNeedsNoWalletBalance pins the F6 boundary:
// a subscription-funded realtime session with a zero wallet must bill the
// subscription instead of failing the mid-session wallet precheck.
func TestPreWssConsumeQuotaSubscriptionNeedsNoWalletBalance(t *testing.T) {
	setupIdentityTestDB(t)
	const model = "realtime-test-model"
	setupRealtimeRatioSettings(t, model)
	seedRealtimeFixtures(t, 0)
	require.NoError(t, dbx.DB.Create(&UserSubscription{
		Id: 93, UserId: 91, AmountTotal: 1_000_000, AmountUsed: 0, Status: "active",
	}).Error)

	info := realtimeRelayInfo(model)
	info.BillingSource = "subscription"
	info.SubscriptionId = 93
	ctxRaw, _ := fiberadapter.NewSyntheticContext(nil)

	require.NoError(t, PreWssConsumeQuota(ctxRaw, info, realtimeTextUsage(10)))
	require.Equal(t, 20, info.RealtimeChargedQuota)

	var used int64
	require.NoError(t, dbx.DB.Raw("SELECT amount_used FROM user_subscriptions WHERE id = ?", 93).Scan(&used).Error)
	require.Equal(t, int64(20), used)
	require.Equal(t, 0, scanUserQuota(t), "subscription billing must not touch the wallet")
}

// TestPreWssConsumeQuotaRejectsWhenWalletCannotCover keeps the wallet-side
// guard intact after the subscription bypass was added.
func TestPreWssConsumeQuotaRejectsWhenWalletCannotCover(t *testing.T) {
	setupIdentityTestDB(t)
	const model = "realtime-test-model"
	setupRealtimeRatioSettings(t, model)
	seedRealtimeFixtures(t, 10)

	info := realtimeRelayInfo(model)
	ctxRaw, _ := fiberadapter.NewSyntheticContext(nil)

	err := PreWssConsumeQuota(ctxRaw, info, realtimeTextUsage(10))
	require.Error(t, err)
	require.Contains(t, err.Error(), "user quota is not enough")
	require.Equal(t, 0, info.RealtimeChargedQuota)
}

package billing

import (
	"fmt"
	"runtime/debug"
	"testing"

	catalog "github.com/QuantumNous/new-api/internal/catalog"
	ratio_setting "github.com/QuantumNous/new-api/internal/catalog/configure_ratio"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"
	hosttypes "github.com/QuantumNous/new-api/internal/types"
	usagedomain "github.com/QuantumNous/new-api/internal/usage"
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
		// Ratio maps replace wholesale via Update*ByJSONString, so pinning the
		// test model + default group keeps batch quotas deterministic (1:1).
		PriceData: hosttypes.PriceData{ModelRatio: 1},
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
	defer captureInlineFrames(t)
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

// captureInlineFrames expands inlined frames so a panic inside the settle
// plumbing names the real function instead of collapsing into the caller.
func captureInlineFrames(t *testing.T) {
	t.Helper()
	if r := recover(); r != nil {
		t.Fatalf("panic: %v\n%s", r, debug.Stack())
	}
}

func TestRealtimePostWssPanicBisect(t *testing.T) {
	setupIdentityTestDB(t)
	const model = "realtime-test-model"
	setupRealtimeRatioSettings(t, model)
	seedRealtimeFixtures(t, 500_000)
	defer captureInlineFrames(t)

	ctxRaw, _ := fiberadapter.NewSyntheticContext(nil)
	info := realtimeRelayInfo(model)
	usage := realtimeTextUsage(10)
	fmt.Printf("WALK pointers ctx=%p info=%p usage=%p\n", &ctxRaw, info, usage)

	// Statement-by-statement replica of usage.GenerateTextOtherInfo +
	// GenerateWssOtherInfo bodies: the missing marker names the faulting line.
	other := make(map[string]any)
	other["model_ratio"] = 1.0
	fmt.Println("WALK a: map writes")
	other["frt"] = float64(info.FirstResponseTime.UnixMilli() - info.StartTime.UnixMilli())
	fmt.Println("WALK b: frt")
	if info.ReasoningEffort != "" {
		other["reasoning_effort"] = info.ReasoningEffort
	}
	fmt.Println("WALK c: reasoning")
	if info.IsModelMapped {
		other["is_model_mapped"] = true
	}
	fmt.Println("WALK d: model mapped")
	_ = common.GetCtxKeyBool(ctxRaw, constant.ContextKeySystemPromptOverride)
	fmt.Println("WALK e: system prompt bool")
	adminInfo := make(map[string]any)
	adminInfo["use_channel"] = ctxRaw.GetStringSlice("use_channel")
	_ = common.GetCtxKeyBool(ctxRaw, constant.ContextKeyChannelIsMultiKey)
	_ = common.GetCtxKeyInt(ctxRaw, constant.ContextKeyChannelMultiKeyIndex)
	fmt.Println("WALK f: admin ctx reads")
	catalog.AppendChannelAffinityAdminInfo(ctxRaw, adminInfo)
	fmt.Println("WALK g: affinity")
	other["admin_info"] = adminInfo
	if p := ctxRaw.Path(); p != "" {
		other["request_path"] = p
	}
	fmt.Println("WALK h: request path")
	_ = info.GetFinalRequestRelayFormat()
	fmt.Println("WALK i: final format")
	fmt.Println("WALK j: billing src", info.BillingSource, info.UserSetting.BillingPreference)
	fmt.Println("WALK k: chain", len(info.RequestConversionChain), "param", len(info.ParamOverrideAudit), "stream", info.IsStream, info.StreamStatus == nil)
	other["ws"] = true
	other["audio_input"] = usage.InputTokenDetails.AudioTokens
	other["audio_output"] = usage.OutputTokenDetails.AudioTokens
	other["text_input"] = usage.InputTokenDetails.TextTokens
	other["text_output"] = usage.OutputTokenDetails.TextTokens
	fmt.Println("WALK l: wss fields")

	// Real helper, same args: if the replica walked clean but this panics,
	// the fault is not in the statements themselves.
	viaHelper := usagedomain.GenerateWssOtherInfo(ctxRaw, info, usage, 1, 1, 1, 1, 1, 0, 0)
	fmt.Printf("WALK m: GenerateWssOtherInfo ok keys=%d\n", len(viaHelper))
	usagedomain.AttachQuotaSaturation(ctxRaw, info, viaHelper)
	fmt.Println("WALK n: AttachQuotaSaturation ok")
	usagedomain.RecordConsumeLog(ctxRaw, 91, usagedomain.RecordConsumeLogParams{Quota: 5, Other: viaHelper})
	fmt.Println("WALK o: RecordConsumeLog ok")
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

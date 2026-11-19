package billing

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"gorm.io/gorm"
)

// isInviteRechargeSourceEnabled reports whether source is in the admin
// configured invite_reward.recharge_sources list (comma separated, trimmed).
func isInviteRechargeSourceEnabled(source string) bool {
	for _, s := range strings.Split(common.InviteRewardRechargeSources, ",") {
		if strings.TrimSpace(s) == source {
			return true
		}
	}
	return false
}

// isInviteRedemptionNameAllowed enforces invite_reward.redemption_code_names.
// An empty whitelist means no redemption code counts as a recharge, so an
// admin who enables redemption but forgets to pick types cannot accidentally
// grant rewards for every code. Otherwise the name must match exactly.
func isInviteRedemptionNameAllowed(name string) bool {
	hasWhitelist := false
	for _, n := range strings.Split(common.InviteRewardRedemptionCodeNames, ",") {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		hasWhitelist = true
		if n == name {
			return true
		}
	}
	return hasWhitelist
}

// rechargeSource classifies a TopUp for the invite recharge rebate. Wallet
// top-ups carry a payment provider; subscription purchases created by the
// billing system do not.
func rechargeSource(topUp *TopUp) string {
	if topUp.PaymentProvider != "" {
		return common.InviteRewardRechargeSourceTopup
	}
	return common.InviteRewardRechargeSourceSubscription
}

// applyInviteRechargeRewards pays the inviter's recharge rebate after a
// successful recharge. source is one of the InviteRewardRechargeSource*
// values; redemptionName is only consulted when source is "redemption".
//
// Three sources are supported: wallet top-ups, subscription purchases and
// allowlisted redemption codes. A recharge is eligible only when its source
// appears in invite_reward.recharge_sources (and, for redemption, its name
// matches invite_reward.redemption_code_names). Every eligible recharge
// records an InviteRewardEvent; the "first" trigger rewards only when no
// earlier event exists for the invitee, while "every" rewards each time.
// The invitee no longer receives a recharge rebate; their QuotaForInvitee is
// granted at registration by applyInviteRewards. All writes run inside the
// caller's transaction.
func applyInviteRechargeRewards(tx *gorm.DB, inviteeID int, creditedQuota int, source string, redemptionName string) error {
	if common.InviteRewardInviterMode != common.InviteRewardModeRecharge {
		return nil
	}
	if !IsPaymentComplianceConfirmed() {
		return nil
	}
	if !isInviteRechargeSourceEnabled(source) {
		return nil
	}
	if source == common.InviteRewardRechargeSourceRedemption && !isInviteRedemptionNameAllowed(redemptionName) {
		return nil
	}

	// Lock the invitee row so concurrent callbacks for the same invitee
	// serialize the first-recharge count. SQLite skips FOR UPDATE but its
	// single-writer transactions still serialize the competing write.
	var invitee identity.User
	if err := dbx.LockForUpdate(tx).Model(&identity.User{}).
		Select("inviter_id, created_at, invite_reward_direct_paid").
		Where("id = ?", inviteeID).
		First(&invitee).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if invitee.InviterId == 0 {
		return nil
	}

	// A reward already paid at registration must not be paid again if the
	// admin later switches the mode to recharge.
	if invitee.InviteRewardDirectPaid {
		return nil
	}

	// "first" rewards only the first eligible recharge. Count before inserting
	// this event so the current recharge is not counted against itself.
	shouldReward := true
	if common.InviteRewardRebateTrigger == common.InviteRewardTriggerFirst {
		var count int64
		if err := tx.Model(&InviteRewardEvent{}).
			Where("user_id = ?", inviteeID).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			shouldReward = false
		}
		// Recharges that predate this table were counted from TopUp rows. A
		// fresh event table otherwise makes an already-rewarded invitee look
		// eligible for a second "first" rebate after an upgrade.
		if count == 0 {
			var legacyCount int64
			if err := tx.Model(&TopUp{}).
				Where("user_id = ? AND status = ? AND payment_provider <> ? AND create_time >= ?",
					inviteeID, common.TopUpStatusSuccess, "", invitee.CreatedAt).
				Count(&legacyCount).Error; err != nil {
				return err
			}
			// The current wallet top-up is already successful inside this
			// transaction, while events exclude it; discount it here.
			if source == common.InviteRewardRechargeSourceTopup && legacyCount > 0 {
				legacyCount--
			}
			if legacyCount > 0 {
				shouldReward = false
			}
		}
	}

	// Record every eligible recharge so the "first" trigger counts across all
	// enabled sources instead of only wallet TopUps.
	if err := tx.Create(&InviteRewardEvent{
		UserId:         inviteeID,
		Source:         source,
		RedemptionName: redemptionName,
		Quota:          creditedQuota,
		CreatedAt:      common.GetTimestamp(),
	}).Error; err != nil {
		return err
	}

	if !shouldReward {
		return nil
	}

	rewardQuota := common.QuotaFromFloat(float64(creditedQuota) * common.InviteRewardRebateMultiplier)
	if rewardQuota <= 0 {
		return nil
	}

	if err := tx.Model(&identity.User{}).Where("id = ?", invitee.InviterId).Updates(map[string]interface{}{
		"aff_quota":   gorm.Expr("aff_quota + ?", rewardQuota),
		"aff_history": gorm.Expr("aff_history + ?", rewardQuota),
	}).Error; err != nil {
		return err
	}

	common.SysLog(fmt.Sprintf(
		"invite recharge reward: invitee_id=%d inviter_id=%d reward_quota=%d inviter_mode=%s trigger=%s source=%s redemption_name=%s",
		inviteeID, invitee.InviterId, rewardQuota,
		common.InviteRewardInviterMode, common.InviteRewardRebateTrigger,
		source, redemptionName,
	))

	return nil
}

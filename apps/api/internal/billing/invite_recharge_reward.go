package billing

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"gorm.io/gorm"
)

// applyInviteRechargeRewards pays the inviter's recharge rebate after a
// successful top-up. It is a no-op unless InviteRewardInviterMode is
// "recharge". The invitee no longer receives a recharge rebate; their
// QuotaForInvitee is granted at registration by applyInviteRewards.
// All writes run inside the caller's transaction.
func applyInviteRechargeRewards(tx *gorm.DB, inviteeID int, creditedQuota int) error {
	if common.InviteRewardInviterMode != common.InviteRewardModeRecharge {
		return nil
	}
	if !IsPaymentComplianceConfirmed() {
		return nil
	}

	// Lock the invitee row so concurrent callbacks for the same invitee
	// serialize the first-recharge count. SQLite skips FOR UPDATE but its
	// single-writer transactions still serialize the competing write.
	var invitee identity.User
	if err := dbx.LockForUpdate(tx).Model(&identity.User{}).
		Select("inviter_id").
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

	// The current order is already marked successful inside the transaction,
	// so a count above one means the invitee has recharged before.
	if common.InviteRewardRebateTrigger == common.InviteRewardTriggerFirst {
		var count int64
		if err := tx.Model(&TopUp{}).
			Where("user_id = ? AND status = ?", inviteeID, common.TopUpStatusSuccess).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 1 {
			return nil
		}
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
		"invite recharge reward: invitee_id=%d inviter_id=%d reward_quota=%d inviter_mode=%s trigger=%s",
		inviteeID, invitee.InviterId, rewardQuota,
		common.InviteRewardInviterMode, common.InviteRewardRebateTrigger,
	))

	return nil
}

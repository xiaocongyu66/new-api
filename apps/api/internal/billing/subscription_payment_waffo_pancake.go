package billing

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/billing/pay_subscription"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/shopspring/decimal"
	"github.com/thanhpk/randstr"
)

type SubscriptionWaffoPancakePayRequest struct {
	PlanId int `json:"plan_id"`
}

func SubscriptionRequestWaffoPancakePay(c contract.Context) {
	if !RequirePaymentCompliance(c) {
		return
	}

	var req SubscriptionWaffoPancakePayRequest
	if err := c.BindJSON(&req); err != nil || req.PlanId <= 0 {
		common.CtxApiErrorMsg(c, "参数错误")
		return
	}

	plan, err := GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if !plan.Enabled {
		common.CtxApiErrorMsg(c, "套餐未启用")
		return
	}
	if strings.TrimSpace(plan.WaffoPancakeProductId) == "" {
		common.CtxApiErrorMsg(c, "该套餐未配置 WaffoPancakeProductId")
		return
	}
	// Plan targets its own Pancake product, so we only require credentials
	// here — not the gateway-level WaffoPancakeProductID.
	if strings.TrimSpace(pay_subscription.WaffoPancakeMerchantID) == "" ||
		strings.TrimSpace(pay_subscription.WaffoPancakePrivateKey) == "" {
		common.CtxApiErrorMsg(c, "Waffo Pancake 未配置或密钥无效")
		return
	}

	userId := c.GetInt("id")
	user, err := identity.GetUserById(userId, false)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if user == nil {
		common.CtxApiErrorMsg(c, "用户不存在")
		return
	}

	if plan.MaxPurchasePerUser > 0 {
		count, err := CountUserSubscriptionsByPlan(userId, plan.Id)
		if err != nil {
			common.CtxApiError(c, err)
			return
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			common.CtxApiErrorMsg(c, "已达到该套餐购买上限")
			return
		}
	}

	// WAFFO_PANCAKE_SUB- prefix (vs. wallet's WAFFO_PANCAKE-) drives webhook
	// dispatch in WaffoPancakeWebhook.
	tradeNo := fmt.Sprintf("WAFFO_PANCAKE_SUB-%d-%d-%s", userId, time.Now().UnixMilli(), randstr.String(6))

	order := &SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         tradeNo,
		PaymentMethod:   PaymentMethodWaffoPancake,
		PaymentProvider: PaymentProviderWaffoPancake,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := order.Insert(); err != nil {
		logger.LogError(c.Context(), fmt.Sprintf("Waffo Pancake 订阅订单创建失败 user_id=%d plan_id=%d trade_no=%s error=%q", userId, plan.Id, tradeNo, err.Error()))
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": "创建订单失败"})
		return
	}

	expiresInSeconds := 45 * 60
	session, err := CreateWaffoPancakeCheckoutSession(c.Context(), &WaffoPancakeCreateSessionParams{
		ProductID:     plan.WaffoPancakeProductId,
		BuyerIdentity: WaffoPancakeBuyerIdentityFromUserID(user.Id),
		PriceSnapshot: &WaffoPancakePriceSnapshot{
			Amount:      decimal.NewFromFloat(plan.PriceAmount).StringFixed(2),
			TaxCategory: "saas",
		},
		BuyerEmail:              getWaffoPancakeBuyerEmail(user),
		ExpiresInSeconds:        &expiresInSeconds,
		OrderMerchantExternalID: tradeNo,
	})
	if err != nil {
		logger.LogError(c.Context(), fmt.Sprintf("Waffo Pancake 订阅结账会话创建失败 user_id=%d plan_id=%d trade_no=%s error=%q", userId, plan.Id, tradeNo, err.Error()))
		order.Status = common.TopUpStatusFailed
		_ = order.Update()
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": "拉起支付失败"})
		return
	}
	logger.LogInfo(c.Context(), fmt.Sprintf("Waffo Pancake 订阅订单创建成功 user_id=%d plan_id=%d trade_no=%s session_id=%s money=%.2f", userId, plan.Id, tradeNo, session.SessionID, plan.PriceAmount))

	_ = c.JSON(http.StatusOK, common.H{
		"message": "success",
		"data": common.H{
			"checkout_url":     session.CheckoutURL,
			"session_id":       session.SessionID,
			"expires_at":       session.ExpiresAt,
			"order_id":         tradeNo,
			"token":            session.Token,
			"token_expires_at": session.TokenExpiresAt,
		},
	})
}

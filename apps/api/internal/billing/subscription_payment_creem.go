package billing

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/internal/billing/manage_subscription"
	"github.com/QuantumNous/new-api/internal/billing/pay_subscription"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/thanhpk/randstr"
)

type SubscriptionCreemPayRequest struct {
	PlanId int `json:"plan_id"`
}

func SubscriptionRequestCreemPay(c contract.Context) {
	if !RequirePaymentCompliance(c) {
		return
	}

	var req SubscriptionCreemPayRequest

	// Keep body for debugging consistency (like RequestCreemPay)
	if _, err := c.RawBody(); err != nil {
		logger.LogError(c.Context(), fmt.Sprintf("Creem 订阅支付请求读取失败 error=%q", err.Error()))
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": "read query error"})
		return
	}

	if err := c.BindJSON(&req); err != nil || req.PlanId <= 0 {
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": "参数错误"})
		return
	}

	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if !plan.Enabled {
		common.CtxApiErrorMsg(c, "套餐未启用")
		return
	}
	if plan.CreemProductId == "" {
		common.CtxApiErrorMsg(c, "该套餐未配置 CreemProductId")
		return
	}
	if pay_subscription.CreemWebhookSecret == "" && !pay_subscription.CreemTestMode {
		common.CtxApiErrorMsg(c, "Creem Webhook 未配置")
		return
	}

	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if user == nil {
		common.CtxApiErrorMsg(c, "用户不存在")
		return
	}

	if plan.MaxPurchasePerUser > 0 {
		count, err := model.CountUserSubscriptionsByPlan(userId, plan.Id)
		if err != nil {
			common.CtxApiError(c, err)
			return
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			common.CtxApiErrorMsg(c, "已达到该套餐购买上限")
			return
		}
	}

	reference := "sub-creem-ref-" + randstr.String(6)
	referenceId := "sub_ref_" + common.Sha1([]byte(reference+time.Now().String()+user.Username))

	// create pending order first
	order := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         referenceId,
		PaymentMethod:   model.PaymentMethodCreem,
		PaymentProvider: model.PaymentProviderCreem,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := order.Insert(); err != nil {
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": "创建订单失败"})
		return
	}

	// Reuse Creem checkout generator by building a lightweight product reference.
	currency := "USD"
	switch manage_subscription.GetGeneralSetting().QuotaDisplayType {
	case manage_subscription.QuotaDisplayTypeCNY:
		currency = "CNY"
	case manage_subscription.QuotaDisplayTypeUSD:
		currency = "USD"
	default:
		currency = "USD"
	}
	product := &CreemProduct{
		ProductId: plan.CreemProductId,
		Name:      plan.Title,
		Price:     plan.PriceAmount,
		Currency:  currency,
		Quota:     0,
	}

	checkoutUrl, err := genCreemLink(c.Context(), referenceId, product, user.Email, user.Username)
	if err != nil {
		logger.LogError(c.Context(), fmt.Sprintf("Creem 订阅支付链接创建失败 trade_no=%s product_id=%s error=%q", referenceId, product.ProductId, err.Error()))
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	_ = c.JSON(http.StatusOK, common.H{
		"message": "success",
		"data": common.H{
			"checkout_url": checkoutUrl,
			"order_id":     referenceId,
		},
	})
}

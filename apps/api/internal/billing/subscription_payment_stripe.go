package billing

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/thanhpk/randstr"
)

type SubscriptionStripePayRequest struct {
	PlanId int `json:"plan_id"`
}

func SubscriptionRequestStripePay(c contract.Context) {
	if !RequirePaymentCompliance(c) {
		return
	}

	var req SubscriptionStripePayRequest
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
	if plan.StripePriceId == "" {
		common.CtxApiErrorMsg(c, "该套餐未配置 StripePriceId")
		return
	}
	if !strings.HasPrefix(StripeApiSecret, "sk_") && !strings.HasPrefix(StripeApiSecret, "rk_") {
		common.CtxApiErrorMsg(c, "Stripe 未配置或密钥无效")
		return
	}
	if StripeWebhookSecret == "" {
		common.CtxApiErrorMsg(c, "Stripe Webhook 未配置")
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

	reference := fmt.Sprintf("sub-stripe-ref-%d-%d-%s", user.Id, time.Now().UnixMilli(), randstr.String(4))
	referenceId := "sub_ref_" + common.Sha1([]byte(reference))

	payLink, err := genStripeSubscriptionLink(referenceId, user.StripeCustomer, user.Email, plan.StripePriceId)
	if err != nil {
		logger.LogError(c.Context(), fmt.Sprintf("Stripe 订阅支付链接创建失败 trade_no=%s plan_id=%d error=%q", referenceId, plan.Id, err.Error()))
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	order := &SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         referenceId,
		PaymentMethod:   PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := order.Insert(); err != nil {
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": "创建订单失败"})
		return
	}

	_ = c.JSON(http.StatusOK, common.H{
		"message": "success",
		"data": common.H{
			"pay_link": payLink,
		},
	})
}

func genStripeSubscriptionLink(referenceId string, customerId string, email string, priceId string) (string, error) {
	stripe.Key = StripeApiSecret

	params := &stripe.CheckoutSessionParams{
		ClientReferenceID: stripe.String(referenceId),
		SuccessURL:        stripe.String(paymentReturnPath("/wallet")),
		CancelURL:         stripe.String(paymentReturnPath("/wallet")),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceId),
				Quantity: stripe.Int64(1),
			},
		},
		Mode: stripe.String(string(stripe.CheckoutSessionModeSubscription)),
	}

	if "" == customerId {
		if "" != email {
			params.CustomerEmail = stripe.String(email)
		}
		params.CustomerCreation = stripe.String(string(stripe.CheckoutSessionCustomerCreationAlways))
	} else {
		params.Customer = stripe.String(customerId)
	}

	result, err := session.New(params)
	if err != nil {
		return "", err
	}
	return result.URL, nil
}

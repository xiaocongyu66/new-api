package billing

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type SubscriptionCodePayPayRequest struct {
	PlanId        int    `json:"plan_id"`
	PaymentMethod string `json:"payment_method"`
}

// SubscriptionRequestCodePayPay 发起 码支付 订阅支付，返回支付网关跳转 URL。
func SubscriptionRequestCodePayPay(c contract.Context) {
	if !RequirePaymentCompliance(c) {
		return
	}
	if !isCodePayTopUpEnabled() {
		common.CtxApiErrorMsg(c, "码支付 未启用")
		return
	}

	var req SubscriptionCodePayPayRequest
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
	if plan.PriceAmount < 0.01 {
		common.CtxApiErrorMsg(c, "套餐金额过低")
		return
	}
	if req.PaymentMethod != PaymentMethodCodePayAlipay && req.PaymentMethod != PaymentMethodCodePayWxpay {
		common.CtxApiErrorMsg(c, "支付方式不存在")
		return
	}

	userId := c.GetInt("id")
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

	callBackAddress := GetCallbackAddress()
	returnUrl, err := url.Parse(paymentReturnPath("/wallet?pay=pending"))
	if err != nil {
		common.CtxApiErrorMsg(c, "回调地址配置错误")
		return
	}
	notifyUrl, err := url.Parse(callBackAddress + "/api/subscription/codepay/notify")
	if err != nil {
		common.CtxApiErrorMsg(c, "回调地址配置错误")
		return
	}

	tradeNo := fmt.Sprintf("%s%d", common.GetRandomString(6), time.Now().Unix())
	tradeNo = fmt.Sprintf("SUBCPUSR%dNO%s", userId, tradeNo)

	// 码支付 type: 1=支付宝, 3=微信
	payType := "1"
	if req.PaymentMethod == PaymentMethodCodePayWxpay {
		payType = "3"
	}
	priceStr := strconv.FormatFloat(plan.PriceAmount, 'f', 2, 64)
	cpParams := map[string]string{
		"id":         CodePayId,
		"type":       payType,
		"price":      priceStr,
		"pay_id":     tradeNo,
		"param":      "sub",
		"notify_url": notifyUrl.String(),
		"return_url": returnUrl.String(),
	}
	sign := codepaySign(cpParams, CodePayKey)

	var queryParts []string
	keys := make([]string, 0, len(cpParams))
	for k := range cpParams {
		if k == "sign" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		queryParts = append(queryParts, k+"="+url.QueryEscape(cpParams[k]))
	}
	queryParts = append(queryParts, "sign="+url.QueryEscape(sign))

	codepayUrl := strings.TrimRight(CodePayAddress, "/") + "/creat_order/?" + strings.Join(queryParts, "&")

	order := &SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         tradeNo,
		PaymentMethod:   req.PaymentMethod,
		PaymentProvider: PaymentProviderCodePay,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	if err := order.Insert(); err != nil {
		common.CtxApiError(c, err)
		return
	}

	_ = c.JSON(http.StatusOK, common.H{"message": "success", "data": map[string]string{"trade_no": tradeNo}, "url": codepayUrl})
}

// SubscriptionCodePayNotify 处理 码支付 订阅异步通知。
func SubscriptionCodePayNotify(c contract.Context) {
	if !isCodePayWebhookEnabled() {
		_ = c.String(http.StatusOK, "fail")
		return
	}

	var params map[string]string
	if c.Method() == "POST" {
		if err := c.ParseForm(); err != nil {
			_ = c.String(http.StatusOK, "fail")
			return
		}
		params = lo.Reduce(lo.Keys(c.PostFormValues()), func(r map[string]string, t string, i int) map[string]string {
			r[t] = c.PostForm(t)
			return r
		}, map[string]string{})
	} else {
		params = lo.Reduce(lo.Keys(c.QueryValues()), func(r map[string]string, t string, i int) map[string]string {
			r[t] = c.Query(t)
			return r
		}, map[string]string{})
	}

	if len(params) == 0 {
		_ = c.String(http.StatusOK, "fail")
		return
	}
	if strings.TrimSpace(params["pay_no"]) == "" {
		_ = c.String(http.StatusOK, "fail")
		return
	}

	sign := params["sign"]
	expected := codepaySign(params, CodePayKey)
	if !constantTimeCompare(sign, expected) {
		_ = c.String(http.StatusOK, "fail")
		return
	}

	tradeNo := params["pay_id"]
	if tradeNo == "" {
		_ = c.String(http.StatusOK, "fail")
		return
	}

	order := GetSubscriptionOrderByTradeNo(tradeNo)
	if order == nil {
		_ = c.String(http.StatusOK, "fail")
		return
	}
	if order.PaymentProvider != PaymentProviderCodePay {
		_ = c.String(http.StatusOK, "fail")
		return
	}

	notifyMoney, parseErr := decimal.NewFromString(params["price"])
	orderMoney := decimal.NewFromFloat(order.Money).Round(2)
	if parseErr != nil || !notifyMoney.Round(2).Equals(orderMoney) {
		_ = c.String(http.StatusOK, "fail")
		return
	}

	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	if err := CompleteSubscriptionOrder(tradeNo, common.GetJsonString(params), PaymentProviderCodePay, ""); err != nil {
		_ = c.String(http.StatusOK, "fail")
		return
	}

	_ = c.String(http.StatusOK, "success")
}

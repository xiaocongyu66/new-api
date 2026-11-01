package billing

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type SubscriptionVmqPayRequest struct {
	PlanId        int    `json:"plan_id"`
	PaymentMethod string `json:"payment_method"`
}

// SubscriptionRequestVmqPay 发起 V免签 订阅支付，返回支付网关跳转 URL。
func SubscriptionRequestVmqPay(c contract.Context) {
	if !RequirePaymentCompliance(c) {
		return
	}
	if !isVmqGatewayConfigured() {
		common.CtxApiErrorMsg(c, "当前管理员未配置支付信息")
		return
	}

	var req SubscriptionVmqPayRequest
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

	method, ok := findPayMethod(req.PaymentMethod, "vmq")
	if !ok {
		common.CtxApiErrorMsg(c, "支付方式不存在")
		return
	}
	typeCode, ok := gatewayPayTypeCode("vmq", method["type"])
	if !ok {
		common.CtxApiErrorMsg(c, "不支持的支付方式")
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
	returnUrl := paymentReturnPath("/wallet?pay=pending")
	notifyUrl := callBackAddress + "/api/subscription/vmq/notify"

	tradeNo := fmt.Sprintf("%s%d", common.GetRandomString(6), time.Now().Unix())
	tradeNo = fmt.Sprintf("SUBVMQUSR%dNO%s", userId, tradeNo)

	priceStr := strconv.FormatFloat(plan.PriceAmount, 'f', 2, 64)
	sign := vmqCreateSign(tradeNo, "sub", typeCode, priceStr, VmqKey)

	vmqUrl := strings.TrimRight(VmqAddress, "/") + "/createOrder?" +
		"payId=" + url.QueryEscape(tradeNo) +
		"&param=" + url.QueryEscape("sub") +
		"&type=" + url.QueryEscape(typeCode) +
		"&price=" + url.QueryEscape(priceStr) +
		"&notifyUrl=" + url.QueryEscape(notifyUrl) +
		"&returnUrl=" + url.QueryEscape(returnUrl) +
		"&sign=" + url.QueryEscape(sign) +
		"&isHtml=1"

	order := &SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         tradeNo,
		PaymentMethod:   req.PaymentMethod,
		PaymentProvider: PaymentProviderVmq,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	if err := order.Insert(); err != nil {
		common.CtxApiError(c, err)
		return
	}

	_ = c.JSON(http.StatusOK, common.H{"message": "success", "data": map[string]string{"trade_no": tradeNo}, "url": vmqUrl})
}

// SubscriptionVmqNotify 处理 V免签 订阅异步通知。
func SubscriptionVmqNotify(c contract.Context) {
	if !isVmqWebhookEnabled() {
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

	sign := params["sign"]
	expected := vmqNotifySign(params["payId"], params["param"], params["type"], params["price"], params["reallyPrice"], VmqKey)
	if !constantTimeCompare(sign, expected) {
		_ = c.String(http.StatusOK, "fail")
		return
	}

	tradeNo := params["payId"]
	if tradeNo == "" {
		_ = c.String(http.StatusOK, "fail")
		return
	}

	order := GetSubscriptionOrderByTradeNo(tradeNo)
	if order == nil {
		_ = c.String(http.StatusOK, "fail")
		return
	}
	if order.PaymentProvider != PaymentProviderVmq {
		_ = c.String(http.StatusOK, "fail")
		return
	}

	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	notifyMoney, parseErr := decimal.NewFromString(params["price"])
	orderMoney := decimal.NewFromFloat(order.Money).Round(2)
	if parseErr != nil || !notifyMoney.Round(2).Equals(orderMoney) {
		_ = c.String(http.StatusOK, "fail")
		return
	}
	if reallyPrice := params["reallyPrice"]; reallyPrice != "" {
		paidMoney, perr := decimal.NewFromString(reallyPrice)
		if perr != nil {
			_ = c.String(http.StatusOK, "fail")
			return
		}
		diff := paidMoney.Round(2).Sub(orderMoney).Abs()
		if diff.GreaterThan(decimal.NewFromFloat(0.10)) {
			_ = c.String(http.StatusOK, "fail")
			return
		}
	}

	if err := CompleteSubscriptionOrder(tradeNo, common.GetJsonString(params), PaymentProviderVmq, ""); err != nil {
		_ = c.String(http.StatusOK, "fail")
		return
	}

	_ = c.String(http.StatusOK, "success")
}

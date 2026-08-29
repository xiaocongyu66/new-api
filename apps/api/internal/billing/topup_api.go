package billing

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/internal/billing/manage_subscription"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/internal/billing/pay_subscription"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/logger"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

func GetTopUpInfo(c contract.Context) {
	complianceConfirmed := pay_subscription.IsPaymentComplianceConfirmed()

	// 获取支付方式
	payMethods := pay_subscription.PayMethods
	if !complianceConfirmed {
		payMethods = []map[string]string{}
	}

	// 如果启用了 Stripe 支付，添加到支付方法列表
	if isStripeTopUpEnabled() {
		// 检查是否已经包含 Stripe
		hasStripe := false
		for _, method := range payMethods {
			if method["type"] == "stripe" {
				hasStripe = true
				break
			}
		}

		if !hasStripe {
			stripeMethod := map[string]string{
				"name":      "Stripe",
				"type":      "stripe",
				"color":     "#635BFF",
				"min_topup": strconv.Itoa(pay_subscription.StripeMinTopUp),
			}
			payMethods = append(payMethods, stripeMethod)
		}
	}

	// Waffo Pancake is displayed above the standard Waffo gateway.
	enableWaffoPancake := isWaffoPancakeTopUpEnabled()
	if enableWaffoPancake {
		hasWaffoPancake := false
		for _, method := range payMethods {
			if method["type"] == PaymentMethodWaffoPancake {
				hasWaffoPancake = true
				break
			}
		}

		if !hasWaffoPancake {
			payMethods = append(payMethods, map[string]string{
				"name":      "Waffo Pancake",
				"type":      PaymentMethodWaffoPancake,
				"color":     "#F97316",
				"min_topup": strconv.Itoa(pay_subscription.WaffoPancakeMinTopUp),
			})
		}
	}

	// 如果启用了 Waffo 支付，添加到支付方法列表
	enableWaffo := isWaffoTopUpEnabled()
	if enableWaffo {
		hasWaffo := false
		for _, method := range payMethods {
			if method["type"] == PaymentMethodWaffo {
				hasWaffo = true
				break
			}
		}

		if !hasWaffo {
			waffoMethod := map[string]string{
				"name":      "Waffo (Global Payment)",
				"type":      PaymentMethodWaffo,
				"color":     "#3B82F6",
				"min_topup": strconv.Itoa(pay_subscription.WaffoMinTopUp),
			}
			payMethods = append(payMethods, waffoMethod)
		}
	}

	data := common.H{
		"enable_online_topup":              isEpayTopUpEnabled(),
		"enable_stripe_topup":              isStripeTopUpEnabled(),
		"enable_creem_topup":               isCreemTopUpEnabled(),
		"enable_waffo_topup":               enableWaffo,
		"enable_waffo_pancake_topup":       enableWaffoPancake,
		"enable_redemption":                complianceConfirmed,
		"payment_compliance_confirmed":     complianceConfirmed,
		"payment_compliance_terms_version": pay_subscription.CurrentComplianceTermsVersion,
		"waffo_pay_methods": func() interface{} {
			if enableWaffo {
				return pay_subscription.GetWaffoPayMethods()
			}
			return nil
		}(),
		"creem_products":          pay_subscription.CreemProducts,
		"pay_methods":             payMethods,
		"min_topup":               pay_subscription.MinTopUp,
		"stripe_min_topup":        pay_subscription.StripeMinTopUp,
		"waffo_min_topup":         pay_subscription.WaffoMinTopUp,
		"waffo_pancake_min_topup": pay_subscription.WaffoPancakeMinTopUp,
		"amount_options":          pay_subscription.GetPaymentSetting().AmountOptions,
		"discount":                pay_subscription.GetPaymentSetting().AmountDiscount,
		"topup_link":              common.TopUpLink,
	}
	common.CtxApiSuccess(c, data)
}

type EpayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
}

type AmountRequest struct {
	Amount int64 `json:"amount"`
}

func GetEpayClient() *epay.Client {
	if pay_subscription.PayAddress == "" || pay_subscription.EpayId == "" || pay_subscription.EpayKey == "" {
		return nil
	}
	withUrl, err := epay.NewClient(&epay.Config{
		PartnerID: pay_subscription.EpayId,
		Key:       pay_subscription.EpayKey,
	}, pay_subscription.PayAddress)
	if err != nil {
		return nil
	}
	return withUrl
}

func getPayMoney(amount int64, group string) float64 {
	dAmount := decimal.NewFromInt(amount)
	// 充值金额以“展示类型”为准：
	// - USD/CNY: 前端传 amount 为金额单位；TOKENS: 前端传 tokens，需要换成 USD 金额
	if manage_subscription.GetQuotaDisplayType() == manage_subscription.QuotaDisplayTypeTokens {
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		dAmount = dAmount.Div(dQuotaPerUnit)
	}

	topupGroupRatio := common.GetTopupGroupRatio(group)
	if topupGroupRatio == 0 {
		topupGroupRatio = 1
	}

	dTopupGroupRatio := decimal.NewFromFloat(topupGroupRatio)
	dPrice := decimal.NewFromFloat(pay_subscription.Price)
	// apply optional preset discount by the original request amount (if configured), default 1.0
	discount := 1.0
	if ds, ok := pay_subscription.GetPaymentSetting().AmountDiscount[int(amount)]; ok {
		if ds > 0 {
			discount = ds
		}
	}
	dDiscount := decimal.NewFromFloat(discount)

	payMoney := dAmount.Mul(dPrice).Mul(dTopupGroupRatio).Mul(dDiscount)

	return payMoney.InexactFloat64()
}

func getMinTopup() int64 {
	minTopup := pay_subscription.MinTopUp
	if manage_subscription.GetQuotaDisplayType() == manage_subscription.QuotaDisplayTypeTokens {
		dMinTopup := decimal.NewFromInt(int64(minTopup))
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		minTopup = common.QuotaFromDecimal(dMinTopup.Mul(dQuotaPerUnit))
	}
	return int64(minTopup)
}

func getTopUpQuota(amount int64) (int, error) {
	quota := decimal.NewFromInt(amount)
	if manage_subscription.GetQuotaDisplayType() == manage_subscription.QuotaDisplayTypeTokens {
		quotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		quota = decimal.NewFromInt(quota.Div(quotaPerUnit).IntPart()).Mul(quotaPerUnit)
	} else {
		quota = quota.Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	}
	return common.QuotaFromDecimalStrict(quota)
}

func getMaxTopUpAmount() int64 {
	if common.QuotaPerUnit <= 0 {
		return 0
	}
	quotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
	maxStoredAmount := decimal.NewFromInt(common.MaxQuota - 1).
		Div(quotaPerUnit).
		Floor()
	if manage_subscription.GetQuotaDisplayType() == manage_subscription.QuotaDisplayTypeTokens {
		return maxStoredAmount.Add(decimal.NewFromInt(1)).
			Mul(quotaPerUnit).
			Ceil().
			Sub(decimal.NewFromInt(1)).
			IntPart()
	}
	return maxStoredAmount.IntPart()
}

func validateCreditedQuota(quota decimal.Decimal) (int, error) {
	value, err := common.QuotaFromDecimalStrict(quota)
	if err != nil {
		return 0, errors.New("充值额度超出系统可表示范围")
	}
	if value <= 0 {
		return 0, errors.New("充值额度必须大于 0")
	}
	return value, nil
}

func validateTopUpQuota(amount int64) (int, error) {
	quota, err := getTopUpQuota(amount)
	if err == nil && quota > 0 {
		return quota, nil
	}
	maxAmount := getMaxTopUpAmount()
	if maxAmount > 0 && amount > maxAmount {
		return 0, fmt.Errorf("单笔充值数量不能大于 %d", maxAmount)
	}
	return 0, errors.New("充值数量无效")
}

func rejectInvalidCreditedQuota(c contract.Context, userId int, quota decimal.Decimal) bool {
	creditedQuota, err := validateCreditedQuota(quota)
	if err == nil {
		err = ValidateTopUpQuotaCapacity(userId, creditedQuota)
	}
	if err != nil {
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": err.Error()})
		return true
	}
	return false
}

func rejectInvalidTopUpQuota(c contract.Context, userId int, amount int64) bool {
	creditedQuota, err := validateTopUpQuota(amount)
	if err == nil {
		err = ValidateTopUpQuotaCapacity(userId, creditedQuota)
	}
	if err != nil {
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": err.Error()})
		return true
	}
	return false
}

func RequestEpay(c contract.Context) {
	var req EpayRequest
	err := c.BindJSON(&req)
	if err != nil {
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": "参数错误"})
		return
	}
	if req.Amount < getMinTopup() {
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", getMinTopup())})
		return
	}
	id := c.GetInt("id")
	if rejectInvalidTopUpQuota(c, id, req.Amount) {
		return
	}

	group, err := identity.GetUserGroup(id, true)
	if err != nil {
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	payMoney := getPayMoney(req.Amount, group)
	if payMoney < 0.01 {
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": "充值金额过低"})
		return
	}

	if !pay_subscription.ContainsPayMethod(req.PaymentMethod) {
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": "支付方式不存在"})
		return
	}

	callBackAddress := GetCallbackAddress()
	returnUrl, _ := url.Parse(paymentReturnPath("/usage-logs"))
	notifyUrl, _ := url.Parse(callBackAddress + "/api/user/epay/notify")
	tradeNo := fmt.Sprintf("%s%d", common.GetRandomString(6), time.Now().Unix())
	tradeNo = fmt.Sprintf("USR%dNO%s", id, tradeNo)
	client := GetEpayClient()
	if client == nil {
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": "当前管理员未配置支付信息"})
		return
	}
	uri, params, err := client.Purchase(&epay.PurchaseArgs{
		Type:           req.PaymentMethod,
		ServiceTradeNo: tradeNo,
		Name:           fmt.Sprintf("TUC%d", req.Amount),
		Money:          strconv.FormatFloat(payMoney, 'f', 2, 64),
		Device:         epay.PC,
		NotifyUrl:      notifyUrl,
		ReturnUrl:      returnUrl,
	})
	if err != nil {
		logger.LogError(c.Context(), fmt.Sprintf("易支付 拉起支付失败 user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", id, tradeNo, req.PaymentMethod, req.Amount, err.Error()))
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": "拉起支付失败"})
		return
	}
	amount := req.Amount
	if manage_subscription.GetQuotaDisplayType() == manage_subscription.QuotaDisplayTypeTokens {
		dAmount := decimal.NewFromInt(int64(amount))
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		amount = dAmount.Div(dQuotaPerUnit).IntPart()
	}
	topUp := &TopUp{
		UserId:          id,
		Amount:          amount,
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   req.PaymentMethod,
		PaymentProvider: PaymentProviderEpay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	err = topUp.Insert()
	if err != nil {
		logger.LogError(c.Context(), fmt.Sprintf("易支付 创建充值订单失败 user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", id, tradeNo, req.PaymentMethod, req.Amount, err.Error()))
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": "创建订单失败"})
		return
	}
	logger.LogInfo(c.Context(), fmt.Sprintf("易支付 充值订单创建成功 user_id=%d trade_no=%s payment_method=%s amount=%d money=%.2f uri=%q params=%q", id, tradeNo, req.PaymentMethod, req.Amount, payMoney, uri, common.GetJsonString(params)))
	_ = c.JSON(http.StatusOK, common.H{"message": "success", "data": params, "url": uri})
}

// tradeNo lock
var orderLocks sync.Map
var createLock sync.Mutex

// refCountedMutex 带引用计数的互斥锁，确保最后一个使用者才从 map 中删除
type refCountedMutex struct {
	mu       sync.Mutex
	refCount int
}

// LockOrder 尝试对给定订单号加锁
func LockOrder(tradeNo string) {
	createLock.Lock()
	var rcm *refCountedMutex
	if v, ok := orderLocks.Load(tradeNo); ok {
		rcm = v.(*refCountedMutex)
	} else {
		rcm = &refCountedMutex{}
		orderLocks.Store(tradeNo, rcm)
	}
	rcm.refCount++
	createLock.Unlock()
	rcm.mu.Lock()
}

// UnlockOrder 释放给定订单号的锁
func UnlockOrder(tradeNo string) {
	v, ok := orderLocks.Load(tradeNo)
	if !ok {
		return
	}
	rcm := v.(*refCountedMutex)
	rcm.mu.Unlock()

	createLock.Lock()
	rcm.refCount--
	if rcm.refCount == 0 {
		orderLocks.Delete(tradeNo)
	}
	createLock.Unlock()
}

func EpayNotify(c contract.Context) {
	if !isEpayWebhookEnabled() {
		logger.LogWarn(c.Context(), fmt.Sprintf("易支付 webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.RequestURI(), c.ClientIP()))
		_, _ = c.ResponseWriter().Write([]byte("fail"))
		return
	}

	var params map[string]string

	if c.Method() == "POST" {
		// POST 请求：从 POST body 解析参数
		if err := c.ParseForm(); err != nil {
			logger.LogError(c.Context(), fmt.Sprintf("易支付 webhook POST 表单解析失败 path=%q client_ip=%s error=%q", c.RequestURI(), c.ClientIP(), err.Error()))
			_, _ = c.ResponseWriter().Write([]byte("fail"))
			return
		}
		params = lo.Reduce(lo.Keys(c.PostFormValues()), func(r map[string]string, t string, i int) map[string]string {
			r[t] = c.PostForm(t)
			return r
		}, map[string]string{})
	} else {
		// GET 请求：从 URL Query 解析参数
		params = lo.Reduce(lo.Keys(c.QueryValues()), func(r map[string]string, t string, i int) map[string]string {
			r[t] = c.Query(t)
			return r
		}, map[string]string{})
	}
	logger.LogInfo(c.Context(), fmt.Sprintf("易支付 webhook 收到请求 path=%q client_ip=%s method=%s params=%q", c.RequestURI(), c.ClientIP(), c.Method(), common.GetJsonString(params)))

	if len(params) == 0 {
		logger.LogWarn(c.Context(), fmt.Sprintf("易支付 webhook 参数为空 path=%q client_ip=%s", c.RequestURI(), c.ClientIP()))
		_, _ = c.ResponseWriter().Write([]byte("fail"))
		return
	}
	client := GetEpayClient()
	if client == nil {
		logger.LogError(c.Context(), fmt.Sprintf("易支付 client 未初始化 path=%q client_ip=%s", c.RequestURI(), c.ClientIP()))
		_, err := c.ResponseWriter().Write([]byte("fail"))
		if err != nil {
			logger.LogError(c.Context(), fmt.Sprintf("易支付 webhook 响应写入失败 path=%q client_ip=%s error=%q", c.RequestURI(), c.ClientIP(), err.Error()))
		}
		return
	}
	verifyInfo, err := client.Verify(params)
	if err != nil || !verifyInfo.VerifyStatus {
		if _, writeErr := c.ResponseWriter().Write([]byte("fail")); writeErr != nil {
			logger.LogError(c.Context(), fmt.Sprintf("易支付 webhook 响应写入失败 path=%q client_ip=%s error=%q", c.RequestURI(), c.ClientIP(), writeErr.Error()))
		}
		if err != nil {
			logger.LogWarn(c.Context(), fmt.Sprintf("易支付 webhook 验签失败 path=%q client_ip=%s verify_error=%q", c.RequestURI(), c.ClientIP(), err.Error()))
		} else {
			logger.LogWarn(c.Context(), fmt.Sprintf("易支付 webhook 验签失败 path=%q client_ip=%s verify_status=false", c.RequestURI(), c.ClientIP()))
		}
		return
	}
	logger.LogInfo(c.Context(), fmt.Sprintf("易支付 webhook 验签成功 trade_no=%s callback_type=%s trade_status=%s client_ip=%s verify_info=%q", verifyInfo.ServiceTradeNo, verifyInfo.Type, verifyInfo.TradeStatus, c.ClientIP(), common.GetJsonString(verifyInfo)))

	if verifyInfo.TradeStatus == epay.StatusTradeSuccess {
		// 进程内锁只是优化；重复/并发回调的正确性由 RechargeEpay 的
		// 数据库行锁 + 事务内状态校验保证（多实例部署下同样安全）。
		LockOrder(verifyInfo.ServiceTradeNo)
		defer UnlockOrder(verifyInfo.ServiceTradeNo)
		alreadyDone, err := RechargeEpay(verifyInfo.ServiceTradeNo, verifyInfo.Type, c.ClientIP())
		if err != nil {
			switch {
			case errors.Is(err, ErrTopUpNotFound):
				logger.LogWarn(c.Context(), fmt.Sprintf("易支付 回调订单不存在 trade_no=%s callback_type=%s client_ip=%s verify_info=%q", verifyInfo.ServiceTradeNo, verifyInfo.Type, c.ClientIP(), common.GetJsonString(verifyInfo)))
			case errors.Is(err, ErrPaymentMethodMismatch):
				logger.LogWarn(c.Context(), fmt.Sprintf("易支付 订单支付网关不匹配 trade_no=%s callback_type=%s client_ip=%s", verifyInfo.ServiceTradeNo, verifyInfo.Type, c.ClientIP()))
			case errors.Is(err, ErrTopUpStatusInvalid):
				logger.LogWarn(c.Context(), fmt.Sprintf("易支付 订单状态非法 trade_no=%s callback_type=%s client_ip=%s", verifyInfo.ServiceTradeNo, verifyInfo.Type, c.ClientIP()))
			default:
				logger.LogError(c.Context(), fmt.Sprintf("易支付 充值处理失败 trade_no=%s client_ip=%s error=%q", verifyInfo.ServiceTradeNo, c.ClientIP(), err.Error()))
			}
			if _, writeErr := c.ResponseWriter().Write([]byte("fail")); writeErr != nil {
				logger.LogError(c.Context(), fmt.Sprintf("易支付 webhook 响应写入失败 trade_no=%s client_ip=%s error=%q", verifyInfo.ServiceTradeNo, c.ClientIP(), writeErr.Error()))
			}
			return
		}
		if alreadyDone {
			logger.LogInfo(c.Context(), fmt.Sprintf("易支付 重复回调幂等忽略 trade_no=%s callback_type=%s client_ip=%s", verifyInfo.ServiceTradeNo, verifyInfo.Type, c.ClientIP()))
		} else {
			logger.LogInfo(c.Context(), fmt.Sprintf("易支付 充值成功 trade_no=%s callback_type=%s client_ip=%s", verifyInfo.ServiceTradeNo, verifyInfo.Type, c.ClientIP()))
		}
	} else {
		logger.LogInfo(c.Context(), fmt.Sprintf("易支付 webhook 忽略事件 trade_no=%s callback_type=%s trade_status=%s client_ip=%s verify_info=%q", verifyInfo.ServiceTradeNo, verifyInfo.Type, verifyInfo.TradeStatus, c.ClientIP(), common.GetJsonString(verifyInfo)))
	}
	if _, writeErr := c.ResponseWriter().Write([]byte("success")); writeErr != nil {
		logger.LogError(c.Context(), fmt.Sprintf("易支付 webhook 响应写入失败 trade_no=%s client_ip=%s error=%q", verifyInfo.ServiceTradeNo, c.ClientIP(), writeErr.Error()))
	}
}

func RequestAmount(c contract.Context) {
	var req AmountRequest
	err := c.BindJSON(&req)
	if err != nil {
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": "参数错误"})
		return
	}

	if req.Amount < getMinTopup() {
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", getMinTopup())})
		return
	}
	id := c.GetInt("id")
	if rejectInvalidTopUpQuota(c, id, req.Amount) {
		return
	}
	group, err := identity.GetUserGroup(id, true)
	if err != nil {
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	payMoney := getPayMoney(req.Amount, group)
	if payMoney <= 0.01 {
		_ = c.JSON(http.StatusOK, common.H{"message": "error", "data": "充值金额过低"})
		return
	}
	_ = c.JSON(http.StatusOK, common.H{"message": "success", "data": strconv.FormatFloat(payMoney, 'f', 2, 64)})
}

func GetUserTopUps(c contract.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	keyword := c.Query("keyword")

	var (
		topups []*TopUp
		total  int64
		err    error
	)
	if keyword != "" {
		topups, total, err = SearchUserTopUps(userId, keyword, pageInfo)
	} else {
		topups, total, err = QueryUserTopUps(userId, pageInfo)
	}
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(topups)
	common.CtxApiSuccess(c, pageInfo)
}

// GetAllTopUps 管理员获取全平台充值记录
func GetAllTopUps(c contract.Context) {
	pageInfo := common.GetPageQuery(c)
	keyword := c.Query("keyword")

	var (
		topups []*TopUp
		total  int64
		err    error
	)
	if keyword != "" {
		topups, total, err = SearchAllTopUps(keyword, pageInfo)
	} else {
		topups, total, err = QueryAllTopUps(pageInfo)
	}
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(topups)
	common.CtxApiSuccess(c, pageInfo)
}

type AdminCompleteTopupRequest struct {
	TradeNo string `json:"trade_no"`
}

// AdminCompleteTopUp 管理员补单接口
func AdminCompleteTopUp(c contract.Context) {
	var req AdminCompleteTopupRequest
	if err := c.BindJSON(&req); err != nil || req.TradeNo == "" {
		common.CtxApiErrorMsg(c, "参数错误")
		return
	}

	// 订单级互斥，防止并发补单
	LockOrder(req.TradeNo)
	defer UnlockOrder(req.TradeNo)

	if err := ManualCompleteTopUp(req.TradeNo, c.ClientIP()); err != nil {
		common.CtxApiError(c, err)
		return
	}
	common.CtxApiSuccess(c, nil)
}

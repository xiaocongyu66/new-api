package billing

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/internal/billing/pay_subscription"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/i18n"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/model"
)

type PaymentComplianceRequest struct {
	Confirmed bool `json:"confirmed"`
}

func RequirePaymentCompliance(c contract.Context) bool {
	if !pay_subscription.IsPaymentComplianceConfirmed() {
		common.CtxApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
		return false
	}
	return true
}

func ConfirmPaymentCompliance(c contract.Context) {
	if c.GetBool("use_access_token") {
		_ = c.JSON(http.StatusForbidden, common.H{
			"success": false,
			"message": "This operation requires dashboard session authentication. API access token is not allowed.",
		})
		return
	}

	var req PaymentComplianceRequest
	if err := c.BindJSON(&req); err != nil {
		common.CtxApiErrorMsg(c, "参数错误")
		return
	}
	if !req.Confirmed {
		common.CtxApiErrorMsg(c, "请确认合规声明")
		return
	}

	now := time.Now().Unix()
	userId := c.GetInt("id")
	clientIP := c.ClientIP()

	updates := map[string]string{
		"payment_setting.compliance_confirmed":     "true",
		"payment_setting.compliance_terms_version": pay_subscription.CurrentComplianceTermsVersion,
		"payment_setting.compliance_confirmed_at":  strconv.FormatInt(now, 10),
		"payment_setting.compliance_confirmed_by":  strconv.Itoa(userId),
		"payment_setting.compliance_confirmed_ip":  clientIP,
	}

	for key, value := range updates {
		if err := model.UpdateOption(key, value); err != nil {
			common.CtxApiError(c, err)
			return
		}
	}

	logger.LogInfo(c.Context(), fmt.Sprintf(
		"payment compliance confirmed user_id=%d ip=%s terms_version=%s confirmed_at=%d",
		userId,
		clientIP,
		pay_subscription.CurrentComplianceTermsVersion,
		now,
	))

	common.CtxApiSuccess(c, common.H{
		"confirmed":     true,
		"terms_version": pay_subscription.CurrentComplianceTermsVersion,
		"confirmed_at":  now,
		"confirmed_by":  userId,
	})
}

// RequirePaymentComplianceMiddleware adapts RequirePaymentCompliance into a
// contract.Middleware: the check writes its own error response on failure and
// stops the chain so the billing gate can sit in the route layer. This keeps
// the identity domain free of the billing dependency (466c split).
func RequirePaymentComplianceMiddleware(c contract.Context) {
	if !RequirePaymentCompliance(c) {
		return
	}
	c.Next()
}

package billing

import (
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/internal/billing/price_expression"
	catalog "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/internal/settings"
)

// Payment option seeding and application live here, not in internal/settings:
// settings owns only the generic option storage/load/config mechanism, so it
// must not import this domain. Registration follows the same nil-safe hook-var
// convention the catalog domain already uses (see catalog/resolve_group.go).
func seedPaymentOptions() map[string]string {
	return map[string]string{
		"Price":                       strconv.FormatFloat(Price, 'f', -1, 64),
		"USDExchangeRate":             strconv.FormatFloat(USDExchangeRate, 'f', -1, 64),
		"MinTopUp":                    strconv.Itoa(MinTopUp),
		"StripeMinTopUp":              strconv.Itoa(StripeMinTopUp),
		"StripeApiSecret":             StripeApiSecret,
		"StripeWebhookSecret":         StripeWebhookSecret,
		"StripePriceId":               StripePriceId,
		"StripeUnitPrice":             strconv.FormatFloat(StripeUnitPrice, 'f', -1, 64),
		"StripePromotionCodesEnabled": strconv.FormatBool(StripePromotionCodesEnabled),
		"CreemApiKey":                 CreemApiKey,
		"CreemProducts":               CreemProducts,
		"CreemTestMode":               strconv.FormatBool(CreemTestMode),
		"CreemWebhookSecret":          CreemWebhookSecret,
		"WaffoEnabled":                strconv.FormatBool(WaffoEnabled),
		"WaffoApiKey":                 WaffoApiKey,
		"WaffoPrivateKey":             WaffoPrivateKey,
		"WaffoPublicCert":             WaffoPublicCert,
		"WaffoSandboxPublicCert":      WaffoSandboxPublicCert,
		"WaffoSandboxApiKey":          WaffoSandboxApiKey,
		"WaffoSandboxPrivateKey":      WaffoSandboxPrivateKey,
		"WaffoSandbox":                strconv.FormatBool(WaffoSandbox),
		"WaffoMerchantId":             WaffoMerchantId,
		"WaffoNotifyUrl":              WaffoNotifyUrl,
		"WaffoReturnUrl":              WaffoReturnUrl,
		"WaffoSubscriptionReturnUrl":  WaffoSubscriptionReturnUrl,
		"WaffoCurrency":               WaffoCurrency,
		"WaffoUnitPrice":              strconv.FormatFloat(WaffoUnitPrice, 'f', -1, 64),
		"WaffoMinTopUp":               strconv.Itoa(WaffoMinTopUp),
		"WaffoPayMethods":             WaffoPayMethods2JsonString(),
		"WaffoPancakeMerchantID":      WaffoPancakeMerchantID,
		"WaffoPancakePrivateKey":      WaffoPancakePrivateKey,
		"WaffoPancakeReturnURL":       WaffoPancakeReturnURL,
		"WaffoPancakeUnitPrice":       strconv.FormatFloat(WaffoPancakeUnitPrice, 'f', -1, 64),
		"WaffoPancakeMinTopUp":        strconv.Itoa(WaffoPancakeMinTopUp),
		"WaffoPancakeStoreID":         WaffoPancakeStoreID,
		"WaffoPancakeProductID":       WaffoPancakeProductID,
		"PayMethods":                  PayMethods2JsonString(),
	}
}

func applyPaymentOption(key, value string) error {
	switch key {
	case "PayAddress":
		PayAddress = value
	case "CustomCallbackAddress":
		CustomCallbackAddress = value
	case "EpayId":
		EpayId = value
	case "EpayKey":
		EpayKey = value
	case "Price":
		Price, _ = strconv.ParseFloat(value, 64)
	case "USDExchangeRate":
		USDExchangeRate, _ = strconv.ParseFloat(value, 64)
	case "MinTopUp":
		MinTopUp, _ = strconv.Atoi(value)
	case "StripeApiSecret":
		StripeApiSecret = value
	case "StripeWebhookSecret":
		StripeWebhookSecret = value
	case "StripePriceId":
		StripePriceId = value
	case "StripeUnitPrice":
		StripeUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "StripeMinTopUp":
		StripeMinTopUp, _ = strconv.Atoi(value)
	case "StripePromotionCodesEnabled":
		StripePromotionCodesEnabled = value == "true"
	case "CreemApiKey":
		CreemApiKey = value
	case "CreemProducts":
		CreemProducts = value
	case "CreemTestMode":
		CreemTestMode = value == "true"
	case "CreemWebhookSecret":
		CreemWebhookSecret = value
	case "WaffoEnabled":
		WaffoEnabled = value == "true"
	case "WaffoApiKey":
		WaffoApiKey = value
	case "WaffoPrivateKey":
		WaffoPrivateKey = value
	case "WaffoPublicCert":
		WaffoPublicCert = value
	case "WaffoSandboxPublicCert":
		WaffoSandboxPublicCert = value
	case "WaffoSandboxApiKey":
		WaffoSandboxApiKey = value
	case "WaffoSandboxPrivateKey":
		WaffoSandboxPrivateKey = value
	case "WaffoSandbox":
		WaffoSandbox = value == "true"
	case "WaffoMerchantId":
		WaffoMerchantId = value
	case "WaffoNotifyUrl":
		WaffoNotifyUrl = value
	case "WaffoReturnUrl":
		WaffoReturnUrl = value
	case "WaffoSubscriptionReturnUrl":
		WaffoSubscriptionReturnUrl = value
	case "WaffoCurrency":
		WaffoCurrency = value
	case "WaffoUnitPrice":
		WaffoUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "WaffoMinTopUp":
		WaffoMinTopUp, _ = strconv.Atoi(value)
	case "WaffoPancakeMerchantID":
		WaffoPancakeMerchantID = value
	case "WaffoPancakePrivateKey":
		WaffoPancakePrivateKey = value
	case "WaffoPancakeReturnURL":
		WaffoPancakeReturnURL = value
	case "WaffoPancakeStoreID":
		WaffoPancakeStoreID = value
	case "WaffoPancakeProductID":
		WaffoPancakeProductID = value
	case "WaffoPancakeUnitPrice":
		WaffoPancakeUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "WaffoPancakeMinTopUp":
		WaffoPancakeMinTopUp, _ = strconv.Atoi(value)
	case "PayMethods":
		return UpdatePayMethodsByJsonString(value)
	}
	return nil
}

// formatQuota carries the display-type rendering that used to live in
// internal/logger. logger owns no billing settings, so this domain registers it.
func formatQuota(quota int, withUnitSuffix bool) string {
	suffix := ""
	if withUnitSuffix {
		suffix = " 额度"
	}
	q := float64(quota)
	switch GetQuotaDisplayType() {
	case QuotaDisplayTypeCNY:
		cny := q / common.QuotaPerUnit * USDExchangeRate
		return fmt.Sprintf("¥%.6f%s", cny, suffix)
	case QuotaDisplayTypeCustom:
		rate := GetGeneralSetting().CustomCurrencyExchangeRate
		symbol := GetGeneralSetting().CustomCurrencySymbol
		if symbol == "" {
			symbol = "¤"
		}
		if rate <= 0 {
			rate = 1
		}
		return fmt.Sprintf("%s%.6f%s", symbol, q/common.QuotaPerUnit*rate, suffix)
	case QuotaDisplayTypeTokens:
		if withUnitSuffix {
			return fmt.Sprintf("%d 点额度", quota)
		}
		return fmt.Sprintf("%d", quota)
	default: // USD
		return fmt.Sprintf("＄%.6f%s", q/common.QuotaPerUnit, suffix)
	}
}

func init() {
	settings.OnSeedPaymentOptions = seedPaymentOptions
	settings.OnApplyPaymentOption = applyPaymentOption

	settings.OnIsToolPriceOptionKey = func(key string) bool {
		return key == price_expression.ToolPriceOptionKey
	}
	settings.OnValidateToolPriceOption = price_expression.ValidateToolPricesJSON
	settings.OnApplyToolPriceOption = price_expression.LoadToolPricesFromJSONString

	logger.OnFormatQuota = formatQuota

	identity.OnIsPaymentComplianceConfirmed = IsPaymentComplianceConfirmed

	catalog.OnResolveTieredBilling = func(model string) (string, string, bool) {
		mode := GetBillingMode(model)
		if mode != BillingModeTieredExpr {
			return "", "", false
		}
		expr, ok := GetBillingExpr(model)
		return mode, expr, ok
	}
}

package ops

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/internal/dbinfra"
	"github.com/QuantumNous/new-api/internal/settings"
)

type ProbeSetting struct {
	AutoProbeEnabled     bool    `json:"auto_probe_enabled"`
	ProbeIntervalMinutes float64 `json:"probe_interval_minutes"`
	WarningThreshold     float64 `json:"warning_threshold"`
	CriticalThreshold    float64 `json:"critical_threshold"`
	EmergencyThreshold   float64 `json:"emergency_threshold"`
	WebhookEnabled       bool    `json:"webhook_enabled"`
	WebhookUrl           string  `json:"webhook_url"`
	WebhookSecret        string  `json:"webhook_secret"`
}

const (
	DefaultProbeIntervalMinutes = 30
	DefaultWarningThreshold     = 70
	DefaultCriticalThreshold    = 50
	DefaultEmergencyThreshold   = 30
)

var (
	probeSetting = ProbeSetting{
		AutoProbeEnabled:     false,
		ProbeIntervalMinutes: DefaultProbeIntervalMinutes,
		WarningThreshold:     DefaultWarningThreshold,
		CriticalThreshold:    DefaultCriticalThreshold,
		EmergencyThreshold:   DefaultEmergencyThreshold,
		WebhookEnabled:       false,
	}
	probeSettingMu sync.RWMutex
)

var probeSettingOptionKeys = map[string]struct{}{
	"probe_setting.auto_probe_enabled":     {},
	"probe_setting.probe_interval_minutes": {},
	"probe_setting.warning_threshold":      {},
	"probe_setting.critical_threshold":     {},
	"probe_setting.emergency_threshold":    {},
	"probe_setting.webhook_enabled":        {},
	"probe_setting.webhook_url":            {},
	"probe_setting.webhook_secret":         {},
}

// IsProbeSettingOptionKey reports whether key belongs to the probe setting domain.
func IsProbeSettingOptionKey(key string) bool {
	_, ok := probeSettingOptionKeys[key]
	return ok
}

// ApplyProbeSettingOption applies one persisted probe option value to the live config.
func ApplyProbeSettingOption(key, value string) error {
	if !IsProbeSettingOptionKey(key) {
		return fmt.Errorf("unknown probe setting option key: %s", key)
	}

	probeSettingMu.Lock()
	defer probeSettingMu.Unlock()

	switch key {
	case "probe_setting.auto_probe_enabled":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean for %s: %w", key, err)
		}
		probeSetting.AutoProbeEnabled = b
	case "probe_setting.probe_interval_minutes":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid float for %s: %w", key, err)
		}
		probeSetting.ProbeIntervalMinutes = v
	case "probe_setting.warning_threshold":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid float for %s: %w", key, err)
		}
		probeSetting.WarningThreshold = v
	case "probe_setting.critical_threshold":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid float for %s: %w", key, err)
		}
		probeSetting.CriticalThreshold = v
	case "probe_setting.emergency_threshold":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid float for %s: %w", key, err)
		}
		probeSetting.EmergencyThreshold = v
	case "probe_setting.webhook_enabled":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean for %s: %w", key, err)
		}
		probeSetting.WebhookEnabled = b
	case "probe_setting.webhook_url":
		probeSetting.WebhookUrl = value
	case "probe_setting.webhook_secret":
		probeSetting.WebhookSecret = value
	default:
		return fmt.Errorf("unknown probe setting option key: %s", key)
	}
	return nil
}

// validateProbeSettingOptionValue does per-key type/range sanity for
// settings.ValidateOptionValue. Cross-field ordering stays in
// ValidateProbeSettingValue, used by the PUT endpoint.
func validateProbeSettingOptionValue(key, value string) error {
	switch key {
	case "probe_setting.auto_probe_enabled", "probe_setting.webhook_enabled":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("invalid boolean for %s: %w", key, err)
		}
	case "probe_setting.probe_interval_minutes":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil || v < 0 {
			return fmt.Errorf("%s must be a non-negative number", key)
		}
	case "probe_setting.warning_threshold", "probe_setting.critical_threshold", "probe_setting.emergency_threshold":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil || v < 0 || v > 100 {
			return fmt.Errorf("%s must be a number in [0, 100]", key)
		}
	}
	return nil
}

func init() {
	settings.GlobalConfig.Register("probe_setting", &probeSetting)
	settings.OnIsProbeSettingOptionKey = IsProbeSettingOptionKey
	settings.OnValidateProbeSettingOption = validateProbeSettingOptionValue
	settings.OnApplyProbeSettingOption = ApplyProbeSettingOption
}

// GetProbeSetting 返回归一化后的副本，避免调用者改写进程全局配置。
func GetProbeSetting() *ProbeSetting {
	probeSettingMu.RLock()
	s := probeSetting
	probeSettingMu.RUnlock()

	clamp := func(v float64) float64 {
		switch {
		case v < 0:
			return 0
		case v > 100:
			return 100
		default:
			return v
		}
	}
	s.WarningThreshold = clamp(s.WarningThreshold)
	s.CriticalThreshold = clamp(s.CriticalThreshold)
	s.EmergencyThreshold = clamp(s.EmergencyThreshold)
	if s.WarningThreshold <= s.CriticalThreshold || s.CriticalThreshold <= s.EmergencyThreshold {
		s.WarningThreshold = DefaultWarningThreshold
		s.CriticalThreshold = DefaultCriticalThreshold
		s.EmergencyThreshold = DefaultEmergencyThreshold
	}
	if s.ProbeIntervalMinutes < 0 {
		s.ProbeIntervalMinutes = DefaultProbeIntervalMinutes
	}
	return &s
}

// ValidateProbeSettingValue 供 PUT 端点全量校验：三阈值 ∈ [0,100] 且
// warning > critical > emergency；interval ≥ 0；开启 webhook 时 url 必须是 http(s)。
func ValidateProbeSettingValue(p ProbeSetting) error {
	for _, v := range []float64{p.WarningThreshold, p.CriticalThreshold, p.EmergencyThreshold} {
		if v < 0 || v > 100 {
			return errors.New("threshold must be a number in [0, 100]")
		}
	}
	if p.WarningThreshold <= p.CriticalThreshold || p.CriticalThreshold <= p.EmergencyThreshold {
		return errors.New("thresholds must satisfy warning_threshold > critical_threshold > emergency_threshold")
	}
	if p.ProbeIntervalMinutes < 0 {
		return errors.New("probe_interval_minutes must be non-negative")
	}
	if p.WebhookEnabled && !strings.HasPrefix(p.WebhookUrl, "http://") && !strings.HasPrefix(p.WebhookUrl, "https://") {
		return errors.New("webhook_url must be an http(s) URL when webhook_enabled is true")
	}
	return nil
}

// SaveProbeSetting 一次性持久化全部键（单事务），随后逐键写回运行时配置。
// 单事务保证不会出现"前几个键已生效、后面的键失败"的半套配置。
func SaveProbeSetting(p *ProbeSetting) error {
	values := map[string]string{
		"probe_setting.auto_probe_enabled":     strconv.FormatBool(p.AutoProbeEnabled),
		"probe_setting.probe_interval_minutes": strconv.FormatFloat(p.ProbeIntervalMinutes, 'f', -1, 64),
		"probe_setting.warning_threshold":      strconv.FormatFloat(p.WarningThreshold, 'f', -1, 64),
		"probe_setting.critical_threshold":     strconv.FormatFloat(p.CriticalThreshold, 'f', -1, 64),
		"probe_setting.emergency_threshold":    strconv.FormatFloat(p.EmergencyThreshold, 'f', -1, 64),
		"probe_setting.webhook_enabled":        strconv.FormatBool(p.WebhookEnabled),
		"probe_setting.webhook_url":            p.WebhookUrl,
		"probe_setting.webhook_secret":         p.WebhookSecret,
	}
	// UpdateOptionsBulk 单事务写库，并统一分发 settings.ApplyOption。
	return dbinfra.UpdateOptionsBulk(values)
}

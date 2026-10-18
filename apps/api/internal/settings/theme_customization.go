package settings

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
)

// ThemeCustomizationOptionKey pins the whole theme panel to the database so a
// release that retunes the shipped CSS defaults cannot silently move an
// operator's tuned theme. The value is a JSON object covering every axis of
// the theme drawer; the empty string means "nothing pinned", leaving the
// client on its own defaults.
//
// The axis tables below mirror the shipped frontend sets in
// apps/web/src/lib/theme-customization.ts. Validation runs on every write so a
// malformed or out-of-range payload is rejected here rather than being pushed
// onto every browser that reads /api/status.
const ThemeCustomizationOptionKey = "ThemeCustomization"

// themeCustomizationOption is the wire shape of ThemeCustomizationOptionKey.
// A pointer field is optional; a plain field is required, so a payload that
// omits e.g. surface_opacity_light is rejected instead of silently pinning a
// zero.
type themeCustomizationOption struct {
	Preset        string `json:"preset"`
	Font          string `json:"font"`
	Radius        string `json:"radius"`
	Scale         string `json:"scale"`
	ContentLayout string `json:"content_layout"`

	Blur       bool `json:"blur"`
	CardBlurLt bool `json:"card_blur_light"`
	CardBlurDk bool `json:"card_blur_dark"`

	SurfaceOpacityLt  *int `json:"surface_opacity_light"`
	SurfaceOpacityDk  *int `json:"surface_opacity_dark"`
	SurfaceBlurLt     *int `json:"surface_blur_light"`
	SurfaceBlurDk     *int `json:"surface_blur_dark"`
	BackgroundScrimLt *int `json:"background_scrim_light"`
	BackgroundScrimDk *int `json:"background_scrim_dark"`
	SheetOpacityLt    *int `json:"sheet_opacity_light"`
	SheetOpacityDk    *int `json:"sheet_opacity_dark"`
	FloatOpacityLt    *int `json:"float_opacity_light"`
	FloatOpacityDk    *int `json:"float_opacity_dark"`
}

func themeCustomizationEnumSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

var (
	themeCustomizationPresets        = themeCustomizationEnumSet("default", "anthropic", "nailao", "simple-large", "underground", "rose-garden", "lake-view", "sunset-glow", "forest-whisper", "ocean-breeze", "lavender-dream")
	themeCustomizationFonts          = themeCustomizationEnumSet("default", "sans", "serif")
	themeCustomizationRadii          = themeCustomizationEnumSet("default", "none", "sm", "md", "lg", "xl")
	themeCustomizationScales         = themeCustomizationEnumSet("default", "sm", "lg", "xl")
	themeCustomizationContentLayouts = themeCustomizationEnumSet("full", "centered")
)

func themeCustomizationEnum(name, value string, allowed map[string]struct{}) error {
	if _, ok := allowed[value]; !ok {
		return fmt.Errorf("%s 无效：%q", name, value)
	}
	return nil
}

func themeCustomizationRange(name string, value *int, min, max int) error {
	if value == nil {
		return fmt.Errorf("%s 缺失", name)
	}
	if *value < min || *value > max {
		return fmt.Errorf("%s 超出范围 [%d, %d]：%d", name, min, max, *value)
	}
	return nil
}

func themeCustomizationAxisErrors(
	axis string,
	light, dark *int,
	min, max int,
) []error {
	return []error{
		themeCustomizationRange(axis+"_light", light, min, max),
		themeCustomizationRange(axis+"_dark", dark, min, max),
	}
}

// ValidateThemeCustomizationOption parses the pinned-theme payload and reports
// every invalid axis. Empty means "not configured" and is always accepted so
// the drawer's reset can clear the pin.
func ValidateThemeCustomizationOption(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var payload themeCustomizationOption
	if err := common.Unmarshal([]byte(value), &payload); err != nil {
		return fmt.Errorf("主题配置不是有效的 JSON：%w", err)
	}
	errs := []error{
		themeCustomizationEnum("preset", payload.Preset, themeCustomizationPresets),
		themeCustomizationEnum("font", payload.Font, themeCustomizationFonts),
		themeCustomizationEnum("radius", payload.Radius, themeCustomizationRadii),
		themeCustomizationEnum("scale", payload.Scale, themeCustomizationScales),
		themeCustomizationEnum("content_layout", payload.ContentLayout, themeCustomizationContentLayouts),
	}
	errs = append(errs, themeCustomizationAxisErrors("surface_opacity", payload.SurfaceOpacityLt, payload.SurfaceOpacityDk, 10, 100)...)
	errs = append(errs, themeCustomizationAxisErrors("surface_blur", payload.SurfaceBlurLt, payload.SurfaceBlurDk, 0, 40)...)
	errs = append(errs, themeCustomizationAxisErrors("background_scrim", payload.BackgroundScrimLt, payload.BackgroundScrimDk, 0, 100)...)
	// Panel surfaces stay tinted glass: their lower bound is 50, not 10.
	errs = append(errs, themeCustomizationAxisErrors("sheet_opacity", payload.SheetOpacityLt, payload.SheetOpacityDk, 50, 100)...)
	errs = append(errs, themeCustomizationAxisErrors("float_opacity", payload.FloatOpacityLt, payload.FloatOpacityDk, 50, 100)...)

	var invalid []string
	for _, err := range errs {
		if err != nil {
			invalid = append(invalid, err.Error())
		}
	}
	if len(invalid) > 0 {
		return fmt.Errorf("主题配置无效：%s", strings.Join(invalid, "；"))
	}
	return nil
}

package settings_test

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The pinned theme row is written through the generic option endpoint, so
// ValidateOptionValue is the only gate between an arbitrary admin payload and
// every browser that reads /api/status. A payload that slips through pins a
// broken theme for the whole instance, so the contract is: every axis present,
// every value inside the shipped ranges.
const themeCustomizationValidPayload = `{"preset":"anthropic","font":"serif",` +
	`"radius":"md","scale":"lg","content_layout":"centered","blur":true,` +
	`"card_blur_light":true,"card_blur_dark":false,` +
	`"surface_opacity_light":80,"surface_opacity_dark":80,` +
	`"surface_blur_light":2,"surface_blur_dark":0,` +
	`"background_scrim_light":30,"background_scrim_dark":76,` +
	`"sheet_opacity_light":90,"sheet_opacity_dark":95,` +
	`"float_opacity_light":90,"float_opacity_dark":90}`

func TestValidateThemeCustomizationAcceptsFullPayload(t *testing.T) {
	require.NoError(
		t,
		settings.ValidateOptionValue(
			settings.ThemeCustomizationOptionKey,
			themeCustomizationValidPayload,
		),
	)
}

// Empty is the reset path: the drawer clears the row so the shipped defaults
// apply again. It must never be rejected as a malformed payload.
func TestValidateThemeCustomizationAcceptsEmptyValue(t *testing.T) {
	for _, value := range []string{"", "   "} {
		require.NoError(
			t,
			settings.ValidateOptionValue(
				settings.ThemeCustomizationOptionKey,
				value,
			),
			value,
		)
	}
}

func TestValidateThemeCustomizationRejectsInvalidPayloads(t *testing.T) {
	cases := map[string]string{
		"not json":                     "not json at all",
		"json array instead of object": `["preset"]`,
		"json null":                    `null`,
		"missing surface_opacity_light": strings.Replace(
			themeCustomizationValidPayload,
			`"surface_opacity_light":80,`,
			``,
			1,
		),
		"missing float_opacity_dark": strings.Replace(
			themeCustomizationValidPayload,
			`"float_opacity_light":90,"float_opacity_dark":90}`,
			`"float_opacity_light":90}`,
			1,
		),
		"surface opacity below min": strings.Replace(
			themeCustomizationValidPayload,
			`"surface_opacity_light":80`,
			`"surface_opacity_light":9`,
			1,
		),
		"surface opacity above max": strings.Replace(
			themeCustomizationValidPayload,
			`"surface_opacity_dark":80`,
			`"surface_opacity_dark":101`,
			1,
		),
		// Panel surfaces stay tinted glass, so their floor is 50, not the 10
		// used by card surfaces.
		"sheet opacity below panel floor": strings.Replace(
			themeCustomizationValidPayload,
			`"sheet_opacity_light":90`,
			`"sheet_opacity_light":40`,
			1,
		),
		"surface blur above max": strings.Replace(
			themeCustomizationValidPayload,
			`"surface_blur_dark":0`,
			`"surface_blur_dark":41`,
			1,
		),
		"background scrim above max": strings.Replace(
			themeCustomizationValidPayload,
			`"background_scrim_light":30`,
			`"background_scrim_light":101`,
			1,
		),
		"unknown preset": strings.Replace(
			themeCustomizationValidPayload,
			`"preset":"anthropic"`,
			`"preset":"midnight-neon"`,
			1,
		),
		"unknown font": strings.Replace(
			themeCustomizationValidPayload,
			`"font":"serif"`,
			`"font":"comic"`,
			1,
		),
		"unknown radius": strings.Replace(
			themeCustomizationValidPayload,
			`"radius":"md"`,
			`"radius":"huge"`,
			1,
		),
		"unknown scale": strings.Replace(
			themeCustomizationValidPayload,
			`"scale":"lg"`,
			`"scale":"xxl"`,
			1,
		),
		"unknown content layout": strings.Replace(
			themeCustomizationValidPayload,
			`"content_layout":"centered"`,
			`"content_layout":"split"`,
			1,
		),
	}

	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Error(
				t,
				settings.ValidateOptionValue(
					settings.ThemeCustomizationOptionKey,
					value,
				),
			)
		})
	}
}

// Every axis the panel exposes must be represented, otherwise a release could
// ship an axis the validator ignores and then silently stop writing it.
func TestValidateThemeCustomizationCoversEveryPanelAxis(t *testing.T) {
	axes := []string{
		`"preset"`, `"font"`, `"radius"`, `"scale"`, `"content_layout"`,
		`"blur"`,
		`"card_blur_light"`, `"card_blur_dark"`,
		`"surface_opacity_light"`, `"surface_opacity_dark"`,
		`"surface_blur_light"`, `"surface_blur_dark"`,
		`"background_scrim_light"`, `"background_scrim_dark"`,
		`"sheet_opacity_light"`, `"sheet_opacity_dark"`,
		`"float_opacity_light"`, `"float_opacity_dark"`,
	}
	for _, axis := range axes {
		assert.Contains(t, themeCustomizationValidPayload, axis)
	}
}

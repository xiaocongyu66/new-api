/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
/**
 * Theme customization constants and types.
 *
 * Lives in `lib/` (not `context/`) so it can be imported alongside the
 * provider without breaking React Fast Refresh boundaries.
 */

export const THEME_PRESETS = [
  {
    value: 'default',
    name: 'Default',
    swatches: ['oklch(0.614 0.207 262)', 'oklch(0.713 0.149 233.6)'],
  },
  {
    // Inspired by Anthropic's official brand language: warm cream canvas
    // (#faf9f5) paired with clay/coral (#d97757) as the single accent.
    // Swatches preview the canvas → accent gradient that defines the system.
    value: 'anthropic',
    name: 'Anthropic',
    swatches: ['oklch(0.984 0.005 95)', 'oklch(0.685 0.142 38)'],
  },
  {
    // Nailao (奶酪公益站) brand palette: warm cream canvas with cheese-gold
    // as the single accent and toasted-crust orange in the chart slots.
    // Surfaces derive from `--primary` via the semantic bridge below, so this
    // preset only declares the canvas, ink and accent anchors.
    value: 'nailao',
    name: 'Cheese',
    swatches: ['oklch(0.967 0.03 95)', 'oklch(0.84 0.14 85)'],
  },
  {
    value: 'simple-large',
    name: 'Simple Large-font',
    swatches: ['oklch(0.15 0 0)', 'oklch(0.99 0 0)'],
  },
  {
    value: 'underground',
    name: 'Underground',
    swatches: ['oklch(0.5315 0.0694 156.19)', 'oklch(0.5748 0.0862 336.52)'],
  },
  {
    value: 'rose-garden',
    name: 'Rose Garden',
    swatches: ['oklch(0.5827 0.2418 12.23)', 'oklch(0.8131 0.1129 5.67)'],
  },
  {
    value: 'lake-view',
    name: 'Lake View',
    swatches: ['oklch(0.765 0.177 163.22)', 'oklch(0.551 0.0899 200.52)'],
  },
  {
    value: 'sunset-glow',
    name: 'Sunset Glow',
    swatches: ['oklch(0.5591 0.1882 25.33)', 'oklch(0.7938 0.1248 42.42)'],
  },
  {
    value: 'forest-whisper',
    name: 'Forest Whisper',
    swatches: ['oklch(0.5276 0.1072 182.22)', 'oklch(0.5236 0.0505 250.18)'],
  },
  {
    value: 'ocean-breeze',
    name: 'Ocean Breeze',
    swatches: ['oklch(0.5461 0.2152 262.88)', 'oklch(0.5854 0.2041 277.12)'],
  },
  {
    value: 'lavender-dream',
    name: 'Lavender Dream',
    swatches: ['oklch(0.5709 0.1808 306.89)', 'oklch(0.811 0.0589 201.14)'],
  },
] as const

export type ThemePreset = (typeof THEME_PRESETS)[number]['value']
export type ThemeRadius = 'default' | 'none' | 'sm' | 'md' | 'lg' | 'xl'
export type ThemeScale = 'default' | 'sm' | 'lg' | 'xl'
export type ContentLayout = 'full' | 'centered'

/**
 * Font axis for the theme.
 *
 * - `default` — resolve at runtime from the active preset
 *   (see `PRESET_DEFAULT_FONT`). The shipped `default` and `anthropic`
 *   presets resolve to serif; other named color presets fall back to
 *   sans unless they list a different choice. Mirrors how
 *   `radius: 'default'` defers to a per-preset hint.
 * - `sans` — humanist sans (Public Sans), the project's UI fallback.
 * - `serif` — editorial serif (Lora + CJK fallbacks), the project's
 *   "soul" typography. Inherits across the whole UI; monospace contexts
 *   keep their own family via Tailwind preflight and `.font-mono`.
 */
export type ThemeFont = 'default' | 'sans' | 'serif'

/**
 * The resolved (non-`default`) font value applied to the DOM. The provider
 * always sets `data-theme-font` to one of these concrete values so CSS only
 * needs simple attribute selectors (no `:not()` gymnastics, no per-preset
 * font branches).
 */
export type ResolvedThemeFont = Exclude<ThemeFont, 'default'>

export type ThemeCustomization = {
  preset: ThemePreset
  font: ThemeFont
  radius: ThemeRadius
  scale: ThemeScale
  contentLayout: ContentLayout
  blur: boolean
  cardBlur: boolean
  // Glass surface axes, applied as inline CSS variables on <html>.
  surfaceOpacity: number
  surfaceBlur: number
  // Gray underlay over the background image (alpha % of a fixed dark gray);
  // written to <html> as the --app-background-scrim variable.
  backgroundScrim: number
  // Mobile shell axes, applied as inline CSS variables on <html>: the bottom
  // navigation dock and the left navigation sheets.
  dockOpacity: number
  sheetOpacity: number
}

/**
 * Default surface axes per color scheme: both schemes rest the UI on 80%
 * glass — light included, because the old 50% let the background photo
 * through far enough to wash out body copy, gray glyphs worst of all. Light
 * keeps card blur on; dark substitutes a heavy dark veil (76%) instead. The
 * mobile shell axes are scheme-independent: the bottom dock sits at 90% and
 * the navigation sheets at 95%. Must match the `:root` / `.dark` defaults of
 * --surface-opacity, --surface-blur, --app-background-scrim,
 * --mobile-dock-opacity and --mobile-sheet-opacity plus the card-blur toggle
 * in theme.css / theme-presets.css.
 */
export const SURFACE_DEFAULTS = {
  light: {
    cardBlur: true,
    surfaceOpacity: 80,
    surfaceBlur: 2,
    backgroundScrim: 30,
    dockOpacity: 90,
    sheetOpacity: 95,
  },
  dark: {
    cardBlur: false,
    surfaceOpacity: 80,
    surfaceBlur: 2,
    backgroundScrim: 76,
    dockOpacity: 90,
    sheetOpacity: 95,
  },
} as const

export type SurfaceScheme = keyof typeof SURFACE_DEFAULTS

export function surfaceDefaultsFor(theme: string) {
  return SURFACE_DEFAULTS[theme === 'light' ? 'light' : 'dark']
}

export const DEFAULT_THEME_CUSTOMIZATION: ThemeCustomization = {
  preset: 'default',
  font: 'default',
  radius: 'default',
  scale: 'default',
  contentLayout: 'full',
  blur: false,
  // Fallback mirror of the light entry of SURFACE_DEFAULTS; the provider
  // re-resolves these per color scheme at runtime.
  ...SURFACE_DEFAULTS.light,
}

export const SURFACE_OPACITY_LIMITS = { min: 10, max: 100 } as const
export const SURFACE_BLUR_LIMITS = { min: 0, max: 40 } as const
export const BACKGROUND_SCRIM_LIMITS = { min: 0, max: 100 } as const
/** Mobile dock/sheet panels are always tinted glass, so their sliders start
 * at 50% and can never dissolve a panel into the page beneath it. */
export const MOBILE_SURFACE_OPACITY_LIMITS = { min: 50, max: 100 } as const

/** Per-scheme scrim veil: light brightens dark random photos so the light
 * UI never sits on a black field; dark deepens the photo for light glyphs.
 * The provider supplies the chosen alpha and closing parenthesis when the
 * user overrides the theme.css default. */
export const BACKGROUND_SCRIM_COLOR_BASES = {
  light: 'oklch(0.93 0.006 260',
  dark: 'oklch(0.15 0.01 260',
} as const satisfies Record<SurfaceScheme, string>

/**
 * Per-color-scheme storage slots for the Blur-section axes. The theme
 * drawer shows a day/night tab so each scheme's surface can be tuned
 * independently; a value equal to the scheme's default is not persisted and
 * instead follows theme.css (SURFACE_DEFAULTS / :root/.dark), including the
 * per-scheme `cardBlur` default (light on, dark off).
 */
export const SURFACE_COOKIE_KEYS = {
  cardBlur: { light: 'theme_card_blur_light', dark: 'theme_card_blur_dark' },
  surfaceOpacity: {
    light: 'theme_surface_opacity_light',
    dark: 'theme_surface_opacity_dark',
  },
  surfaceBlur: {
    light: 'theme_surface_blur_light',
    dark: 'theme_surface_blur_dark',
  },
  backgroundScrim: {
    light: 'theme_bg_scrim_light',
    dark: 'theme_bg_scrim_dark',
  },
  dockOpacity: {
    light: 'theme_dock_opacity_light',
    dark: 'theme_dock_opacity_dark',
  },
  sheetOpacity: {
    light: 'theme_sheet_opacity_light',
    dark: 'theme_sheet_opacity_dark',
  },
} as const satisfies Record<
  | 'cardBlur'
  | 'surfaceOpacity'
  | 'surfaceBlur'
  | 'backgroundScrim'
  | 'dockOpacity'
  | 'sheetOpacity',
  Record<SurfaceScheme, string>
>

export const SURFACE_AXIS_LIST = [
  'cardBlur',
  'surfaceOpacity',
  'surfaceBlur',
  'backgroundScrim',
  'dockOpacity',
  'sheetOpacity',
] as const
export type SurfaceAxis = (typeof SURFACE_AXIS_LIST)[number]
export type SurfaceNumberAxis = Exclude<SurfaceAxis, 'cardBlur'>

/** Scheme-agnostic cookies from the pre-tab UI; read once as a fallback and
 * dropped whenever the matching per-scheme slot is written. The mobile
 * dock/sheet axes never had a pre-tab cookie, hence the partial map. */
export const LEGACY_SURFACE_COOKIE_KEYS: Partial<
  Record<SurfaceAxis, string>
> = {
  cardBlur: 'theme_card_blur',
  surfaceOpacity: 'theme_surface_opacity',
  surfaceBlur: 'theme_surface_blur',
  backgroundScrim: 'theme_bg_scrim',
}

export const THEME_PRESET_VALUES = new Set(
  THEME_PRESETS.map((p) => p.value)
) as ReadonlySet<ThemePreset>

export const THEME_FONT_VALUES: ReadonlySet<ThemeFont> = new Set([
  'default',
  'sans',
  'serif',
])

export const THEME_RADIUS_VALUES: ReadonlySet<ThemeRadius> = new Set([
  'default',
  'none',
  'sm',
  'md',
  'lg',
  'xl',
])

export const THEME_SCALE_VALUES: ReadonlySet<ThemeScale> = new Set([
  'default',
  'sm',
  'lg',
  'xl',
])

export const CONTENT_LAYOUT_VALUES: ReadonlySet<ContentLayout> = new Set([
  'full',
  'centered',
])

export const THEME_COOKIE_KEYS = {
  preset: 'theme_preset',
  font: 'theme_font',
  radius: 'theme_radius',
  scale: 'theme_scale',
  contentLayout: 'theme_content_layout',
  blur: 'theme_blur',
  // Surface axes are per-scheme; see SURFACE_COOKIE_KEYS.
} as const

/**
 * Preset → default font mapping. Used by the provider to resolve the user's
 * `font: 'default'` preference against the active preset.
 *
 * Co-located with the preset registry so a preset's signature typography
 * is declared in one place. Presets not listed here fall back to the
 * `resolveThemeFont` default of `sans`. The shipped `default` preset
 * opts into serif so the editorial Lora voice is the out-of-the-box
 * experience; vivid color presets stay on the humanist sans so their
 * accents read clearly without competing with the body type.
 */
export const PRESET_DEFAULT_FONT: Partial<
  Record<ThemePreset, ResolvedThemeFont>
> = {
  default: 'sans',
  anthropic: 'serif',
}

/**
 * Resolve a user font preference + active preset into the concrete font that
 * should drive the DOM. Pure function so it's safe to call inside both the
 * effect that applies the attribute and the UI preview that hints at what
 * `default` will render as.
 */
export function resolveThemeFont(
  font: ThemeFont,
  preset: ThemePreset
): ResolvedThemeFont {
  if (font === 'default') {
    return PRESET_DEFAULT_FONT[preset] ?? 'sans'
  }
  return font
}

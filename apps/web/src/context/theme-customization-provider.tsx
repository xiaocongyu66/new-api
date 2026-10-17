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
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react'

import { useTheme } from '@/context/theme-provider'
import { getCookie, removeCookie, setCookie } from '@/lib/cookies'
import {
  BACKGROUND_SCRIM_COLOR_BASES,
  BACKGROUND_SCRIM_LIMITS,
  CONTENT_LAYOUT_VALUES,
  type ContentLayout,
  DEFAULT_THEME_CUSTOMIZATION,
  LEGACY_SURFACE_COOKIE_KEYS,
  resolveThemeFont,
  SURFACE_AXIS_LIST,
  SURFACE_BLUR_LIMITS,
  SURFACE_COOKIE_KEYS,
  SURFACE_DEFAULTS,
  SURFACE_OPACITY_LIMITS,
  SURFACE_PANEL_OPACITY_LIMITS,
  type SurfaceAxis,
  surfaceDefaultsFor,
  type SurfaceNumberAxis,
  type SurfaceScheme,
  THEME_COOKIE_KEYS,
  THEME_FONT_VALUES,
  THEME_PRESET_VALUES,
  THEME_RADIUS_VALUES,
  THEME_SCALE_VALUES,
  type ThemeCustomization,
  type ThemeFont,
  type ThemePreset,
  type ThemeRadius,
  type ThemeScale,
} from '@/lib/theme-customization'

const COOKIE_MAX_AGE = 60 * 60 * 24 * 365 // 1 year

function readCookie<T extends string>(
  name: string,
  allowed: ReadonlySet<T>,
  fallback: T
): T {
  const value = getCookie(name)
  return value && allowed.has(value as T) ? (value as T) : fallback
}

function readBooleanCookie(name: string, fallback: boolean): boolean
function readBooleanCookie(name: string, fallback: null): boolean | null
function readBooleanCookie(name: string, fallback: boolean | null) {
  const value = getCookie(name)
  if (value === 'true') return true
  if (value === 'false') return false
  return fallback
}

function applyAttribute(name: string, value: string | null) {
  if (typeof document === 'undefined') return
  const body = document.body
  if (!body) return
  if (value === null) {
    body.removeAttribute(name)
  } else {
    body.setAttribute(name, value)
  }
}

function readNumberCookie(
  name: string,
  min: number,
  max: number
): number | null {
  const raw = getCookie(name)
  if (raw === undefined) return null
  const parsed = Number(raw)
  if (!Number.isFinite(parsed) || parsed < min || parsed > max) return null
  return Math.round(parsed)
}

// Continuous axes override theme variables inline on <html> so they beat the
// stylesheet defaults without adding one CSS rule per value.
function applyCssVariable(name: string, value: string | null) {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  if (value === null) {
    root.style.removeProperty(name)
  } else {
    root.style.setProperty(name, value)
  }
}

// The Blur-section axes live in per-color-scheme cookie slots so the
// drawer's day/night tab edits each scheme independently. The scheme-agnostic
// pre-tab cookies stay as a read fallback until the first per-scheme write
// drops them.
function surfaceLimitsFor(axis: SurfaceNumberAxis) {
  switch (axis) {
    case 'surfaceOpacity':
      return SURFACE_OPACITY_LIMITS
    case 'surfaceBlur':
      return SURFACE_BLUR_LIMITS
    case 'backgroundScrim':
      return BACKGROUND_SCRIM_LIMITS
    case 'sheetOpacity':
    case 'floatOpacity':
      return SURFACE_PANEL_OPACITY_LIMITS
  }
}

function readSurfaceCookie(
  axis: SurfaceNumberAxis,
  scheme: SurfaceScheme
): number | null {
  const { min, max } = surfaceLimitsFor(axis)
  const value = readNumberCookie(SURFACE_COOKIE_KEYS[axis][scheme], min, max)
  if (value !== null) return value
  // Only the four original axes ever shipped a pre-tab cookie.
  const legacy = LEGACY_SURFACE_COOKIE_KEYS[axis]
  return legacy === undefined ? null : readNumberCookie(legacy, min, max)
}

// The legacy map is partial — the mobile sheet and floating-overlay
// axes shipped after the per-scheme split and never had a scheme-agnostic
// cookie.
function removeLegacySurfaceCookie(axis: SurfaceAxis) {
  const name = LEGACY_SURFACE_COOKIE_KEYS[axis]
  if (name !== undefined) removeCookie(name)
}

function writeSurfaceCookie(
  axis: SurfaceNumberAxis,
  scheme: SurfaceScheme,
  value: number | null
) {
  removeLegacySurfaceCookie(axis)
  if (value === null) {
    removeCookie(SURFACE_COOKIE_KEYS[axis][scheme])
  } else {
    setCookie(
      SURFACE_COOKIE_KEYS[axis][scheme],
      String(value),
      COOKIE_MAX_AGE
    )
  }
}

function readSurfaceCardBlurCookie(scheme: SurfaceScheme): boolean | null {
  const explicit = readBooleanCookie(
    SURFACE_COOKIE_KEYS.cardBlur[scheme],
    null
  )
  if (explicit !== null) return explicit
  const legacyName = LEGACY_SURFACE_COOKIE_KEYS.cardBlur
  const legacy =
    legacyName === undefined ? null : readBooleanCookie(legacyName, null)
  // Card blur was split from the page-wide blur cookie long ago; keep the
  // same fallback chain for very old single-slot preferences.
  return legacy ?? readBooleanCookie(THEME_COOKIE_KEYS.blur, null)
}

function writeSurfaceCardBlurCookie(scheme: SurfaceScheme, value: boolean) {
  removeLegacySurfaceCookie('cardBlur')
  setCookie(
    SURFACE_COOKIE_KEYS.cardBlur[scheme],
    String(value),
    COOKIE_MAX_AGE
  )
}

type ThemeCustomizationContextType = {
  defaults: ThemeCustomization
  customization: ThemeCustomization
  setPreset: (preset: ThemePreset) => void
  setFont: (font: ThemeFont) => void
  setRadius: (radius: ThemeRadius) => void
  setScale: (scale: ThemeScale) => void
  setContentLayout: (contentLayout: ContentLayout) => void
  blur: boolean
  setBlur: (blur: boolean) => void
  cardBlur: boolean
  setCardBlur: (blur: boolean) => void
  surfaceOpacity: number
  setSurfaceOpacity: (opacity: number) => void
  surfaceBlur: number
  setSurfaceBlur: (blur: number) => void
  backgroundScrim: number
  setBackgroundScrim: (scrim: number) => void
  sheetOpacity: number
  setSheetOpacity: (opacity: number) => void
  floatOpacity: number
  setFloatOpacity: (opacity: number) => void
  // Color scheme whose storage slots the surface values above read from and
  // write to (the drawer's day/night tab switches it).
  surfaceScheme: SurfaceScheme
  resetCustomization: () => void
}

// Fallback used when a consumer renders outside the provider (e.g. an error
// route mounted before providers are ready, or stale HMR boundaries). Keeping
// it permissive prevents the whole tree from crashing — the UI just behaves
// like the defaults until the real provider re-mounts.
const FALLBACK_CONTEXT: ThemeCustomizationContextType = {
  defaults: DEFAULT_THEME_CUSTOMIZATION,
  customization: DEFAULT_THEME_CUSTOMIZATION,
  setPreset: () => {},
  setFont: () => {},
  setRadius: () => {},
  setScale: () => {},
  setContentLayout: () => {},
  blur: false,
  setBlur: () => {},
  cardBlur: false,
  setCardBlur: () => {},
  surfaceOpacity: DEFAULT_THEME_CUSTOMIZATION.surfaceOpacity,
  setSurfaceOpacity: () => {},
  surfaceBlur: DEFAULT_THEME_CUSTOMIZATION.surfaceBlur,
  setSurfaceBlur: () => {},
  backgroundScrim: DEFAULT_THEME_CUSTOMIZATION.backgroundScrim,
  setBackgroundScrim: () => {},
  sheetOpacity: SURFACE_DEFAULTS.dark.sheetOpacity,
  setSheetOpacity: () => {},
  floatOpacity: DEFAULT_THEME_CUSTOMIZATION.floatOpacity,
  setFloatOpacity: () => {},
  surfaceScheme: 'dark',
  resetCustomization: () => {},
}

const ThemeCustomizationContext =
  createContext<ThemeCustomizationContextType>(FALLBACK_CONTEXT)

export function ThemeCustomizationProvider(props: {
  children: React.ReactNode
}) {
  const [preset, _setPreset] = useState<ThemePreset>(() =>
    readCookie<ThemePreset>(
      THEME_COOKIE_KEYS.preset,
      THEME_PRESET_VALUES,
      DEFAULT_THEME_CUSTOMIZATION.preset
    )
  )
  const [font, _setFont] = useState<ThemeFont>(() =>
    readCookie<ThemeFont>(
      THEME_COOKIE_KEYS.font,
      THEME_FONT_VALUES,
      DEFAULT_THEME_CUSTOMIZATION.font
    )
  )
  const [radius, _setRadius] = useState<ThemeRadius>(() =>
    readCookie<ThemeRadius>(
      THEME_COOKIE_KEYS.radius,
      THEME_RADIUS_VALUES,
      DEFAULT_THEME_CUSTOMIZATION.radius
    )
  )
  const [scale, _setScale] = useState<ThemeScale>(() =>
    readCookie<ThemeScale>(
      THEME_COOKIE_KEYS.scale,
      THEME_SCALE_VALUES,
      DEFAULT_THEME_CUSTOMIZATION.scale
    )
  )
  const [contentLayout, _setContentLayout] = useState<ContentLayout>(() =>
    readCookie<ContentLayout>(
      THEME_COOKIE_KEYS.contentLayout,
      CONTENT_LAYOUT_VALUES,
      DEFAULT_THEME_CUSTOMIZATION.contentLayout
    )
  )
  const [blur, _setBlur] = useState(() =>
    readBooleanCookie(THEME_COOKIE_KEYS.blur, DEFAULT_THEME_CUSTOMIZATION.blur)
  )

  // The Blur-section axes are stored per color scheme so the drawer's
  // day/night tab edits each scheme independently; a value equal to the
  // scheme's CSS default is dropped from storage and follows theme.css.
  const { resolvedTheme } = useTheme()
  const surfaceScheme: SurfaceScheme =
    resolvedTheme === 'light' ? 'light' : 'dark'
  const surfaceDefaults = surfaceDefaultsFor(resolvedTheme)
  const [explicitCardBlur, _setCardBlur] = useState<boolean | null>(() =>
    readSurfaceCardBlurCookie(surfaceScheme)
  )
  const [explicitSurfaceOpacity, _setSurfaceOpacity] = useState<
    number | null
  >(() => readSurfaceCookie('surfaceOpacity', surfaceScheme))
  const [explicitSurfaceBlur, _setSurfaceBlur] = useState<number | null>(
    () => readSurfaceCookie('surfaceBlur', surfaceScheme)
  )
  const [explicitBackgroundScrim, _setBackgroundScrim] = useState<
    number | null
  >(() => readSurfaceCookie('backgroundScrim', surfaceScheme))
  const [explicitSheetOpacity, _setSheetOpacity] = useState<number | null>(
    () => readSurfaceCookie('sheetOpacity', surfaceScheme)
  )
  const [explicitFloatOpacity, _setFloatOpacity] = useState<number | null>(
    () => readSurfaceCookie('floatOpacity', surfaceScheme)
  )
  const cardBlur = explicitCardBlur ?? surfaceDefaults.cardBlur
  const surfaceOpacity =
    explicitSurfaceOpacity ?? surfaceDefaults.surfaceOpacity
  const surfaceBlur = explicitSurfaceBlur ?? surfaceDefaults.surfaceBlur
  const backgroundScrim =
    explicitBackgroundScrim ?? surfaceDefaults.backgroundScrim
  const sheetOpacity = explicitSheetOpacity ?? surfaceDefaults.sheetOpacity
  const floatOpacity = explicitFloatOpacity ?? surfaceDefaults.floatOpacity
  // Re-read storage when the color scheme flips so sliders, inline CSS
  // variables and cookie slots always track the active tab's scheme.
  useEffect(() => {
    _setCardBlur(readSurfaceCardBlurCookie(surfaceScheme))
    _setSurfaceOpacity(readSurfaceCookie('surfaceOpacity', surfaceScheme))
    _setSurfaceBlur(readSurfaceCookie('surfaceBlur', surfaceScheme))
    _setBackgroundScrim(readSurfaceCookie('backgroundScrim', surfaceScheme))
    _setSheetOpacity(readSurfaceCookie('sheetOpacity', surfaceScheme))
    _setFloatOpacity(readSurfaceCookie('floatOpacity', surfaceScheme))
  }, [surfaceScheme])

  // Mirror state to the <body> via data-* attributes so theme-presets.css can
  // override CSS variables at the right cascade layer.
  useEffect(() => {
    applyAttribute(
      'data-theme-preset',
      preset === DEFAULT_THEME_CUSTOMIZATION.preset ? null : preset
    )
  }, [preset])

  // Font is the one axis where we resolve before writing the attribute:
  // the persisted preference may be `default`, but CSS works in terms of
  // the concrete `sans`/`serif` choice that should drive the cascade.
  // Resolving here (instead of in CSS via `:not()` selectors) keeps the
  // stylesheet to one simple `[data-theme-font='serif']` selector and lets
  // future presets opt into typography via `PRESET_DEFAULT_FONT` alone.
  useEffect(() => {
    applyAttribute('data-theme-font', resolveThemeFont(font, preset))
  }, [font, preset])

  useEffect(() => {
    applyAttribute(
      'data-theme-radius',
      radius === DEFAULT_THEME_CUSTOMIZATION.radius ? null : radius
    )
  }, [radius])

  useEffect(() => {
    applyAttribute(
      'data-theme-scale',
      scale === DEFAULT_THEME_CUSTOMIZATION.scale ? null : scale
    )
  }, [scale])

  useEffect(() => {
    applyAttribute('data-theme-content-layout', contentLayout)
  }, [contentLayout])

  useEffect(() => {
    applyAttribute('data-theme-blur', blur ? 'true' : null)
  }, [blur])

  useEffect(() => {
    applyAttribute('data-theme-card-blur', cardBlur ? 'true' : null)
  }, [cardBlur])

  useEffect(() => {
    applyCssVariable(
      '--surface-opacity',
      explicitSurfaceOpacity === null ||
        explicitSurfaceOpacity === surfaceDefaults.surfaceOpacity
        ? null
        : `${explicitSurfaceOpacity}%`
    )
  }, [explicitSurfaceOpacity, surfaceDefaults])

  useEffect(() => {
    applyCssVariable(
      '--surface-blur',
      explicitSurfaceBlur === null ||
        explicitSurfaceBlur === surfaceDefaults.surfaceBlur
        ? null
        : `${explicitSurfaceBlur}px`
    )
  }, [explicitSurfaceBlur, surfaceDefaults])

  useEffect(() => {
    applyCssVariable(
      '--app-background-scrim',
      explicitBackgroundScrim === null ||
        explicitBackgroundScrim === surfaceDefaults.backgroundScrim
        ? null
        : `${BACKGROUND_SCRIM_COLOR_BASES[surfaceScheme]} / ${explicitBackgroundScrim}%)`
    )
  }, [explicitBackgroundScrim, surfaceDefaults, surfaceScheme])

  useEffect(() => {
    applyCssVariable(
      '--mobile-sheet-opacity',
      explicitSheetOpacity === null ||
        explicitSheetOpacity === surfaceDefaults.sheetOpacity
        ? null
        : `${explicitSheetOpacity}%`
    )
  }, [explicitSheetOpacity, surfaceDefaults])

  useEffect(() => {
    applyCssVariable(
      '--float-opacity',
      explicitFloatOpacity === null ||
        explicitFloatOpacity === surfaceDefaults.floatOpacity
        ? null
        : `${explicitFloatOpacity}%`
    )
  }, [explicitFloatOpacity, surfaceDefaults])

  const setPreset = useCallback((value: ThemePreset) => {
    _setPreset(value)
    if (value === DEFAULT_THEME_CUSTOMIZATION.preset) {
      removeCookie(THEME_COOKIE_KEYS.preset)
    } else {
      setCookie(THEME_COOKIE_KEYS.preset, value, COOKIE_MAX_AGE)
    }
  }, [])

  const setFont = useCallback((value: ThemeFont) => {
    _setFont(value)
    if (value === DEFAULT_THEME_CUSTOMIZATION.font) {
      removeCookie(THEME_COOKIE_KEYS.font)
    } else {
      setCookie(THEME_COOKIE_KEYS.font, value, COOKIE_MAX_AGE)
    }
  }, [])

  const setRadius = useCallback((value: ThemeRadius) => {
    _setRadius(value)
    if (value === DEFAULT_THEME_CUSTOMIZATION.radius) {
      removeCookie(THEME_COOKIE_KEYS.radius)
    } else {
      setCookie(THEME_COOKIE_KEYS.radius, value, COOKIE_MAX_AGE)
    }
  }, [])

  const setScale = useCallback((value: ThemeScale) => {
    _setScale(value)
    if (value === DEFAULT_THEME_CUSTOMIZATION.scale) {
      removeCookie(THEME_COOKIE_KEYS.scale)
    } else {
      setCookie(THEME_COOKIE_KEYS.scale, value, COOKIE_MAX_AGE)
    }
  }, [])

  const setContentLayout = useCallback((value: ContentLayout) => {
    _setContentLayout(value)
    if (value === DEFAULT_THEME_CUSTOMIZATION.contentLayout) {
      removeCookie(THEME_COOKIE_KEYS.contentLayout)
    } else {
      setCookie(THEME_COOKIE_KEYS.contentLayout, value, COOKIE_MAX_AGE)
    }
  }, [])

  const setBlur = useCallback((value: boolean) => {
    _setBlur(value)
    if (value === DEFAULT_THEME_CUSTOMIZATION.blur) {
      removeCookie(THEME_COOKIE_KEYS.blur)
    } else {
      setCookie(THEME_COOKIE_KEYS.blur, String(value), COOKIE_MAX_AGE)
    }
  }, [])

  const setCardBlur = useCallback(
    (value: boolean) => {
      _setCardBlur(value)
      const atDefault = value === surfaceDefaults.cardBlur
      if (atDefault) {
        removeLegacySurfaceCookie('cardBlur')
        removeCookie(SURFACE_COOKIE_KEYS.cardBlur[surfaceScheme])
      } else {
        writeSurfaceCardBlurCookie(surfaceScheme, value)
      }
    },
    [surfaceScheme]
  )

  const setSurfaceOpacity = useCallback(
    (value: number) => {
      if (!Number.isFinite(value)) return
      const clamped = Math.round(
        Math.min(
          SURFACE_OPACITY_LIMITS.max,
          Math.max(SURFACE_OPACITY_LIMITS.min, value)
        )
      )
      const atDefault = clamped === surfaceDefaults.surfaceOpacity
      _setSurfaceOpacity(atDefault ? null : clamped)
      writeSurfaceCookie(
        'surfaceOpacity',
        surfaceScheme,
        atDefault ? null : clamped
      )
    },
    [surfaceScheme, surfaceDefaults]
  )

  const setSurfaceBlur = useCallback(
    (value: number) => {
      if (!Number.isFinite(value)) return
      const clamped = Math.round(
        Math.min(
          SURFACE_BLUR_LIMITS.max,
          Math.max(SURFACE_BLUR_LIMITS.min, value)
        )
      )
      const atDefault = clamped === surfaceDefaults.surfaceBlur
      _setSurfaceBlur(atDefault ? null : clamped)
      writeSurfaceCookie(
        'surfaceBlur',
        surfaceScheme,
        atDefault ? null : clamped
      )
    },
    [surfaceScheme, surfaceDefaults]
  )

  const setBackgroundScrim = useCallback(
    (value: number) => {
      if (!Number.isFinite(value)) return
      const clamped = Math.round(
        Math.min(
          BACKGROUND_SCRIM_LIMITS.max,
          Math.max(BACKGROUND_SCRIM_LIMITS.min, value)
        )
      )
      const atDefault = clamped === surfaceDefaults.backgroundScrim
      _setBackgroundScrim(atDefault ? null : clamped)
      writeSurfaceCookie(
        'backgroundScrim',
        surfaceScheme,
        atDefault ? null : clamped
      )
    },
    [surfaceScheme, surfaceDefaults]
  )

  const setSheetOpacity = useCallback(
    (value: number) => {
      if (!Number.isFinite(value)) return
      const clamped = Math.round(
        Math.min(
          SURFACE_PANEL_OPACITY_LIMITS.max,
          Math.max(SURFACE_PANEL_OPACITY_LIMITS.min, value)
        )
      )
      const atDefault = clamped === surfaceDefaults.sheetOpacity
      _setSheetOpacity(atDefault ? null : clamped)
      writeSurfaceCookie(
        'sheetOpacity',
        surfaceScheme,
        atDefault ? null : clamped
      )
    },
    [surfaceScheme, surfaceDefaults]
  )

  const setFloatOpacity = useCallback(
    (value: number) => {
      if (!Number.isFinite(value)) return
      const clamped = Math.round(
        Math.min(
          SURFACE_PANEL_OPACITY_LIMITS.max,
          Math.max(SURFACE_PANEL_OPACITY_LIMITS.min, value)
        )
      )
      const atDefault = clamped === surfaceDefaults.floatOpacity
      _setFloatOpacity(atDefault ? null : clamped)
      writeSurfaceCookie(
        'floatOpacity',
        surfaceScheme,
        atDefault ? null : clamped
      )
    },
    [surfaceScheme, surfaceDefaults]
  )

  // Clears both color schemes' surface slots (plus the pre-tab legacy
  // cookies) so each scheme falls back to its CSS preset.
  const resetCustomization = useCallback(() => {
    setPreset(DEFAULT_THEME_CUSTOMIZATION.preset)
    setFont(DEFAULT_THEME_CUSTOMIZATION.font)
    setRadius(DEFAULT_THEME_CUSTOMIZATION.radius)
    setScale(DEFAULT_THEME_CUSTOMIZATION.scale)
    setContentLayout(DEFAULT_THEME_CUSTOMIZATION.contentLayout)
    setBlur(DEFAULT_THEME_CUSTOMIZATION.blur)
    for (const axis of SURFACE_AXIS_LIST) {
      removeCookie(SURFACE_COOKIE_KEYS[axis].light)
      removeCookie(SURFACE_COOKIE_KEYS[axis].dark)
      removeLegacySurfaceCookie(axis)
    }
    _setCardBlur(null)
    _setSurfaceOpacity(null)
    _setSurfaceBlur(null)
    _setBackgroundScrim(null)
    _setSheetOpacity(null)
    _setFloatOpacity(null)
  }, [setPreset, setFont, setRadius, setScale, setContentLayout, setBlur])

  const defaults = useMemo<ThemeCustomization>(
    () => ({
      ...DEFAULT_THEME_CUSTOMIZATION,
      cardBlur: surfaceDefaults.cardBlur,
      surfaceOpacity: surfaceDefaults.surfaceOpacity,
      surfaceBlur: surfaceDefaults.surfaceBlur,
      backgroundScrim: surfaceDefaults.backgroundScrim,
      sheetOpacity: surfaceDefaults.sheetOpacity,
      floatOpacity: surfaceDefaults.floatOpacity,
    }),
    [surfaceDefaults]
  )

  const value = useMemo<ThemeCustomizationContextType>(
    () => ({
      defaults,
      customization: {
        preset,
        font,
        radius,
        scale,
        contentLayout,
        blur,
        cardBlur,
        surfaceOpacity,
        surfaceBlur,
        backgroundScrim,
        sheetOpacity,
        floatOpacity,
      },
      blur,
      setBlur,
      cardBlur,
      setCardBlur,
      surfaceOpacity,
      setSurfaceOpacity,
      surfaceBlur,
      setSurfaceBlur,
      backgroundScrim,
      setBackgroundScrim,
      sheetOpacity,
      setSheetOpacity,
      floatOpacity,
      setFloatOpacity,
      surfaceScheme,
      setPreset,
      setFont,
      setRadius,
      setScale,
      setContentLayout,
      resetCustomization,
    }),
    [
      preset,
      font,
      radius,
      scale,
      contentLayout,
      blur,
      cardBlur,
      surfaceOpacity,
      surfaceBlur,
      backgroundScrim,
      sheetOpacity,
      floatOpacity,
      surfaceScheme,
      setPreset,
      setBlur,
      setCardBlur,
      setSurfaceOpacity,
      setSurfaceBlur,
      setBackgroundScrim,
      setSheetOpacity,
      setFloatOpacity,
      setFont,
      setRadius,
      setScale,
      setContentLayout,
      resetCustomization,
      defaults,
    ]
  )

  return (
    <ThemeCustomizationContext.Provider value={value}>
      {props.children}
    </ThemeCustomizationContext.Provider>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export function useThemeCustomization() {
  return useContext(ThemeCustomizationContext)
}

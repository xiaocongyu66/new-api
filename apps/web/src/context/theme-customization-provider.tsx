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
  useRef,
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
import {
  clearThemeCustomization,
  type PinnedTheme,
  parseThemeCustomization,
  pinnedThemeFont,
  saveThemeCustomization,
  serializeThemeCustomization,
  surfaceBlockFor,
} from '@/lib/theme-persistence'
import { useSystemConfigStore } from '@/stores/system-config-store'

const COOKIE_MAX_AGE = 60 * 60 * 24 * 365 // 1 year

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
// drawer's day/night tab edits each scheme independently.
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

// The legacy map is partial — the mobile sheet and floating-overlay
// axes shipped after the per-scheme split and never had a scheme-agnostic
// cookie.
function removeLegacySurfaceCookie(axis: SurfaceAxis) {
  const name = LEGACY_SURFACE_COOKIE_KEYS[axis]
  if (name !== undefined) removeCookie(name)
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

function readSurfaceCardBlurCookie(scheme: SurfaceScheme): boolean | null {
  const explicit = readBooleanCookie(SURFACE_COOKIE_KEYS.cardBlur[scheme], null)
  if (explicit !== null) return explicit
  const legacyName = LEGACY_SURFACE_COOKIE_KEYS.cardBlur
  const legacy =
    legacyName === undefined ? null : readBooleanCookie(legacyName, null)
  return legacy
}

function writeSurfaceCookie(
  axis: SurfaceNumberAxis,
  scheme: SurfaceScheme,
  value: number
) {
  removeLegacySurfaceCookie(axis)
  setCookie(
    SURFACE_COOKIE_KEYS[axis][scheme],
    String(value),
    COOKIE_MAX_AGE
  )
}

// Mirror the whole pinned state into per-scheme cookies so a fresh browser
// paints the right theme before /api/status answers.
function writePinnedCookies(pinned: PinnedTheme) {
  setCookie(THEME_COOKIE_KEYS.preset, pinned.preset, COOKIE_MAX_AGE)
  setCookie(THEME_COOKIE_KEYS.font, pinned.font, COOKIE_MAX_AGE)
  setCookie(THEME_COOKIE_KEYS.radius, pinned.radius, COOKIE_MAX_AGE)
  setCookie(THEME_COOKIE_KEYS.scale, pinned.scale, COOKIE_MAX_AGE)
  setCookie(
    THEME_COOKIE_KEYS.contentLayout,
    pinned.contentLayout,
    COOKIE_MAX_AGE
  )
  setCookie(THEME_COOKIE_KEYS.blur, String(pinned.blur), COOKIE_MAX_AGE)

  for (const scheme of ['light', 'dark'] as const) {
    const block = pinned[scheme]
    setCookie(
      SURFACE_COOKIE_KEYS.cardBlur[scheme],
      String(block.cardBlur),
      COOKIE_MAX_AGE
    )
    for (const axis of SURFACE_AXIS_LIST) {
      if (axis === 'cardBlur') continue
      writeSurfaceCookie(axis, scheme, block[axis])
    }
  }
}

function clearPinnedCookies() {
  for (const key of Object.values(THEME_COOKIE_KEYS)) {
    removeCookie(key)
  }
  for (const axis of SURFACE_AXIS_LIST) {
    removeCookie(SURFACE_COOKIE_KEYS[axis].light)
    removeCookie(SURFACE_COOKIE_KEYS[axis].dark)
    removeLegacySurfaceCookie(axis)
  }
}

function readPinnedFromCookies(): PinnedTheme {
  const readBlock = (scheme: SurfaceScheme) => {
    const defaults = surfaceBlockFor(scheme)
    return {
      cardBlur:
        readSurfaceCardBlurCookie(scheme) ?? defaults.cardBlur,
      surfaceOpacity:
        readSurfaceCookie('surfaceOpacity', scheme) ?? defaults.surfaceOpacity,
      surfaceBlur:
        readSurfaceCookie('surfaceBlur', scheme) ?? defaults.surfaceBlur,
      backgroundScrim:
        readSurfaceCookie('backgroundScrim', scheme) ??
        defaults.backgroundScrim,
      sheetOpacity:
        readSurfaceCookie('sheetOpacity', scheme) ?? defaults.sheetOpacity,
      floatOpacity:
        readSurfaceCookie('floatOpacity', scheme) ?? defaults.floatOpacity,
    }
  }
  return {
    preset: readCookie(
      THEME_COOKIE_KEYS.preset,
      THEME_PRESET_VALUES,
      DEFAULT_THEME_CUSTOMIZATION.preset
    ),
    font: readCookie(
      THEME_COOKIE_KEYS.font,
      THEME_FONT_VALUES,
      DEFAULT_THEME_CUSTOMIZATION.font
    ),
    radius: readCookie(
      THEME_COOKIE_KEYS.radius,
      THEME_RADIUS_VALUES,
      DEFAULT_THEME_CUSTOMIZATION.radius
    ),
    scale: readCookie(
      THEME_COOKIE_KEYS.scale,
      THEME_SCALE_VALUES,
      DEFAULT_THEME_CUSTOMIZATION.scale
    ),
    contentLayout: readCookie(
      THEME_COOKIE_KEYS.contentLayout,
      CONTENT_LAYOUT_VALUES,
      DEFAULT_THEME_CUSTOMIZATION.contentLayout
    ),
    blur: readBooleanCookie(
      THEME_COOKIE_KEYS.blur,
      DEFAULT_THEME_CUSTOMIZATION.blur
    ),
    light: readBlock('light'),
    dark: readBlock('dark'),
  }
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
  /** Whether the shared option row currently governs this theme. */
  themePinnedToServer: boolean
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
  themePinnedToServer: false,
  resetCustomization: () => {},
}

const ThemeCustomizationContext =
  createContext<ThemeCustomizationContextType>(FALLBACK_CONTEXT)

export function ThemeCustomizationProvider(props: {
  children: React.ReactNode
}) {
  const [pinned, setPinned] = useState<PinnedTheme>(readPinnedFromCookies)
  // Cookies (or the shipped defaults) paint first; the shared row replaces
  // them once /api/status answers.
  const [hydrated, setHydrated] = useState(false)
  const [serverRow, setServerRow] = useState('')
  const serverTheme = useSystemConfigStore((state) => state.serverTheme)
  const serverThemeLoaded = useSystemConfigStore(
    (state) => state.serverThemeLoaded
  )

  const { resolvedTheme } = useTheme()
  const surfaceScheme: SurfaceScheme =
    resolvedTheme === 'light' ? 'light' : 'dark'
  const surfaceDefaults = surfaceDefaultsFor(resolvedTheme)

  // Adopt the shared row when it arrives. A row that is absent leaves the
  // local state alone: browsers that never pinned one keep their own values
  // and can promote them on the first edit.
  useEffect(() => {
    if (!serverThemeLoaded) return
    const parsed = parseThemeCustomization(
      serverTheme,
      DEFAULT_THEME_CUSTOMIZATION
    )
    setServerRow(serverTheme)
    setPinned(parsed ?? readPinnedFromCookies())
    // The row already on the server is the baseline an edit is measured
    // against, so adopting it must not count as a change worth writing.
    persistedRowRef.current = serverTheme.trim() === '' ? null : serverTheme
    setHydrated(true)
  }, [serverThemeLoaded, serverTheme])

  // Persist on every edit. The cookie mirror is synchronous so the next paint
  // is right even when the write below fails (a non-admin browser keeps its
  // own theme, as before); the shared row is what survives a new release.
  // Nothing is written until the panel is actually edited, so a browser that
  // only ever reads the defaults does not create a row on page load. Writes
  // are debounced because a slider drag produces dozens of intermediate values.
  const dirtyRef = useRef(false)
  const persistedRowRef = useRef<string | null>(null)
  const saveTimerRef = useRef<number | null>(null)
  useEffect(() => {
    if (!hydrated || !dirtyRef.current) return
    writePinnedCookies(pinned)
    const serialized = serializeThemeCustomization(pinned)
    if (serialized === persistedRowRef.current) return

    if (saveTimerRef.current !== null) {
      window.clearTimeout(saveTimerRef.current)
    }
    saveTimerRef.current = window.setTimeout(() => {
      saveTimerRef.current = null
      void saveThemeCustomization(serialized).then((ok) => {
        if (ok) persistedRowRef.current = serialized
      })
    }, 500)

    return () => {
      if (saveTimerRef.current !== null) {
        window.clearTimeout(saveTimerRef.current)
        saveTimerRef.current = null
      }
    }
  }, [pinned, hydrated])

  const block = pinned[surfaceScheme]

  // Mirror state to the <body> via data-* attributes so theme-presets.css can
  // override CSS variables at the right cascade layer.
  useEffect(() => {
    applyAttribute(
      'data-theme-preset',
      pinned.preset === DEFAULT_THEME_CUSTOMIZATION.preset ? null : pinned.preset
    )
  }, [pinned.preset])

  useEffect(() => {
    applyAttribute('data-theme-font', pinnedThemeFont(pinned))
  }, [pinned.font, pinned.preset])

  useEffect(() => {
    applyAttribute(
      'data-theme-radius',
      pinned.radius === DEFAULT_THEME_CUSTOMIZATION.radius ? null : pinned.radius
    )
  }, [pinned.radius])

  useEffect(() => {
    applyAttribute(
      'data-theme-scale',
      pinned.scale === DEFAULT_THEME_CUSTOMIZATION.scale ? null : pinned.scale
    )
  }, [pinned.scale])

  useEffect(() => {
    applyAttribute('data-theme-content-layout', pinned.contentLayout)
  }, [pinned.contentLayout])

  useEffect(() => {
    applyAttribute('data-theme-blur', pinned.blur ? 'true' : null)
  }, [pinned.blur])

  useEffect(() => {
    applyAttribute('data-theme-card-blur', block.cardBlur ? 'true' : null)
  }, [block.cardBlur])

  // Continuous axes always arrive as numbers now — a pinned row carries every
  // axis, so no value silently falls back to a stylesheet default that a
  // future release may retune.
  useEffect(() => {
    applyCssVariable('--surface-opacity', `${block.surfaceOpacity}%`)
  }, [block.surfaceOpacity])

  useEffect(() => {
    applyCssVariable('--surface-blur', `${block.surfaceBlur}px`)
  }, [block.surfaceBlur])

  useEffect(() => {
    applyCssVariable(
      '--app-background-scrim',
      `${BACKGROUND_SCRIM_COLOR_BASES[surfaceScheme]} / ${block.backgroundScrim}%)`
    )
  }, [block.backgroundScrim, surfaceScheme])

  useEffect(() => {
    applyCssVariable('--mobile-sheet-opacity', `${block.sheetOpacity}%`)
  }, [block.sheetOpacity])

  useEffect(() => {
    applyCssVariable('--float-opacity', `${block.floatOpacity}%`)
  }, [block.floatOpacity])

  const updateBlock = useCallback(
    (scheme: SurfaceScheme, patch: Partial<PinnedTheme['light']>) => {
      setPinned((previous) =>
        scheme === 'light'
          ? { ...previous, light: { ...previous.light, ...patch } }
          : { ...previous, dark: { ...previous.dark, ...patch } }
      )
      dirtyRef.current = true
    },
    []
  )

  const clampTo = (value: number, limits: { min: number; max: number }) =>
    Math.min(limits.max, Math.max(limits.min, Math.round(value)))

  const setPreset = useCallback((value: ThemePreset) => {
    dirtyRef.current = true
    setPinned((previous) => ({ ...previous, preset: value }))
  }, [])

  const setFont = useCallback((value: ThemeFont) => {
    dirtyRef.current = true
    setPinned((previous) => ({ ...previous, font: value }))
  }, [])

  const setRadius = useCallback((value: ThemeRadius) => {
    dirtyRef.current = true
    setPinned((previous) => ({ ...previous, radius: value }))
  }, [])

  const setScale = useCallback((value: ThemeScale) => {
    dirtyRef.current = true
    setPinned((previous) => ({ ...previous, scale: value }))
  }, [])

  const setContentLayout = useCallback((value: ContentLayout) => {
    dirtyRef.current = true
    setPinned((previous) => ({ ...previous, contentLayout: value }))
  }, [])

  const setBlur = useCallback((value: boolean) => {
    dirtyRef.current = true
    setPinned((previous) => ({ ...previous, blur: value }))
  }, [])

  const setCardBlur = useCallback(
    (value: boolean) => {
      updateBlock(surfaceScheme, { cardBlur: value })
    },
    [surfaceScheme, updateBlock]
  )

  const setSurfaceOpacity = useCallback(
    (value: number) => {
      if (!Number.isFinite(value)) return
      updateBlock(surfaceScheme, {
        surfaceOpacity: clampTo(value, SURFACE_OPACITY_LIMITS),
      })
    },
    [surfaceScheme, updateBlock]
  )

  const setSurfaceBlur = useCallback(
    (value: number) => {
      if (!Number.isFinite(value)) return
      updateBlock(surfaceScheme, {
        surfaceBlur: clampTo(value, SURFACE_BLUR_LIMITS),
      })
    },
    [surfaceScheme, updateBlock]
  )

  const setBackgroundScrim = useCallback(
    (value: number) => {
      if (!Number.isFinite(value)) return
      updateBlock(surfaceScheme, {
        backgroundScrim: clampTo(value, BACKGROUND_SCRIM_LIMITS),
      })
    },
    [surfaceScheme, updateBlock]
  )

  const setSheetOpacity = useCallback(
    (value: number) => {
      if (!Number.isFinite(value)) return
      updateBlock(surfaceScheme, {
        sheetOpacity: clampTo(value, SURFACE_PANEL_OPACITY_LIMITS),
      })
    },
    [surfaceScheme, updateBlock]
  )

  const setFloatOpacity = useCallback(
    (value: number) => {
      if (!Number.isFinite(value)) return
      updateBlock(surfaceScheme, {
        floatOpacity: clampTo(value, SURFACE_PANEL_OPACITY_LIMITS),
      })
    },
    [surfaceScheme, updateBlock]
  )

  // Reset returns the panel to the shipped defaults and clears the shared row
  // with it, so the next release's defaults apply again for every browser.
  // dirtyRef is cleared first so the persistence effect does not immediately
  // re-create the row we are deleting.
  const resetCustomization = useCallback(() => {
    dirtyRef.current = false
    persistedRowRef.current = null
    clearPinnedCookies()
    setPinned({
      preset: DEFAULT_THEME_CUSTOMIZATION.preset,
      font: DEFAULT_THEME_CUSTOMIZATION.font,
      radius: DEFAULT_THEME_CUSTOMIZATION.radius,
      scale: DEFAULT_THEME_CUSTOMIZATION.scale,
      contentLayout: DEFAULT_THEME_CUSTOMIZATION.contentLayout,
      blur: DEFAULT_THEME_CUSTOMIZATION.blur,
      light: surfaceBlockFor('light'),
      dark: surfaceBlockFor('dark'),
    })
    void clearThemeCustomization()
  }, [])

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

  const customization = useMemo<ThemeCustomization>(
    () => ({
      preset: pinned.preset,
      font: pinned.font,
      radius: pinned.radius,
      scale: pinned.scale,
      contentLayout: pinned.contentLayout,
      blur: pinned.blur,
      cardBlur: block.cardBlur,
      surfaceOpacity: block.surfaceOpacity,
      surfaceBlur: block.surfaceBlur,
      backgroundScrim: block.backgroundScrim,
      sheetOpacity: block.sheetOpacity,
      floatOpacity: block.floatOpacity,
    }),
    [pinned, block]
  )

  // Whether the shared row currently governs this theme. False means the
  // panel is only live in this browser — either it has never been pinned, or
  // the viewer cannot write the shared option.
  const themePinnedToServer = useMemo(
    () =>
      parseThemeCustomization(serverRow, DEFAULT_THEME_CUSTOMIZATION) !== null,
    [serverRow]
  )

  const value = useMemo<ThemeCustomizationContextType>(
    () => ({
      defaults,
      customization,
      blur: pinned.blur,
      setBlur,
      cardBlur: block.cardBlur,
      setCardBlur,
      surfaceOpacity: block.surfaceOpacity,
      setSurfaceOpacity,
      surfaceBlur: block.surfaceBlur,
      setSurfaceBlur,
      backgroundScrim: block.backgroundScrim,
      setBackgroundScrim,
      sheetOpacity: block.sheetOpacity,
      setSheetOpacity,
      floatOpacity: block.floatOpacity,
      setFloatOpacity,
      surfaceScheme,
      themePinnedToServer,
      setPreset,
      setFont,
      setRadius,
      setScale,
      setContentLayout,
      resetCustomization,
    }),
    [
      defaults,
      customization,
      pinned.blur,
      block,
      surfaceScheme,
      themePinnedToServer,
      setPreset,
      setFont,
      setRadius,
      setScale,
      setContentLayout,
      setBlur,
      setCardBlur,
      setSurfaceOpacity,
      setSurfaceBlur,
      setBackgroundScrim,
      setSheetOpacity,
      setFloatOpacity,
      resetCustomization,
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

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
import { api } from '@/lib/http-client'
import {
  CONTENT_LAYOUT_VALUES,
  resolveThemeFont,
  surfaceDefaultsFor,
  THEME_FONT_VALUES,
  THEME_PRESET_VALUES,
  THEME_RADIUS_VALUES,
  THEME_SCALE_VALUES,
  type ContentLayout,
  type SurfaceScheme,
  type ThemeCustomization,
  type ThemeFont,
  type ThemePreset,
  type ThemeRadius,
  type ThemeScale,
} from '@/lib/theme-customization'

/**
 * Server-backed theme panel.
 *
 * The theme panel used to live only in browser cookies, and every axis that
 * happened to equal the shipped CSS default was dropped from storage so it
 * could "follow theme.css". An upstream release that retuned those defaults
 * therefore moved an operator's tuned panel with no action on their side.
 *
 * The whole panel is now pinned to one option row: a change is written
 * unconditionally, so the stored value — not whatever the next release
 * happens to ship — is what renders. Cookies stay as the read path that
 * avoids a first-paint flash, and as the offline/local fallback for
 * browsers that have no row yet.
 */
const THEME_CUSTOMIZATION_OPTION_KEY = 'ThemeCustomization'

export type SurfaceValues = {
  cardBlur: boolean
  surfaceOpacity: number
  surfaceBlur: number
  backgroundScrim: number
  sheetOpacity: number
  floatOpacity: number
}

/** The discrete axes plus one surface block per color scheme. */
export type PinnedTheme = {
  preset: ThemePreset
  font: ThemeFont
  radius: ThemeRadius
  scale: ThemeScale
  contentLayout: ContentLayout
  blur: boolean
  light: SurfaceValues
  dark: SurfaceValues
}

function clampNumber(value: number, min: number, max: number): number {
  if (!Number.isFinite(value)) return min
  return Math.min(max, Math.max(min, Math.round(value)))
}

export function surfaceBlockFor(scheme: SurfaceScheme): SurfaceValues {
  const defaults = surfaceDefaultsFor(scheme)
  return {
    cardBlur: defaults.cardBlur,
    surfaceOpacity: defaults.surfaceOpacity,
    surfaceBlur: defaults.surfaceBlur,
    backgroundScrim: defaults.backgroundScrim,
    sheetOpacity: defaults.sheetOpacity,
    floatOpacity: defaults.floatOpacity,
  }
}

/**
 * Build the row payload from the full panel state — both scheme blocks, not
 * just the one on screen. Serializing only the visible scheme would reset the
 * hidden one to CSS defaults the moment the operator touched anything.
 */
export function serializeThemeCustomization(pinned: PinnedTheme): string {
  const payload: Record<string, unknown> = {
    preset: pinned.preset,
    font: pinned.font,
    radius: pinned.radius,
    scale: pinned.scale,
    content_layout: pinned.contentLayout,
    blur: pinned.blur,
    card_blur_light: pinned.light.cardBlur,
    card_blur_dark: pinned.dark.cardBlur,
    surface_opacity_light: pinned.light.surfaceOpacity,
    surface_opacity_dark: pinned.dark.surfaceOpacity,
    surface_blur_light: pinned.light.surfaceBlur,
    surface_blur_dark: pinned.dark.surfaceBlur,
    background_scrim_light: pinned.light.backgroundScrim,
    background_scrim_dark: pinned.dark.backgroundScrim,
    sheet_opacity_light: pinned.light.sheetOpacity,
    sheet_opacity_dark: pinned.dark.sheetOpacity,
    float_opacity_light: pinned.light.floatOpacity,
    float_opacity_dark: pinned.dark.floatOpacity,
  }
  return JSON.stringify(payload)
}

function readEnum<T extends string>(
  value: unknown,
  allowed: ReadonlySet<T>,
  fallback: T
): T {
  return typeof value === 'string' && allowed.has(value as T)
    ? (value as T)
    : fallback
}

function readNumber(
  value: unknown,
  min: number,
  max: number
): number | null {
  if (typeof value !== 'number') return null
  if (!Number.isFinite(value)) return null
  return clampNumber(value, min, max)
}

/** Parse a stored row back into the panel shape. Returns null when the row is
 * absent or unusable so callers fall back to their own defaults. */
export function parseThemeCustomization(
  raw: unknown,
  defaults: ThemeCustomization
): PinnedTheme | null {
  if (typeof raw !== 'string' || raw.trim() === '') return null
  let payload: Record<string, unknown>
  try {
    const parsed: unknown = JSON.parse(raw)
    if (typeof parsed !== 'object' || parsed === null) return null
    payload = parsed as Record<string, unknown>
  } catch {
    return null
  }

  const block = (
    suffix: string,
    fallback: SurfaceValues
  ): SurfaceValues => {
    const numeric = (
      key: string,
      min: number,
      max: number,
      backstop: number
    ) => readNumber(payload[key], min, max) ?? backstop

    return {
      cardBlur:
        typeof payload[`card_blur_${suffix}`] === 'boolean'
          ? (payload[`card_blur_${suffix}`] as boolean)
          : fallback.cardBlur,
      surfaceOpacity: numeric(
        `surface_opacity_${suffix}`,
        10,
        100,
        fallback.surfaceOpacity
      ),
      surfaceBlur: numeric(
        `surface_blur_${suffix}`,
        0,
        40,
        fallback.surfaceBlur
      ),
      backgroundScrim: numeric(
        `background_scrim_${suffix}`,
        0,
        100,
        fallback.backgroundScrim
      ),
      sheetOpacity: numeric(
        `sheet_opacity_${suffix}`,
        50,
        100,
        fallback.sheetOpacity
      ),
      floatOpacity: numeric(
        `float_opacity_${suffix}`,
        50,
        100,
        fallback.floatOpacity
      ),
    }
  }

  return {
    preset: readEnum(payload.preset, THEME_PRESET_VALUES, defaults.preset),
    font: readEnum(payload.font, THEME_FONT_VALUES, defaults.font),
    radius: readEnum(payload.radius, THEME_RADIUS_VALUES, defaults.radius),
    scale: readEnum(payload.scale, THEME_SCALE_VALUES, defaults.scale),
    contentLayout: readEnum(
      payload.content_layout,
      CONTENT_LAYOUT_VALUES,
      defaults.contentLayout
    ),
    blur: typeof payload.blur === 'boolean' ? payload.blur : defaults.blur,
    light: block('light', surfaceBlockFor('light')),
    dark: block('dark', surfaceBlockFor('dark')),
  }
}

/** Resolved font is derived, not stored: the row keeps the preference so a
 * preset's signature typography still applies while the DOM attribute stays
 * a concrete `sans`/`serif`. */
export function pinnedThemeFont(pinned: PinnedTheme): 'sans' | 'serif' {
  return resolveThemeFont(pinned.font, pinned.preset)
}

export async function saveThemeCustomization(value: string): Promise<boolean> {
  try {
    const res = await api.put('/api/option/', {
      key: THEME_CUSTOMIZATION_OPTION_KEY,
      value,
    })
    return res.data?.success === true
  } catch {
    return false
  }
}

/** Clears the pin so the panel falls back to the shipped defaults again. */
export async function clearThemeCustomization(): Promise<boolean> {
  try {
    const res = await api.put('/api/option/', {
      key: THEME_CUSTOMIZATION_OPTION_KEY,
      value: '',
    })
    return res.data?.success === true
  } catch {
    return false
  }
}

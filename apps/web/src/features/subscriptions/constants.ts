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
import { type TFunction } from 'i18next'

// ============================================================================
// Subscription Expiry / Limit Modes
// ============================================================================

// The plan editor exposes two orthogonal single-select axes: how the
// subscription ends (expiry) and what it grants (limit). The backend collapses
// them into the stored kind; these helpers derive the axes back for any plan
// payload, including legacy ones that only carry kind.
export type PlanExpireMode = 'time' | 'quota'
export type PlanLimitMode = 'group' | 'request'

export function planExpireMode(plan: {
  kind?: string
  expire_mode?: string
}): PlanExpireMode {
  if (plan.expire_mode === 'time' || plan.expire_mode === 'quota') {
    return plan.expire_mode
  }
  return plan.kind === 'quota' || plan.kind === 'request' ? 'quota' : 'time'
}

export function planLimitMode(plan: {
  kind?: string
  limit_mode?: string
}): PlanLimitMode {
  if (plan.limit_mode === 'group' || plan.limit_mode === 'request') {
    return plan.limit_mode
  }
  return plan.kind === 'request' ? 'request' : 'group'
}

export const SUBSCRIPTION_EXPIRE_MODES = [
  { value: 'time', labelKey: 'Expires by time' },
  { value: 'quota', labelKey: 'Expires by quota' },
] as const

export const SUBSCRIPTION_LIMIT_MODES = [
  { value: 'group', labelKey: 'Group limit' },
  { value: 'request', labelKey: 'Request limit' },
] as const

export function getPlanExpireModeOptions(t: TFunction) {
  return SUBSCRIPTION_EXPIRE_MODES.map((mode) => ({
    value: mode.value,
    label: t(mode.labelKey),
  }))
}

export function getPlanLimitModeOptions(t: TFunction) {
  return SUBSCRIPTION_LIMIT_MODES.map((mode) => ({
    value: mode.value,
    label: t(mode.labelKey),
  }))
}

export function getSubscriptionModeLabel(
  plan: { kind?: string; expire_mode?: string; limit_mode?: string },
  t: TFunction
): string {
  const expire =
    planExpireMode(plan) === 'quota' ? 'Expires by quota' : 'Expires by time'
  const limit =
    planLimitMode(plan) === 'request' ? 'Request limit' : 'Group limit'
  return `${t(expire)} · ${t(limit)}`
}

// ============================================================================
// Duration Unit Options
// ============================================================================

const DURATION_UNITS = [
  { value: 'year', labelKey: 'years' },
  { value: 'month', labelKey: 'months' },
  { value: 'day', labelKey: 'days' },
  { value: 'hour', labelKey: 'hours' },
  { value: 'custom', labelKey: 'Custom (seconds)' },
] as const

const RESET_PERIODS = [
  { value: 'never', labelKey: 'No Reset' },
  { value: 'daily', labelKey: 'Daily' },
  { value: 'weekly', labelKey: 'Weekly' },
  { value: 'monthly', labelKey: 'Monthly' },
  { value: 'custom', labelKey: 'Custom (seconds)' },
] as const

export function getDurationUnitOptions(t: TFunction) {
  return DURATION_UNITS.map((u) => ({ value: u.value, label: t(u.labelKey) }))
}

export function getResetPeriodOptions(t: TFunction) {
  return RESET_PERIODS.map((p) => ({ value: p.value, label: t(p.labelKey) }))
}

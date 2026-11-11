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

export function planExpireMode(plan: {
  kind?: string
  expire_mode?: string
}): PlanExpireMode {
  if (plan.expire_mode === 'time' || plan.expire_mode === 'quota') {
    return plan.expire_mode
  }
  return plan.kind === 'quota' || plan.kind === 'request' ? 'quota' : 'time'
}

// planScope/planMeasure resolve the limit axes of a plan payload, falling back
// to the legacy kind for rows saved before the axes existed.
export type PlanScope = 'all' | 'group' | 'model'
export type PlanMeasure = 'quota' | 'count'

export function planScope(plan: {
  kind?: string
  scope?: string
}): PlanScope {
  if (plan.scope === 'all' || plan.scope === 'group' || plan.scope === 'model') {
    return plan.scope
  }
  return plan.kind === 'request' ? 'model' : 'all'
}

export function planMeasure(plan: {
  kind?: string
  measure?: string
}): PlanMeasure {
  if (plan.measure === 'quota' || plan.measure === 'count') {
    return plan.measure
  }
  return plan.kind === 'request' ? 'count' : 'quota'
}

export const SUBSCRIPTION_EXPIRE_MODES = [
  { value: 'time', labelKey: 'Expires by time' },
  { value: 'quota', labelKey: 'Expires by quota' },
] as const

export function getPlanExpireModeOptions(t: TFunction) {
  return SUBSCRIPTION_EXPIRE_MODES.map((mode) => ({
    value: mode.value,
    label: t(mode.labelKey),
  }))
}

// The limit axes the editor exposes as small tab rows: what the granted amount
// applies to (scope) and how it is counted (measure).
export const SUBSCRIPTION_SCOPES = [
  { value: 'all', labelKey: 'Shared quota' },
  { value: 'group', labelKey: 'Per group' },
  { value: 'model', labelKey: 'Per model' },
] as const

export const SUBSCRIPTION_MEASURES = [
  { value: 'quota', labelKey: 'Quota' },
  { value: 'count', labelKey: 'Request count' },
] as const

export function getPlanScopeOptions(t: TFunction) {
  return SUBSCRIPTION_SCOPES.map((scope) => ({
    value: scope.value,
    label: t(scope.labelKey),
  }))
}

export function getPlanMeasureOptions(t: TFunction) {
  return SUBSCRIPTION_MEASURES.map((measure) => ({
    value: measure.value,
    label: t(measure.labelKey),
  }))
}

export function getSubscriptionModeLabel(
  plan: { kind?: string; expire_mode?: string; scope?: string; measure?: string },
  t: TFunction
): string {
  const expire =
    planExpireMode(plan) === 'quota' ? 'Expires by quota' : 'Expires by time'
  const scopeValue =
    plan.scope === 'group' || plan.scope === 'model'
      ? plan.scope
      : plan.kind === 'request'
        ? 'model'
        : 'all'
  const scopeLabel =
    scopeValue === 'group'
      ? 'Per group'
      : scopeValue === 'model'
        ? 'Per model'
        : 'Shared quota'
  const measureValue =
    plan.measure === 'quota' || plan.measure === 'count'
      ? plan.measure
      : plan.kind === 'request'
        ? 'count'
        : 'quota'
  if (scopeValue === 'all') {
    return `${t(expire)} · ${t(scopeLabel)}`
  }
  return `${t(expire)} · ${t(scopeLabel)} · ${t(measureValue === 'quota' ? 'Quota' : 'Request count')}`
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

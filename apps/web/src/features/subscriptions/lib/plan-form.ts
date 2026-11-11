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
import type { TFunction } from 'i18next'
import { z } from 'zod'

import {
  planExpireMode,
  planMeasure,
  planScope,
  type PlanMeasure,
} from '../constants'
import type { SubscriptionPlan, PlanPayload } from '../types'

// One row per model in the request-limits matrix; counts maps each upgrade
// group to the amount it grants (request counts or quota units, per the plan's
// measure). A group missing from the map (or carrying 0) grants nothing.
export interface RequestMatrixRow {
  model: string
  counts: Record<string, number>
}

export function getPlanFormSchema(t: TFunction) {
  return z
    .object({
      title: z.string().min(1, t('Please enter plan title')),
      subtitle: z.string().optional(),
      price_amount: z.coerce.number().min(0, t('Please enter amount')),
      expire_mode: z.enum(['time', 'quota']),
      scope: z.enum(['all', 'group', 'model']),
      measure: z.enum(['quota', 'count']),
      duration_unit: z.enum(['year', 'month', 'day', 'hour', 'custom']),
      duration_value: z.coerce.number().min(1),
      custom_seconds: z.coerce.number().min(0).optional(),
      quota_reset_period: z.enum([
        'never',
        'daily',
        'weekly',
        'monthly',
        'custom',
      ]),
      quota_reset_custom_seconds: z.coerce.number().min(0).optional(),
      enabled: z.boolean(),
      sort_order: z.coerce.number(),
      allow_balance_pay: z.boolean(),
      allow_wallet_overflow: z.boolean(),
      max_purchase_per_user: z.coerce.number().min(0),
      total_amount: z.coerce.number().min(0),
      upgrade_groups: z.array(z.string()),
      downgrade_group: z.string().optional(),
      // The model matrix: the editor edits (model x group) values; the payload
      // collapses it back into per-group rules.
      request_rows: z.array(
        z.object({
          model: z.string().min(1),
          counts: z.record(z.string(), z.coerce.number().min(0)),
        })
      ),
      // The group table: one value per upgrade group for group-scoped plans.
      group_values: z.record(z.string(), z.coerce.number().min(0)),
      stripe_price_id: z.string().optional(),
      creem_product_id: z.string().optional(),
      waffo_pancake_product_id: z.string().optional(),
    })
    .superRefine((values, ctx) => {
      if (values.expire_mode === 'quota' && values.scope === 'all') {
        if (values.total_amount <= 0) {
          ctx.addIssue({
            code: 'custom',
            path: ['total_amount'],
            message: t('Quota-exhausted plans require a positive total quota'),
          })
        }
      }
      if (values.scope === 'group') {
        const hasGrant = Object.values(values.group_values).some(
          (value) => value > 0
        )
        if (!hasGrant) {
          ctx.addIssue({
            code: 'custom',
            path: ['group_values'],
            message: t(
              'Group limits need at least one group with a positive amount'
            ),
          })
        }
      }
      if (values.scope === 'model') {
        const hasGrant = values.request_rows.some((row) =>
          Object.values(row.counts).some((value) => value > 0)
        )
        if (!hasGrant) {
          ctx.addIssue({
            code: 'custom',
            path: ['request_rows'],
            message: t(
              'Request limits need at least one model with a positive count'
            ),
          })
        }
        // The matrix keys on the model itself; a second row for the same
        // model would be invisible data the backend rejects.
        const seenModels = new Set<string>()
        for (const row of values.request_rows) {
          if (seenModels.has(row.model)) {
            ctx.addIssue({
              code: 'custom',
              path: ['request_rows'],
              message: t('Each model can only be added once'),
            })
          } else {
            seenModels.add(row.model)
          }
        }
        // Values for groups the plan no longer upgrades are invisible in the
        // table (its columns are the upgrade groups) but still configured.
        const upgradeSet = new Set(values.upgrade_groups)
        const orphaned: string[] = []
        for (const row of values.request_rows) {
          for (const [group, value] of Object.entries(row.counts)) {
            if (value > 0 && !upgradeSet.has(group)) {
              orphaned.push(`${row.model} (${group})`)
            }
          }
        }
        if (orphaned.length > 0) {
          ctx.addIssue({
            code: 'custom',
            path: ['request_rows'],
            message: `${t(
              'These models are limited in groups the plan no longer upgrades'
            )}: ${orphaned.join(', ')}`,
          })
        }
      }
    })
}

export type PlanFormValues = z.infer<ReturnType<typeof getPlanFormSchema>>

export const PLAN_FORM_DEFAULTS: PlanFormValues = {
  title: '',
  subtitle: '',
  price_amount: 0,
  expire_mode: 'time',
  scope: 'all',
  measure: 'quota',
  duration_unit: 'month',
  duration_value: 1,
  custom_seconds: 0,
  quota_reset_period: 'never',
  quota_reset_custom_seconds: 0,
  enabled: true,
  sort_order: 0,
  allow_balance_pay: true,
  allow_wallet_overflow: true,
  max_purchase_per_user: 0,
  total_amount: 0,
  upgrade_groups: [],
  downgrade_group: '',
  request_rows: [],
  group_values: {},
  stripe_price_id: '',
  creem_product_id: '',
  waffo_pancake_product_id: '',
}

export function getPlanUpgradeGroups(
  plan: Partial<Pick<SubscriptionPlan, 'upgrade_groups' | 'upgrade_group'>>
): string[] {
  if (plan.upgrade_groups && plan.upgrade_groups.length > 0) {
    return plan.upgrade_groups
  }
  return plan.upgrade_group ? [plan.upgrade_group] : []
}

// planRequestRows transposes the plan's per-group rules into the matrix the
// editor renders: one row per model, keyed by group. The cell value is the
// rule's count or quota amount per the plan's measure.
export function planRequestRows(plan: SubscriptionPlan): RequestMatrixRow[] {
  const order: string[] = []
  const counts: Record<string, Record<string, number>> = {}
  const quota = planMeasure(plan) === 'quota'
  for (const rule of plan.request_rules || []) {
    const value = quota
      ? Number(rule.amount_display || 0)
      : Number(rule.count || 0)
    for (const model of rule.models || []) {
      if (!counts[model]) {
        counts[model] = {}
        order.push(model)
      }
      counts[model][rule.group] = value
    }
  }
  return order.map((model) => ({ model, counts: counts[model] }))
}

// planGroupValues transposes group-scoped rules into the per-group table.
export function planGroupValues(plan: SubscriptionPlan): Record<string, number> {
  const values: Record<string, number> = {}
  const quota = planMeasure(plan) === 'quota'
  for (const rule of plan.request_rules || []) {
    values[rule.group] = quota
      ? Number(rule.amount_display || 0)
      : Number(rule.count || 0)
  }
  return values
}

export function planToFormValues(plan: SubscriptionPlan): PlanFormValues {
  return {
    title: plan.title || '',
    subtitle: plan.subtitle || '',
    price_amount: Number(plan.price_amount || 0),
    expire_mode: planExpireMode(plan),
    scope: planScope(plan),
    measure: planMeasure(plan),
    duration_unit: plan.duration_unit || 'month',
    duration_value: Number(plan.duration_value || 1),
    custom_seconds: Number(plan.custom_seconds || 0),
    quota_reset_period: plan.quota_reset_period || 'never',
    quota_reset_custom_seconds: Number(plan.quota_reset_custom_seconds || 0),
    enabled: plan.enabled !== false,
    sort_order: Number(plan.sort_order || 0),
    allow_balance_pay: plan.allow_balance_pay !== false,
    allow_wallet_overflow: plan.allow_wallet_overflow !== false,
    max_purchase_per_user: Number(plan.max_purchase_per_user || 0),
    total_amount: plan.total_amount_display ?? 0,
    upgrade_groups: getPlanUpgradeGroups(plan),
    downgrade_group: plan.downgrade_group || '',
    request_rows: planRequestRows(plan),
    group_values: planGroupValues(plan),
    stripe_price_id: plan.stripe_price_id || '',
    creem_product_id: plan.creem_product_id || '',
    waffo_pancake_product_id: plan.waffo_pancake_product_id || '',
  }
}

type RuleDraft = NonNullable<SubscriptionPlan['request_rules']>[number]

// requestRowsToRules collapses the matrix back into the per-group rules the
// backend stores: models sharing a value ride in one rule, and values for
// groups the plan no longer upgrades are dropped.
export function requestRowsToRules(
  rows: RequestMatrixRow[],
  groups: string[],
  measure: PlanMeasure
): RuleDraft[] {
  const upgradeGroups = new Set(groups)
  // group -> value -> models
  const buckets: Record<string, Map<number, string[]>> = {}
  for (const row of rows) {
    for (const [group, value] of Object.entries(row.counts)) {
      if (value > 0 && upgradeGroups.has(group)) {
        const byValue = (buckets[group] ??= new Map())
        const models = byValue.get(value) ?? []
        models.push(row.model)
        byValue.set(value, models)
      }
    }
  }
  const rules: RuleDraft[] = []
  for (const [group, byValue] of Object.entries(buckets)) {
    for (const [value, models] of byValue) {
      rules.push({
        group,
        models,
        count: measure === 'count' ? value : 0,
        amount_display: measure === 'quota' ? value : 0,
      })
    }
  }
  return rules
}

// groupValuesToRules builds the group-scoped rules from the per-group table.
export function groupValuesToRules(
  values: Record<string, number>,
  groups: string[],
  measure: PlanMeasure
): RuleDraft[] {
  const upgradeGroups = new Set(groups)
  const rules: RuleDraft[] = []
  for (const [group, value] of Object.entries(values)) {
    if (value > 0 && upgradeGroups.has(group)) {
      rules.push({
        group,
        models: [],
        count: measure === 'count' ? value : 0,
        amount_display: measure === 'quota' ? value : 0,
      })
    }
  }
  return rules
}

export function formValuesToPlanPayload(values: PlanFormValues): PlanPayload {
  // The matrix and the group table are editor-only views; the payload carries
  // the rules they collapse into.
  const { request_rows, group_values, ...rest } = values
  const scoped = values.scope !== 'all'
  return {
    plan: {
      ...rest,
      price_amount: Number(values.price_amount || 0),
      currency: 'USD',
      duration_value: Number(values.duration_value || 0),
      quota_reset_period:
        values.expire_mode === 'time'
          ? values.quota_reset_period || 'never'
          : 'never',
      quota_reset_custom_seconds:
        values.expire_mode === 'time' &&
        values.quota_reset_period === 'custom'
          ? Number(values.quota_reset_custom_seconds || 0)
          : 0,
      sort_order: Number(values.sort_order || 0),
      max_purchase_per_user: Number(values.max_purchase_per_user || 0),
      total_amount_display: scoped ? 0 : Number(values.total_amount || 0),
      upgrade_group: values.upgrade_groups[0] || '',
      downgrade_group: values.downgrade_group || '',
      request_rules:
        values.scope === 'group'
          ? groupValuesToRules(group_values, values.upgrade_groups, values.measure)
          : values.scope === 'model'
            ? requestRowsToRules(
                request_rows,
                values.upgrade_groups,
                values.measure
              )
            : [],
    },
  }
}

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

import { planExpireMode, planLimitMode } from '../constants'
import type { SubscriptionPlan, PlanPayload } from '../types'

export function getPlanFormSchema(t: TFunction) {
  return z
    .object({
      title: z.string().min(1, t('Please enter plan title')),
      subtitle: z.string().optional(),
      price_amount: z.coerce.number().min(0, t('Please enter amount')),
      expire_mode: z.enum(['time', 'quota']),
      limit_mode: z.enum(['group', 'request']),
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
      request_rules: z.array(
        z.object({
          group: z.string(),
          models: z.array(z.string()),
          count: z.coerce.number().min(1),
        })
      ),
      stripe_price_id: z.string().optional(),
      creem_product_id: z.string().optional(),
      waffo_pancake_product_id: z.string().optional(),
    })
    .superRefine((values, ctx) => {
      if (
        values.expire_mode === 'quota' &&
        values.limit_mode === 'group' &&
        values.total_amount <= 0
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['total_amount'],
          message: t('Quota-exhausted plans require a positive total quota'),
        })
      }
      if (values.limit_mode === 'request') {
        const incomplete = values.request_rules.some(
          (rule) => !rule.group || rule.models.length === 0 || rule.count <= 0
        )
        if (values.request_rules.length === 0 || incomplete) {
          ctx.addIssue({
            code: 'custom',
            path: ['request_rules'],
            message: t(
              'Request limits need a group, models and a positive count'
            ),
          })
        }
        // The backend keys counters on (group, model); a repeat would
        // silently grant the sum under a single bar.
        const seen = new Set<string>()
        for (const rule of values.request_rules) {
          for (const model of rule.models) {
            const key = `${rule.group}\u0000${model}`
            if (seen.has(key)) {
              ctx.addIssue({
                code: 'custom',
                path: ['request_rules'],
                message: t('Each model can only be limited once per group'),
              })
            } else {
              seen.add(key)
            }
          }
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
  limit_mode: 'group',
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
  request_rules: [],
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

export function planToFormValues(plan: SubscriptionPlan): PlanFormValues {
  return {
    title: plan.title || '',
    subtitle: plan.subtitle || '',
    price_amount: Number(plan.price_amount || 0),
    expire_mode: planExpireMode(plan),
    limit_mode: planLimitMode(plan),
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
    request_rules: (plan.request_rules || []).map((rule) => ({
      group: rule.group || '',
      models: rule.models || [],
      count: Number(rule.count || 1),
    })),
    stripe_price_id: plan.stripe_price_id || '',
    creem_product_id: plan.creem_product_id || '',
    waffo_pancake_product_id: plan.waffo_pancake_product_id || '',
  }
}

export function formValuesToPlanPayload(values: PlanFormValues): PlanPayload {
  return {
    plan: {
      ...values,
      price_amount: Number(values.price_amount || 0),
      currency: 'USD',
      duration_value: Number(values.duration_value || 0),
      quota_reset_period:
        values.limit_mode === 'group' && values.expire_mode === 'time'
          ? values.quota_reset_period || 'never'
          : 'never',
      quota_reset_custom_seconds:
        values.limit_mode === 'group' &&
        values.expire_mode === 'time' &&
        values.quota_reset_period === 'custom'
          ? Number(values.quota_reset_custom_seconds || 0)
          : 0,
      sort_order: Number(values.sort_order || 0),
      max_purchase_per_user: Number(values.max_purchase_per_user || 0),
      total_amount_display: Number(values.total_amount || 0),
      upgrade_group: values.upgrade_groups[0] || '',
      downgrade_group: values.downgrade_group || '',
    },
  }
}

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
import { describe, test } from 'bun:test'
import assert from 'node:assert/strict'

import type { TFunction } from 'i18next'

import type { SubscriptionPlan } from '../../types'
import {
  formValuesToPlanPayload,
  getPlanFormSchema,
  getPlanUpgradeGroups,
  PLAN_FORM_DEFAULTS,
  planToFormValues,
} from '../plan-form'

const t = ((key: string) => key) as unknown as TFunction

const basePlan = {
  id: 1,
  title: 'plan',
  price_amount: 1,
  currency: 'USD',
  duration_unit: 'month',
  duration_value: 1,
  quota_reset_period: 'monthly',
  enabled: true,
  sort_order: 0,
  allow_balance_pay: true,
  allow_wallet_overflow: true,
  max_purchase_per_user: 0,
  total_amount: 5,
} as unknown as SubscriptionPlan

describe('getPlanUpgradeGroups', () => {
  test('prefers the upgrade_groups snapshot', () => {
    assert.deepEqual(
      getPlanUpgradeGroups({
        upgrade_groups: ['vip', 'svip'],
        upgrade_group: 'default',
      }),
      ['vip', 'svip']
    )
  })

  test('falls back to the legacy single group', () => {
    assert.deepEqual(getPlanUpgradeGroups({ upgrade_group: 'vip' }), ['vip'])
  })

  test('returns empty for unrestricted plans', () => {
    assert.deepEqual(getPlanUpgradeGroups({}), [])
  })
})

describe('planToFormValues', () => {
  test('backfills legacy plans without kind or request fields', () => {
    const values = planToFormValues(basePlan)
    assert.equal(values.kind, 'time')
    assert.deepEqual(values.upgrade_groups, [])
    assert.deepEqual(values.request_rules, [])
  })

  test('snapshots multi-group and request rules from the plan', () => {
    const plan = {
      ...basePlan,
      kind: 'request',
      upgrade_groups: ['vip', 'svip'],
      request_rules: [{ group: 'vip', models: ['gpt-4o'], count: 3 }],
    } as unknown as SubscriptionPlan
    const values = planToFormValues(plan)
    assert.equal(values.kind, 'request')
    assert.deepEqual(values.upgrade_groups, ['vip', 'svip'])
    assert.deepEqual(values.request_rules, [
      { group: 'vip', models: ['gpt-4o'], count: 3 },
    ])
  })
})

describe('formValuesToPlanPayload', () => {
  test('time kind keeps the chosen reset period and custom seconds', () => {
    const payload = formValuesToPlanPayload({
      ...PLAN_FORM_DEFAULTS,
      kind: 'time',
      quota_reset_period: 'custom',
      quota_reset_custom_seconds: 90,
      upgrade_groups: ['vip', 'svip'],
    })
    assert.equal(payload.plan.quota_reset_period, 'custom')
    assert.equal(payload.plan.quota_reset_custom_seconds, 90)
    assert.equal(payload.plan.upgrade_group, 'vip')
    assert.equal(payload.plan.kind, 'time')
  })

  test('quota and request kinds force the reset period to never', () => {
    for (const kind of ['quota', 'request'] as const) {
      const payload = formValuesToPlanPayload({
        ...PLAN_FORM_DEFAULTS,
        kind,
        quota_reset_period: 'daily',
        quota_reset_custom_seconds: 90,
      })
      assert.equal(payload.plan.quota_reset_period, 'never')
      assert.equal(payload.plan.quota_reset_custom_seconds, 0)
      assert.equal(payload.plan.kind, kind)
    }
  })

  test('request rules and upgrade groups ride through to the payload', () => {
    const payload = formValuesToPlanPayload({
      ...PLAN_FORM_DEFAULTS,
      kind: 'request',
      upgrade_groups: ['vip'],
      request_rules: [{ group: 'vip', models: ['gpt-4o', 'claude'], count: 2 }],
    })
    assert.deepEqual(payload.plan.request_rules, [
      { group: 'vip', models: ['gpt-4o', 'claude'], count: 2 },
    ])
    assert.deepEqual(payload.plan.upgrade_groups, ['vip'])
    assert.equal(payload.plan.upgrade_group, 'vip')
  })
})

describe('getPlanFormSchema quota validation', () => {
  const schema = getPlanFormSchema(t)

  const baseValues = {
    ...PLAN_FORM_DEFAULTS,
    title: 'plan',
    kind: 'quota',
  }

  test('quota kind requires a positive total amount', () => {
    const result = schema.safeParse({ ...baseValues, total_amount: 0 })
    assert.equal(result.success, false)
    if (!result.success) {
      const paths = result.error.issues.map((issue) => issue.path.join('.'))
      assert.ok(
        paths.includes('total_amount'),
        `expected total_amount issue, got ${paths}`
      )
    }
  })

  test('quota kind with a positive total amount passes', () => {
    const result = schema.safeParse({ ...baseValues, total_amount: 10 })
    assert.equal(result.success, true)
  })

  test('time kind still allows zero (unlimited) quota', () => {
    const result = schema.safeParse({
      ...baseValues,
      kind: 'time',
      total_amount: 0,
    })
    assert.equal(result.success, true)
  })
})

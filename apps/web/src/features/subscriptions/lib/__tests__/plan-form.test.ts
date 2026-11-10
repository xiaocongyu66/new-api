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
  planRequestRows,
  planToFormValues,
  requestRowsToRules,
} from '../plan-form'
import { planExpireMode, planLimitMode } from '../../constants'

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
  test('backfills legacy plans without mode fields', () => {
    const values = planToFormValues(basePlan)
    assert.equal(values.expire_mode, 'time')
    assert.equal(values.limit_mode, 'group')
    assert.deepEqual(values.upgrade_groups, [])
    assert.deepEqual(values.request_rows, [])
  })

  test('derives request-limit plans from the stored kind', () => {
    const plan = {
      ...basePlan,
      kind: 'request',
      upgrade_groups: ['vip', 'svip'],
      request_rules: [{ group: 'vip', models: ['gpt-4o'], count: 3 }],
    } as unknown as SubscriptionPlan
    const values = planToFormValues(plan)
    assert.equal(values.limit_mode, 'request')
    // Legacy request plans never carried a term, so they stay quota-expired.
    assert.equal(values.expire_mode, 'quota')
    assert.deepEqual(values.upgrade_groups, ['vip', 'svip'])
  })

  test('keeps an explicit time term on request-limit plans', () => {
    const plan = {
      ...basePlan,
      kind: 'request',
      expire_mode: 'time',
      request_rules: [{ group: 'vip', models: ['gpt-4o'], count: 3 }],
    } as unknown as SubscriptionPlan
    const values = planToFormValues(plan)
    assert.equal(values.limit_mode, 'request')
    assert.equal(values.expire_mode, 'time')
  })
})

describe('planExpireMode / planLimitMode derive the axes from kind', () => {
  test('time kind maps to time expiry and group limit', () => {
    assert.equal(planExpireMode({ kind: 'time' }), 'time')
    assert.equal(planLimitMode({ kind: 'time' }), 'group')
  })

  test('quota kind maps to quota expiry and group limit', () => {
    assert.equal(planExpireMode({ kind: 'quota' }), 'quota')
    assert.equal(planLimitMode({ kind: 'quota' }), 'group')
  })

  test('request kind maps to request limit with quota expiry fallback', () => {
    assert.equal(planLimitMode({ kind: 'request' }), 'request')
    assert.equal(planExpireMode({ kind: 'request' }), 'quota')
    assert.equal(planExpireMode({ kind: 'request', expire_mode: 'time' }), 'time')
  })
})

describe('planRequestRows', () => {
  test('transposes per-group rules into one row per model', () => {
    const plan = {
      ...basePlan,
      request_rules: [
        { group: 'vip', models: ['gpt-4o', 'claude'], count: 5 },
        { group: 'svip', models: ['gpt-4o'], count: 10 },
      ],
    } as unknown as SubscriptionPlan
    assert.deepEqual(planRequestRows(plan), [
      { model: 'gpt-4o', counts: { vip: 5, svip: 10 } },
      { model: 'claude', counts: { vip: 5 } },
    ])
  })

  test('survives rules without models', () => {
    const plan = {
      ...basePlan,
      request_rules: [{ group: 'vip', models: [], count: 5 }],
    } as unknown as SubscriptionPlan
    assert.deepEqual(planRequestRows(plan), [])
  })
})

describe('requestRowsToRules', () => {
  test('merges models sharing a count into one rule per group', () => {
    const rows = [
      { model: 'gpt-4o', counts: { vip: 5, svip: 10 } },
      { model: 'claude', counts: { vip: 5 } },
    ]
    assert.deepEqual(requestRowsToRules(rows, ['vip', 'svip']), [
      { group: 'vip', models: ['gpt-4o', 'claude'], count: 5 },
      { group: 'svip', models: ['gpt-4o'], count: 10 },
    ])
  })

  test('drops counts for groups the plan no longer upgrades', () => {
    const rows = [{ model: 'gpt-4o', counts: { vip: 5, stale: 99 } }]
    assert.deepEqual(requestRowsToRules(rows, ['vip']), [
      { group: 'vip', models: ['gpt-4o'], count: 5 },
    ])
  })

  test('zeros and empty cells grant nothing', () => {
    const rows = [{ model: 'gpt-4o', counts: { vip: 0 } }]
    assert.deepEqual(requestRowsToRules(rows, ['vip']), [])
  })
})

describe('formValuesToPlanPayload', () => {
  test('group limit with time expiry keeps the chosen reset period', () => {
    const payload = formValuesToPlanPayload({
      ...PLAN_FORM_DEFAULTS,
      quota_reset_period: 'custom',
      quota_reset_custom_seconds: 90,
      upgrade_groups: ['vip', 'svip'],
    })
    assert.equal(payload.plan.quota_reset_period, 'custom')
    assert.equal(payload.plan.quota_reset_custom_seconds, 90)
    assert.equal(payload.plan.upgrade_group, 'vip')
    assert.equal(payload.plan.expire_mode, 'time')
    assert.equal(payload.plan.limit_mode, 'group')
    assert.equal('kind' in payload.plan, false)
    assert.equal('request_rows' in payload.plan, false)
  })

  test('quota expiry and request limit force the reset period to never', () => {
    for (const values of [
      { ...PLAN_FORM_DEFAULTS, expire_mode: 'quota', total_amount: 10 },
      { ...PLAN_FORM_DEFAULTS, limit_mode: 'request' },
    ]) {
      const payload = formValuesToPlanPayload({
        ...values,
        quota_reset_period: 'daily',
        quota_reset_custom_seconds: 90,
      })
      assert.equal(payload.plan.quota_reset_period, 'never')
      assert.equal(payload.plan.quota_reset_custom_seconds, 0)
    }
  })

  test('the matrix collapses into rules and upgrade groups ride through', () => {
    const payload = formValuesToPlanPayload({
      ...PLAN_FORM_DEFAULTS,
      limit_mode: 'request',
      upgrade_groups: ['vip', 'svip'],
      request_rows: [
        { model: 'gpt-4o', counts: { vip: 5 } },
        { model: 'claude', counts: { svip: 7 } },
      ],
    })
    assert.deepEqual(payload.plan.request_rules, [
      { group: 'vip', models: ['gpt-4o'], count: 5 },
      { group: 'svip', models: ['claude'], count: 7 },
    ])
    assert.deepEqual(payload.plan.upgrade_groups, ['vip', 'svip'])
    assert.equal(payload.plan.upgrade_group, 'vip')
  })
})

describe('getPlanFormSchema axis validation', () => {
  const schema = getPlanFormSchema(t)

  const baseValues = {
    ...PLAN_FORM_DEFAULTS,
    title: 'plan',
    expire_mode: 'quota',
    limit_mode: 'group',
  }

  test('quota expiry with group limit requires a positive total amount', () => {
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

  test('quota expiry with group limit and a positive total passes', () => {
    const result = schema.safeParse({ ...baseValues, total_amount: 10 })
    assert.equal(result.success, true)
  })

  test('time expiry with group limit still allows zero (unlimited) quota', () => {
    const result = schema.safeParse({
      ...baseValues,
      expire_mode: 'time',
      total_amount: 0,
    })
    assert.equal(result.success, true)
  })

  test('request limit needs at least one model with a positive count', () => {
    const empty = schema.safeParse({
      ...baseValues,
      expire_mode: 'time',
      limit_mode: 'request',
      request_rows: [],
    })
    assert.equal(empty.success, false)

    const allZero = schema.safeParse({
      ...baseValues,
      expire_mode: 'time',
      limit_mode: 'request',
      request_rows: [{ model: 'gpt-4o', counts: { vip: 0 } }],
    })
    assert.equal(allZero.success, false)
  })

  test('request limit accepts a filled matrix', () => {
    const result = schema.safeParse({
      ...baseValues,
      expire_mode: 'time',
      limit_mode: 'request',
      upgrade_groups: ['vip', 'svip'],
      request_rows: [
        { model: 'gpt-4o', counts: { vip: 5 } },
        { model: 'claude', counts: { svip: 3, vip: 5 } },
      ],
    })
    assert.equal(result.success, true)
  })

  test('the same model in different groups is not a duplicate', () => {
    const result = schema.safeParse({
      ...baseValues,
      expire_mode: 'time',
      limit_mode: 'request',
      upgrade_groups: ['vip', 'svip'],
      request_rows: [
        { model: 'gpt-4o', counts: { vip: 5, svip: 3 } },
      ],
    })
    assert.equal(result.success, true)
  })

  test('a second row for the same model is rejected', () => {
    const result = schema.safeParse({
      ...baseValues,
      expire_mode: 'time',
      limit_mode: 'request',
      request_rows: [
        { model: 'gpt-4o', counts: { vip: 5 } },
        { model: 'gpt-4o', counts: { svip: 3 } },
      ],
    })
    assert.equal(result.success, false)
    if (!result.success) {
      const messages = result.error.issues.map((issue) => issue.message)
      assert.ok(
        messages.includes('Each model can only be added once'),
        `expected the duplicate-row issue, got ${messages}`
      )
    }
  })
})

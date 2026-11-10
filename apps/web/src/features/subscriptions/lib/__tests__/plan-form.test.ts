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
  groupValuesToRules,
  PLAN_FORM_DEFAULTS,
  planGroupValues,
  planMeasure,
  planRequestRows,
  planScope,
  planToFormValues,
  requestRowsToRules,
  type PlanFormValues,
  type RequestMatrixRow,
} from '../plan-form'
import { planExpireMode } from '../../constants'

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
})

describe('plan axes fall back to the legacy kind', () => {
  test('time kind is a shared quota pool', () => {
    assert.equal(planExpireMode({ kind: 'time' }), 'time')
    assert.equal(planScope({ kind: 'time' }), 'all')
    assert.equal(planMeasure({ kind: 'time' }), 'quota')
  })

  test('request kind is per-model request counts', () => {
    assert.equal(planScope({ kind: 'request' }), 'model')
    assert.equal(planMeasure({ kind: 'request' }), 'count')
  })

  test('explicit axes win over the kind', () => {
    assert.equal(planScope({ kind: 'time', scope: 'group' }), 'group')
    assert.equal(planMeasure({ kind: 'time', measure: 'count' }), 'count')
  })
})

describe('planToFormValues', () => {
  test('backfills legacy plans to the shared pool', () => {
    const values = planToFormValues(basePlan)
    assert.equal(values.expire_mode, 'time')
    assert.equal(values.scope, 'all')
    assert.equal(values.measure, 'quota')
    assert.deepEqual(values.request_rows, [])
    assert.deepEqual(values.group_values, {})
  })

  test('expands model-scoped count rules into matrix rows', () => {
    const plan = {
      ...basePlan,
      kind: 'request',
      upgrade_groups: ['vip', 'svip'],
      request_rules: [
        { group: 'vip', models: ['gpt-4o', 'claude'], count: 5 },
        { group: 'svip', models: ['gpt-4o'], count: 10 },
      ],
    } as unknown as SubscriptionPlan
    const values = planToFormValues(plan)
    assert.equal(values.scope, 'model')
    assert.equal(values.measure, 'count')
    assert.deepEqual(values.request_rows, [
      { model: 'gpt-4o', counts: { vip: 5, svip: 10 } },
      { model: 'claude', counts: { vip: 5 } },
    ])
  })

  test('expands group-scoped quota rules into the group table', () => {
    const plan = {
      ...basePlan,
      kind: 'time',
      scope: 'group',
      measure: 'quota',
      expire_mode: 'time',
      upgrade_groups: ['vip', 'svip'],
      request_rules: [
        { group: 'vip', amount_display: 10 },
        { group: 'svip', amount_display: 25 },
      ],
    } as unknown as SubscriptionPlan
    const values = planToFormValues(plan)
    assert.equal(values.scope, 'group')
    assert.deepEqual(values.group_values, { vip: 10, svip: 25 })
    assert.deepEqual(values.request_rows, [])
  })
})

describe('requestRowsToRules / groupValuesToRules', () => {
  test('merges models sharing a count into one rule per group', () => {
    const rows: RequestMatrixRow[] = [
      { model: 'gpt-4o', counts: { vip: 5, svip: 10 } },
      { model: 'claude', counts: { vip: 5 } },
    ]
    assert.deepEqual(requestRowsToRules(rows, ['vip', 'svip'], 'count'), [
      { group: 'vip', models: ['gpt-4o', 'claude'], count: 5, amount_display: 0 },
      { group: 'svip', models: ['gpt-4o'], count: 10, amount_display: 0 },
    ])
  })

  test('quota measure emits amounts instead of counts', () => {
    const rows: RequestMatrixRow[] = [{ model: 'gpt-4o', counts: { vip: 2.5 } }]
    assert.deepEqual(requestRowsToRules(rows, ['vip'], 'quota'), [
      { group: 'vip', models: ['gpt-4o'], count: 0, amount_display: 2.5 },
    ])
  })

  test('drops values for groups the plan no longer upgrades', () => {
    const rows: RequestMatrixRow[] = [
      { model: 'gpt-4o', counts: { vip: 5, stale: 99 } },
    ]
    assert.deepEqual(requestRowsToRules(rows, ['vip'], 'count'), [
      { group: 'vip', models: ['gpt-4o'], count: 5, amount_display: 0 },
    ])
  })

  test('group table emits one rule per positive group', () => {
    assert.deepEqual(
      groupValuesToRules({ vip: 10, svip: 0, stale: 5 }, ['vip', 'svip'], 'quota'),
      [{ group: 'vip', models: [], count: 0, amount_display: 10 }]
    )
  })
})

describe('planGroupValues', () => {
  test('reads group-scoped rules into the table', () => {
    const plan = {
      ...basePlan,
      scope: 'group',
      measure: 'count',
      request_rules: [
        { group: 'vip', count: 7 },
        { group: 'svip', count: 3 },
      ],
    } as unknown as SubscriptionPlan
    assert.deepEqual(planGroupValues(plan), { vip: 7, svip: 3 })
  })
})

describe('formValuesToPlanPayload', () => {
  const base: PlanFormValues = {
    ...PLAN_FORM_DEFAULTS,
    title: 'plan',
  }

  test('shared pool keeps the reset period and no rules', () => {
    const payload = formValuesToPlanPayload({
      ...base,
      quota_reset_period: 'custom',
      quota_reset_custom_seconds: 90,
      total_amount: 12,
      upgrade_groups: ['vip'],
    })
    assert.equal(payload.plan.quota_reset_period, 'custom')
    assert.equal(payload.plan.quota_reset_custom_seconds, 90)
    assert.equal(payload.plan.total_amount_display, 12)
    assert.deepEqual(payload.plan.request_rules, [])
    assert.equal(payload.plan.scope, 'all')
    assert.equal(payload.plan.measure, 'quota')
    assert.equal('request_rows' in payload.plan, false)
    assert.equal('group_values' in payload.plan, false)
  })

  test('scoped plans drop the shared pool and force reset to never', () => {
    const payload = formValuesToPlanPayload({
      ...base,
      expire_mode: 'quota',
      scope: 'group',
      measure: 'quota',
      total_amount: 99,
      quota_reset_period: 'daily',
      upgrade_groups: ['vip'],
      group_values: { vip: 10 },
    })
    assert.equal(payload.plan.total_amount_display, 0)
    assert.equal(payload.plan.quota_reset_period, 'never')
    assert.deepEqual(payload.plan.request_rules, [
      { group: 'vip', models: [], count: 0, amount_display: 10 },
    ])
  })

  test('model scope collapses the matrix into rules', () => {
    const payload = formValuesToPlanPayload({
      ...base,
      scope: 'model',
      measure: 'count',
      upgrade_groups: ['vip', 'svip'],
      request_rows: [
        { model: 'gpt-4o', counts: { vip: 5 } },
        { model: 'claude', counts: { svip: 7 } },
      ],
    })
    assert.deepEqual(payload.plan.request_rules, [
      { group: 'vip', models: ['gpt-4o'], count: 5, amount_display: 0 },
      { group: 'svip', models: ['claude'], count: 7, amount_display: 0 },
    ])
  })
})

describe('getPlanFormSchema validation', () => {
  const schema = getPlanFormSchema(t)
  const base: PlanFormValues = {
    ...PLAN_FORM_DEFAULTS,
    title: 'plan',
  }

  test('shared quota with quota expiry needs a positive pool', () => {
    const bad = schema.safeParse({
      ...base,
      expire_mode: 'quota',
      scope: 'all',
      total_amount: 0,
    })
    assert.equal(bad.success, false)
  })

  test('shared pool with time expiry allows zero (unlimited)', () => {
    const ok = schema.safeParse({
      ...base,
      expire_mode: 'time',
      scope: 'all',
      total_amount: 0,
    })
    assert.equal(ok.success, true)
  })

  test('group scope needs at least one positive group', () => {
    const empty = schema.safeParse({
      ...base,
      scope: 'group',
      upgrade_groups: ['vip'],
      group_values: {},
    })
    assert.equal(empty.success, false)
    const zero = schema.safeParse({
      ...base,
      scope: 'group',
      upgrade_groups: ['vip'],
      group_values: { vip: 0 },
    })
    assert.equal(zero.success, false)
    const ok = schema.safeParse({
      ...base,
      scope: 'group',
      upgrade_groups: ['vip'],
      group_values: { vip: 5 },
    })
    assert.equal(ok.success, true)
  })

  test('model scope needs a filled matrix without duplicate rows', () => {
    const empty = schema.safeParse({
      ...base,
      scope: 'model',
      upgrade_groups: ['vip'],
      request_rows: [],
    })
    assert.equal(empty.success, false)
    const ok = schema.safeParse({
      ...base,
      scope: 'model',
      upgrade_groups: ['vip'],
      request_rows: [{ model: 'gpt-4o', counts: { vip: 3 } }],
    })
    assert.equal(ok.success, true)
    const dup = schema.safeParse({
      ...base,
      scope: 'model',
      upgrade_groups: ['vip'],
      request_rows: [
        { model: 'gpt-4o', counts: { vip: 3 } },
        { model: 'gpt-4o', counts: { vip: 4 } },
      ],
    })
    assert.equal(dup.success, false)
  })

  test('orphaned group values in the matrix are flagged', () => {
    const result = schema.safeParse({
      ...base,
      scope: 'model',
      upgrade_groups: ['vip'],
      request_rows: [{ model: 'gpt-4o', counts: { removed: 3 } }],
    })
    assert.equal(result.success, false)
    if (!result.success) {
      const message = result.error.issues.map((issue) => issue.message).join('; ')
      assert.ok(
        message.includes('gpt-4o (removed)'),
        `expected the orphaned-group warning, got: ${message}`
      )
    }
  })
})

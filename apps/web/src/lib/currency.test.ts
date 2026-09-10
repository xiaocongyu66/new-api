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
import assert from 'node:assert/strict'
import { afterEach, describe, test } from 'node:test'

import {
  DEFAULT_CURRENCY_CONFIG,
  type CurrencyConfig,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import {
  formatPaymentAmount,
  getAmountSymbol,
  getAmountUnit,
} from './currency'

/**
 * 支付金额单位 (amountUnit: usd/cny/custom) names PAYMENT amounts
 * (充值/兑换/支付) and is intentionally independent of the 额度
 * consumption-currency display. These tests guard the single source the UI
 * reads: getAmountUnit() normalizes stored values, getAmountSymbol() resolves
 * the prefix ('$'/'¥'/金额名称), and formatPaymentAmount() renders with it.
 */
function setCurrency(overrides: Partial<typeof DEFAULT_CURRENCY_CONFIG>) {
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG, ...overrides },
  })
}

describe('payment amount unit (支付金额单位)', () => {
  // The config store is a process-wide singleton shared by every test file in
  // the same `bun test` run; reset it to defaults after each test so this file
  // leaves no state that can affect later suites.
  afterEach(() => {
    setCurrency({})
  })

  test('custom unit trims the configured name for the symbol', () => {
    setCurrency({ amountUnit: 'custom', amountName: '  稀有气体 ' })
    assert.equal(getAmountUnit(), 'custom')
    assert.equal(getAmountSymbol(), '稀有气体')
  })

  test('formatPaymentAmount uses the 金额名称 as the unit in custom mode', () => {
    setCurrency({ amountUnit: 'custom', amountName: '稀有气体' })
    const out = formatPaymentAmount(50)
    assert.ok(out.startsWith('稀有气体'))
    assert.ok(out.includes('50'))
  })

  test('getAmountUnit normalizes empty/unknown stored values to usd', () => {
    setCurrency({ amountUnit: undefined })
    assert.equal(getAmountUnit(), 'usd')
    setCurrency({ amountUnit: 'bogus' as CurrencyConfig['amountUnit'] })
    assert.equal(getAmountUnit(), 'usd')
  })

  test('usd/cny units render currency symbols without a custom name', () => {
    setCurrency({ amountUnit: 'usd', amountName: '' })
    assert.equal(getAmountSymbol(), '$')
    assert.ok(formatPaymentAmount(9.9).startsWith('$'))

    setCurrency({ amountUnit: 'cny', amountName: '' })
    assert.equal(getAmountSymbol(), '¥')
    assert.ok(formatPaymentAmount(9.9).startsWith('¥'))
  })

  test('custom unit falls back to $ when the name is blank', () => {
    setCurrency({ amountUnit: 'custom', amountName: '🐸' })
    assert.equal(getAmountSymbol(), '🐸')
    assert.ok(formatPaymentAmount(9.9).includes('🐸'))

    setCurrency({ amountUnit: 'custom', amountName: '' })
    assert.equal(getAmountSymbol(), '$')
  })

  test('formatPaymentAmount ignores the 额度 display mode for payment units', () => {
    // Even in CUSTOM 额度 display with a 🍄 symbol, a USD payment unit stays $.
    setCurrency({
      quotaDisplayType: 'CUSTOM',
      customCurrencySymbol: '🍄',
      amountUnit: 'usd',
      amountName: '',
    })
    assert.ok(formatPaymentAmount(50).startsWith('$'))
    assert.ok(!formatPaymentAmount(50).includes('🍄'))
  })
})

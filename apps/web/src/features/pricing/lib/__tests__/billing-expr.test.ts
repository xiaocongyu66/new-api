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

import { parseTiersFromExpr } from '../billing-expr'
import { getDynamicPriceEntries, getTierBoundLabel } from '../dynamic-price'
import { evalExprLocally, type ExtraTokenValues } from '../tier-expr'

const emptyExtraTokens: ExtraTokenValues = {
  cacheReadTokens: 0,
  cacheCreateTokens: 0,
  cacheCreate1hTokens: 0,
  imageTokens: 0,
  imageOutputTokens: 0,
  audioInputTokens: 0,
  audioOutputTokens: 0,
}

describe('evalExprLocally fixed prices', () => {
  test('evaluates a fixed-price context ladder and records the matched tier', () => {
    const result = evalExprLocally(
      'len <= 100000 ? tier("100K", fixed(0.21)) : tier("default", fixed(0.5376))',
      50000,
      0,
      emptyExtraTokens
    )

    assert.equal(result.error, null)
    assert.equal(result.cost, 210000)
    assert.equal(result.matchedTier, '100K')
  })

  test('adds a fixed per-request fee to token pricing', () => {
    const result = evalExprLocally(
      'tier("base", fixed(0.01) + p * 2 + c * 8)',
      1000,
      100,
      emptyExtraTokens
    )

    assert.equal(result.error, null)
    assert.equal(result.cost, 12800)
    assert.equal(result.matchedTier, 'base')
  })
})

function prices(expr: string) {
  return parseTiersFromExpr(expr).map((tier) => [
    tier.label,
    tier.inputPrice,
    tier.outputPrice,
    tier.cacheReadPrice,
  ])
}

describe('parseTiersFromExpr', () => {
  // Both forms are valid for the backend engine; the parser used to stop at
  // the first `)` and only accept `var * N`, so these tiers read as $0.
  test.each([
    ['parenthesised variable', 'tier("t1", (p)*2 + c*8 + (cr)*0.2)'],
    ['coefficient first', 'tier("t1", 2 * p + 8 * c + 0.2 * cr)'],
  ])('reads prices with a %s', (_name, expr) => {
    assert.deepEqual(prices(expr), [['t1', 2, 8, 0.2]])
  })

  test('keeps tier boundaries and order across a context ladder', () => {
    const tiers = parseTiersFromExpr(
      'len <= 10000 ? tier("t1", (p)*2 + c*8) : len <= 100000 ? tier("t2", 3 * p + 12 * c) : tier("t3", p * 6 + c * 24)'
    )
    assert.deepEqual(
      tiers.map((tier) => [tier.label, tier.inputPrice, tier.outputPrice]),
      [
        ['t1', 2, 8],
        ['t2', 3, 12],
        ['t3', 6, 24],
      ]
    )
    assert.deepEqual(
      tiers.map((_, index) => getTierBoundLabel(tiers, index)),
      ['≤10K', '≤100K', '>100K']
    )
  })
})

describe('per-request tier prices', () => {
  // Production ladders price each context tier as a flat fee per request.
  const ladder =
    'len <= 10000 ? tier("t1", fixed(0.055))\n: len <= 100000 ? tier("t2", fixed(0.11))\n: tier("t3", fixed(0.42))'

  test('reads fixed() as the tier price, not as token prices', () => {
    assert.deepEqual(
      parseTiersFromExpr(ladder).map((tier) => [
        tier.label,
        tier.fixedPrice,
        tier.inputPrice,
        tier.outputPrice,
      ]),
      [
        ['t1', 0.055, 0, 0],
        ['t2', 0.11, 0, 0],
        ['t3', 0.42, 0, 0],
      ]
    )
  })

  test('shows the amount per request with the group ratio applied', () => {
    const [tier] = parseTiersFromExpr(ladder)
    const entries = getDynamicPriceEntries(tier, {
      tokenUnit: 'M',
      groupRatioMultiplier: 0.5,
    })
    assert.deepEqual(
      entries.map((entry) => [entry.field, entry.value, entry.formatted]),
      [['fixedPrice', 0.055, '$0.0275']]
    )
  })
})

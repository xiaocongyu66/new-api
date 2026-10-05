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
import { describe, test } from 'node:test'

import { mapStatusDataToConfig } from './use-system-config'

// /api/status is the only place the operator's payment currency label enters
// the frontend, so this mapping is the seam that decides whether an admin's
// chosen name survives. A past release severed "custom" from the backend enum
// and coerced it to "usd" in both directions, which silently renamed every
// payment amount; these tests pin the round trip so that cannot come back.
describe('payment amount unit from /api/status', () => {
  test('keeps the custom unit together with the configured name', () => {
    const config = mapStatusDataToConfig({
      amount_unit: 'custom',
      amount_name: '稀有气体',
    })

    assert.equal(config.currency?.amountUnit, 'custom')
    assert.equal(config.currency?.amountName, '稀有气体')
  })

  test('trims the name so a padded option cannot break the prefix', () => {
    const config = mapStatusDataToConfig({
      amount_unit: 'custom',
      amount_name: '  稀有气体  ',
    })

    assert.equal(config.currency?.amountName, '稀有气体')
  })

  test('passes usd and cny through unchanged', () => {
    assert.equal(
      mapStatusDataToConfig({ amount_unit: 'usd' }).currency?.amountUnit,
      'usd'
    )
    assert.equal(
      mapStatusDataToConfig({ amount_unit: 'cny' }).currency?.amountUnit,
      'cny'
    )
  })

  test('falls back to usd only for missing or unrecognized units', () => {
    // A blank historical option and a garbage value are the cases that must
    // normalize; "custom" deliberately must not land here.
    assert.equal(mapStatusDataToConfig({}).currency?.amountUnit, 'usd')
    assert.equal(
      mapStatusDataToConfig({ amount_unit: '' }).currency?.amountUnit,
      'usd'
    )
    assert.equal(
      mapStatusDataToConfig({ amount_unit: 'bogus' }).currency?.amountUnit,
      'usd'
    )
  })

  test('a missing name yields an empty label rather than undefined', () => {
    const config = mapStatusDataToConfig({ amount_unit: 'custom' })

    assert.equal(config.currency?.amountName, '')
  })
})

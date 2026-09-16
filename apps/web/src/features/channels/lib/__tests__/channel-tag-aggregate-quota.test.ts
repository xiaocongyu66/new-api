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

import type { Channel } from '../../types'
import { aggregateChannelsByTag, isTagAggregateRow } from '../channel-utils'

function channel(id: number, used: number, usedDisplay: number): Channel {
  return {
    id,
    tag: 'group-a',
    used_quota: used,
    used_quota_display: usedDisplay,
  } as Channel
}

describe('tag aggregate row used quota', () => {
  test('sums used_quota_display across children instead of inheriting the first', () => {
    // The tag row is built by spreading the first child. Before the fix that
    // left the first child's used_quota_display in place while used_quota was
    // summed, so the group header showed one channel's usage as the total.
    const rows = aggregateChannelsByTag([
      channel(1, 1_000_000, 2),
      channel(2, 1_500_000, 3),
      channel(3, 2_000_000, 4),
    ])

    assert.equal(rows.length, 1)
    assert.ok(isTagAggregateRow(rows[0]))
    const tagRow = rows[0] as (typeof rows)[0] & {
      used_quota: number
      used_quota_display: number
    }
    assert.equal(tagRow.used_quota, 4_500_000)
    assert.equal(
      tagRow.used_quota_display,
      9,
      'used_quota_display must be the sum of children, not the first child'
    )
  })

  test('handles channels whose used_quota_display is absent', () => {
    const rows = aggregateChannelsByTag([
      channel(1, 1_000_000, 2),
      { ...channel(2, 1_000_000, 0), used_quota_display: undefined },
    ])

    const tagRow = rows[0] as (typeof rows)[0] & {
      used_quota_display: number
    }
    assert.equal(tagRow.used_quota_display, 2)
  })
})

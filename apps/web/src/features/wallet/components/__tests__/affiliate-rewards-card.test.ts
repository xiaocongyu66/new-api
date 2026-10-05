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
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { buildReferralRewardLine } from '../affiliate-rewards-card'

// formatQuota renders through the site currency config. Pin TOKENS (the
// identity case, no symbol) so assertions cover the reward-line *composition*
// — which currency parts appear — and not currency.ts's symbol formatting,
// which has its own coverage. Restore afterwards: the store is process-global.
function setTokensDisplay() {
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG, quotaDisplayType: 'TOKENS' },
  })
}

describe('referral reward line', () => {
  afterEach(() => {
    useSystemConfigStore.getState().setConfig({
      currency: { ...DEFAULT_CURRENCY_CONFIG },
    })
  })

  test('quotes both rewards in balance', () => {
    setTokensDisplay()
    const line = buildReferralRewardLine({
      inviterRewardDisplay: 20,
      inviteeRewardDisplay: 10,
    })
    assert.ok(line)
    assert.equal(line!.inviter, '20')
    assert.equal(line!.invitee, '10')
  })

  test('leaves the inviter slot empty when the inviter reward is zero', () => {
    setTokensDisplay()
    const line = buildReferralRewardLine({
      inviterRewardDisplay: 0,
      inviteeRewardDisplay: 10,
    })
    assert.ok(line)
    assert.equal(line!.inviter, '')
    assert.equal(line!.invitee, '10')
  })

  test('leaves the invitee slot empty when the invitee reward is zero', () => {
    setTokensDisplay()
    const line = buildReferralRewardLine({
      inviterRewardDisplay: 20,
      inviteeRewardDisplay: 0,
    })
    assert.ok(line)
    assert.equal(line!.inviter, '20')
    assert.equal(line!.invitee, '')
  })

  test('returns null when neither reward is configured', () => {
    assert.equal(
      buildReferralRewardLine({
        inviterRewardDisplay: 0,
        inviteeRewardDisplay: 0,
      }),
      null
    )
  })

  test('treats missing rewards as zero', () => {
    assert.equal(buildReferralRewardLine({}), null)
  })
})

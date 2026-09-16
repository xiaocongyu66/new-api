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

  test('quotes both currencies when the admin pays quota + spore', () => {
    setTokensDisplay()
    // Live config at the time of the fix: QuotaForInviter 20 (display),
    // SporeInviterReward 0.1, currency 'both'. The card must show both numbers,
    // mirroring the Quota Settings page, not just one of them.
    const line = buildReferralRewardLine({
      inviterRewardDisplay: 20,
      inviteeRewardDisplay: 10,
      sporeInviterReward: 0.1,
      inviterRewardCurrency: 'both',
      sporeUnit: '🍄',
    })
    assert.ok(line)
    assert.equal(line!.inviter, '20 + 🍄 0.1')
    assert.equal(line!.invitee, '10')
  })

  test('shows only spore when the inviter reward currency is spore', () => {
    setTokensDisplay()
    const line = buildReferralRewardLine({
      inviterRewardDisplay: 20,
      inviteeRewardDisplay: 10,
      sporeInviterReward: 0.1,
      inviterRewardCurrency: 'spore',
      sporeUnit: '🍄',
    })
    assert.ok(line)
    assert.equal(line!.inviter, '🍄 0.1')
    assert.equal(line!.invitee, '10')
  })

  test('shows only quota when the inviter reward currency is quota', () => {
    setTokensDisplay()
    const line = buildReferralRewardLine({
      inviterRewardDisplay: 20,
      inviteeRewardDisplay: 10,
      sporeInviterReward: 0.1,
      inviterRewardCurrency: 'quota',
      sporeUnit: '🍄',
    })
    assert.ok(line)
    assert.equal(line!.inviter, '20')
    assert.equal(line!.invitee, '10')
  })

  test('returns null when neither reward is configured', () => {
    assert.equal(
      buildReferralRewardLine({
        inviterRewardDisplay: 0,
        inviteeRewardDisplay: 0,
        sporeInviterReward: 0,
        inviterRewardCurrency: 'quota',
        sporeUnit: '🍄',
      }),
      null
    )
  })

  test('drops the spore part when it is zero in both mode', () => {
    setTokensDisplay()
    const line = buildReferralRewardLine({
      inviterRewardDisplay: 20,
      inviteeRewardDisplay: 0,
      sporeInviterReward: 0,
      inviterRewardCurrency: 'both',
      sporeUnit: '🍄',
    })
    assert.ok(line)
    assert.equal(line!.inviter, '20')
    assert.equal(line!.invitee, '')
  })
})

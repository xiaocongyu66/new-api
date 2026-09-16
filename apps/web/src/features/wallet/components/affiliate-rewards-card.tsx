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
import { Share2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { IconBadge } from '@/components/ui/icon-badge'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { getCurrencyDisplay } from '@/lib/currency'
import { formatQuota } from '@/lib/format'
import {
  SPORE_UNITS_PER_SPORE,
  formatSpore,
  getSporeName,
  getSporeSymbol,
} from '@/lib/spore'
import { useSystemConfigStore } from '@/stores/system-config-store'

import type { UserWalletData } from '../types'

interface AffiliateRewardsCardProps {
  user: UserWalletData | null
  affiliateLink: string
  onTransfer: () => void
  complianceConfirmed?: boolean
  loading?: boolean
}

/**
 * Build the "inviter X, invitee Y" reward line shown on the referral card.
 * The inviter reward is paid in the admin-configured currency (quota, spore, or
 * both); the invitee reward is quota only. Returns null when no reward is
 * configured, so the card falls back to the generic description.
 */
export function buildReferralRewardLine(args: {
  inviterRewardDisplay?: number
  inviteeRewardDisplay?: number
  sporeInviterReward?: number
  inviterRewardCurrency?: 'quota' | 'spore' | 'both'
  sporeUnit: string
}): { inviter: string; invitee: string } | null {
  const {
    inviterRewardDisplay,
    inviteeRewardDisplay,
    sporeInviterReward,
    inviterRewardCurrency,
    sporeUnit,
  } = args
  const paysSpore = inviterRewardCurrency === 'spore'
  const paysBoth = inviterRewardCurrency === 'both'

  const inviterSporeUnits = (sporeInviterReward ?? 0) * SPORE_UNITS_PER_SPORE
  const inviterParts: string[] = []
  if (!paysSpore && (inviterRewardDisplay ?? 0) > 0) {
    inviterParts.push(formatQuota(inviterRewardDisplay ?? 0))
  }
  if ((paysSpore || paysBoth) && inviterSporeUnits > 0) {
    inviterParts.push(`${sporeUnit} ${formatSpore(inviterSporeUnits)}`)
  }

  const inviter = inviterParts.join(' + ')
  const invitee =
    (inviteeRewardDisplay ?? 0) > 0
      ? formatQuota(inviteeRewardDisplay ?? 0)
      : ''

  if (!inviter && !invitee) return null
  return { inviter, invitee }
}

export function AffiliateRewardsCard({
  user,
  affiliateLink,
  onTransfer,
  complianceConfirmed = true,
  loading,
}: AffiliateRewardsCardProps) {
  const { t } = useTranslation()
  const {
    sporeInviterReward,
    inviterRewardDisplay,
    inviteeRewardDisplay,
    inviterRewardCurrency,
  } = useSystemConfigStore.getState().config.currency
  // 邀请奖励货币跟随后台设置：菌种模式显示菌种符号/菌种数额，
  // 余额模式显示余额自定义符号/额度数额，both 模式两者并列展示。
  const paysSpore = inviterRewardCurrency === 'spore'
  const paysBoth = inviterRewardCurrency === 'both'
  // 卡片徽标：符号未配置时回落到 Share2 图标。
  const { meta } = getCurrencyDisplay()
  const sporeUnit = getSporeSymbol() || getSporeName()
  const rewardSymbol = paysSpore
    ? getSporeSymbol()
    : meta.kind === 'tokens'
      ? ''
      : meta.symbol
  if (loading) {
    return (
      <Card data-card-hover='false' className='bg-muted/20 py-0'>
        <CardContent className='grid gap-4 p-3 sm:p-4 lg:grid-cols-[minmax(220px,1fr)_minmax(220px,0.72fr)_minmax(320px,1.15fr)] lg:items-center'>
          <div>
            <Skeleton className='h-5 w-32' />
            <Skeleton className='mt-2 h-4 w-48' />
          </div>
          <Skeleton className='h-14 rounded-lg' />
          <Skeleton className='h-10 rounded-lg' />
        </CardContent>
      </Card>
    )
  }

  const hasRewards = (user?.aff_quota ?? 0) > 0

  // 邀请者奖励数额：按后台货币拼装，both 模式两种并列；受邀者奖励只有额度。
  const rewardLine = buildReferralRewardLine({
    inviterRewardDisplay,
    inviteeRewardDisplay,
    sporeInviterReward,
    inviterRewardCurrency,
    sporeUnit,
  })

  // 待确认/累计：两种货币都展示。奶酪走后端换算的 _display；菌种发放即到账，
  // 没有待入账中间态，待确认展示当前余额，总收入展示后端累计的 aff_spore_history。
  const sporePending = formatSpore(user?.spore ?? 0)
  const sporeTotal = formatSpore(user?.aff_spore_history ?? 0)
  const pendingValue =
    paysSpore || paysBoth
      ? `${formatQuota(user?.aff_quota_display ?? 0)} + ${sporeUnit} ${sporePending}`
      : formatQuota(user?.aff_quota_display ?? 0)
  const totalValue =
    paysSpore || paysBoth
      ? `${formatQuota(user?.aff_history_quota_display ?? 0)} + ${sporeUnit} ${sporeTotal}`
      : formatQuota(user?.aff_history_quota_display ?? 0)

  return (
    <Card data-card-hover='false' className='bg-muted/20 py-0'>
      <CardContent className='grid gap-3 p-3 sm:gap-4 sm:p-4 lg:grid-cols-[minmax(200px,1fr)_minmax(180px,0.65fr)_minmax(280px,1fr)] lg:items-center'>
        <div className='flex min-w-0 items-center gap-2.5'>
          <IconBadge tone='chart-3'>
            {rewardSymbol ? (
              <span aria-hidden='true' className='text-base leading-none'>
                {rewardSymbol}
              </span>
            ) : (
              <Share2 />
            )}
          </IconBadge>
          <div className='min-w-0'>
            <h3 className='truncate text-sm font-semibold'>
              {t('Referral Program')}
            </h3>
            <p className='text-muted-foreground line-clamp-1 text-xs'>
              {rewardLine
                ? t('Inviter {{inviter}}, invitee {{invitee}}', {
                    inviter: rewardLine.inviter || t('None'),
                    invitee: rewardLine.invitee || t('None'),
                  })
                : t(
                    'Earn rewards when users join through your referral link. Transfer accumulated rewards to your balance anytime.'
                  )}
            </p>
          </div>
        </div>

        <div className='grid grid-cols-3 gap-1.5 text-center'>
          {[
            [t('Pending'), pendingValue],
            [t('Total Earned'), totalValue],
            [t('Invites'), String(user?.aff_count ?? 0)],
          ].map(([label, value]) => (
            <div key={label}>
              <div className='text-muted-foreground truncate text-[10px] font-medium tracking-wider uppercase'>
                {label}
              </div>
              <div className='mt-0.5 truncate text-sm font-semibold tabular-nums'>
                {value}
              </div>
            </div>
          ))}
        </div>

        <div className='flex items-center gap-2'>
          <Input
            value={affiliateLink}
            readOnly
            className='border-muted bg-background/70 h-9 min-w-0 flex-1 font-mono text-xs'
          />
          <CopyButton
            value={affiliateLink}
            variant='outline'
            className='bg-background size-9 shrink-0'
            iconClassName='size-4'
            tooltip={t('Copy referral link')}
            aria-label={t('Copy referral link')}
          />
          {hasRewards && (
            <Button
              onClick={onTransfer}
              disabled={!complianceConfirmed}
              className='h-9 shrink-0 px-3'
              size='sm'
            >
              {t('Transfer to Balance')}
            </Button>
          )}
        </div>
        {!complianceConfirmed ? (
          <p className='text-muted-foreground text-xs lg:col-span-3'>
            {t(
              'Referral reward transfer is disabled until the administrator confirms compliance terms.'
            )}
          </p>
        ) : null}
      </CardContent>
    </Card>
  )
}

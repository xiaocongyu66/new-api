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
import { Check, Copy, Gift, Link2, Percent, Share2, Users } from 'lucide-react'
import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { useSystemConfig } from '@/hooks/use-system-config'
import { getSelf } from '@/lib/api'
import { formatQuota } from '@/lib/format'

import { TransferDialog } from '../wallet/components/dialogs/transfer-dialog'
import { useAffiliate, useTopupInfo } from '../wallet/hooks'
import type { UserWalletData } from '../wallet/types'

import { buildReferralDescription } from './lib/description'

function highlightDescription(text: string, tokens: string[]): ReactNode[] {
  const ranges: { start: number; end: number }[] = []
  for (const token of tokens) {
    if (!token) continue
    let from = 0
    while (true) {
      const index = text.indexOf(token, from)
      if (index === -1) break
      ranges.push({ start: index, end: index + token.length })
      from = index + token.length
    }
  }
  ranges.sort((a, b) => a.start - b.start)

  const nodes: ReactNode[] = []
  let cursor = 0
  ranges.forEach((range, index) => {
    if (range.start < cursor) return
    nodes.push(text.slice(cursor, range.start))
    nodes.push(
      <span key={index} className='text-primary font-medium'>
        {text.slice(range.start, range.end)}
      </span>
    )
    cursor = range.end
  })
  nodes.push(text.slice(cursor))
  return nodes
}

export function InvitationRewards() {
  const { t } = useTranslation()
  const { copiedText, copyToClipboard } = useCopyToClipboard()
  const { currency } = useSystemConfig()
  const [user, setUser] = useState<UserWalletData | null>(null)
  const [loading, setLoading] = useState(true)
  const [transferOpen, setTransferOpen] = useState(false)
  const {
    affiliateCode,
    affiliateLink,
    loading: linkLoading,
    transferQuota,
    transferring,
  } = useAffiliate()

  const codeCopied = copiedText === affiliateCode
  const linkCopied = copiedText === affiliateLink
  const { topupInfo } = useTopupInfo()

  const rewardMode = currency.inviteRewardMode ?? 'direct'
  const rebateTrigger = currency.inviteRewardRebateTrigger ?? 'first'
  const rebateMultiplier = currency.inviteRewardRebateMultiplier ?? 0
  const minTransfer = currency.inviteRewardTransferMinDisplay ?? 0

  const multiplierPercent = `${Number((rebateMultiplier * 100).toFixed(1))}%`
  const inviterReward = currency.inviterRewardDisplay ?? 0
  const inviteeReward = currency.inviteeRewardDisplay ?? 0
  const inviterLabel = formatQuota(inviterReward)
  const inviteeLabel = formatQuota(inviteeReward)
  const transferAmount = formatQuota(minTransfer)
  const descriptionLine = buildReferralDescription(
    (key, values) => String(t(key, values)),
    {
      mode: rewardMode,
      trigger: rebateTrigger,
      multiplierPercent,
      inviterReward,
      inviteeReward,
      inviterRewardLabel: inviterLabel,
      inviteeRewardLabel: inviteeLabel,
      minTransfer,
      minTransferLabel: transferAmount,
    }
  )
  const highlightedDescription = highlightDescription(descriptionLine, [
    inviterReward > 0 ? inviterLabel : '',
    inviteeReward > 0 ? inviteeLabel : '',
    rewardMode === 'recharge' ? multiplierPercent : '',
    minTransfer > 0 ? transferAmount : '',
  ])

  const fetchUser = useCallback(async () => {
    try {
      setLoading(true)
      const response = await getSelf()
      if (response.success && response.data) {
        setUser(response.data as UserWalletData)
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to fetch user data:', error)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void fetchUser()
  }, [fetchUser])

  const handleTransfer = async (amount: number) => {
    const success = await transferQuota(amount)
    if (success) await fetchUser()
    return success
  }


  const stats = [
    {
      key: 'invites',
      label: t('Invites'),
      value: String(user?.aff_count ?? 0),
      icon: Users,
    },
    ...(rewardMode === 'recharge'
      ? [
          {
            key: 'multiplier',
            label: t('Rebate Multiplier'),
            value: multiplierPercent,
            icon: Percent,
          },
        ]
      : []),
    {
      key: 'pending',
      label: t('Pending'),
      value: formatQuota(user?.aff_quota_display ?? 0),
      icon: Gift,
    },
    {
      key: 'total',
      label: t('Total Earned'),
      value: formatQuota(user?.aff_history_quota_display ?? 0),
      icon: Share2,
    },
  ]

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>
          {t('Invitation Rewards')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <div className='mx-auto flex w-full max-w-7xl flex-col gap-4 sm:gap-5'>
            <div
              className={
                rewardMode === 'recharge'
                  ? 'grid gap-4 sm:grid-cols-4'
                  : 'grid gap-4 sm:grid-cols-3'
              }
            >
              {stats.map((stat) => {
                const Icon = stat.icon
                return (
                  <Card key={stat.key} className='bg-card-surface py-0'>
                    <CardContent className='p-4 sm:p-5'>
                      <div className='flex items-start justify-between'>
                        <span className='text-muted-foreground text-xs'>
                          {stat.label}
                        </span>
                        <Icon className='text-muted-foreground size-4' />
                      </div>
                      <div className='mt-3 flex items-end justify-between gap-2'>
                        {loading ? (
                          <Skeleton className='h-6 w-20' />
                        ) : (
                          <span className='text-xl font-semibold tabular-nums'>
                            {stat.value}
                          </span>
                        )}
                        {stat.key === 'pending' && (
                          <Button
                            type='button'
                            variant='ghost'
                            size='sm'
                            onClick={() => {
                              if ((user?.aff_quota ?? 0) <= 0) {
                                toast.info(t('No rewards available to transfer yet.'))
                                return
                              }
                              setTransferOpen(true)
                            }}
                            disabled={topupInfo?.payment_compliance_confirmed === false}
                            className='border-border/60 bg-muted/30 hover:bg-muted/50 disabled:opacity-70 h-7 shrink-0 border px-2.5 text-xs'
                          >
                            {t('Transfer')}
                          </Button>
                        )}
                      </div>
                    </CardContent>
                  </Card>
                )
              })}
            </div>

            <div className='w-full'>
              <Card className='bg-card-surface py-0'>
                <CardHeader className='border-border/60 border-b px-4 py-4 sm:px-5 sm:py-5'>
                  <div className='flex items-center gap-2.5'>
                    <Link2 className='text-muted-foreground size-4' />
                    <div>
                      <CardTitle className='text-sm font-semibold'>
                        {t('Your Referral Link')}
                      </CardTitle>
                      <CardDescription className='text-xs'>
                        {highlightedDescription}
                      </CardDescription>
                    </div>
                  </div>
                </CardHeader>
                <CardContent className='p-4 sm:p-5'>
                  <div className='flex flex-wrap items-end gap-3'>
                    <div className='w-full sm:w-40'>
                      <label className='text-muted-foreground mb-1.5 block text-xs'>
                        {t('Invitation Code')}
                      </label>
                      <Button
                        type='button'
                        variant='ghost'
                        onClick={() => copyToClipboard(affiliateCode)}
                        className='border-border/60 flex h-9 w-full items-center justify-between gap-2 px-3 font-mono text-xs select-none'
                        aria-label={t('Copy code')}
                      >
                        <span className='truncate leading-none'>{affiliateCode}</span>
                        {codeCopied ? (
                          <Check className='text-success size-4 shrink-0' />
                        ) : (
                          <Copy className='size-4 shrink-0' />
                        )}
                      </Button>
                    </div>
                    <div className='min-w-[240px] flex-1'>
                      <label className='text-muted-foreground mb-1.5 block text-xs'>
                        {t('Referral link:')}
                      </label>
                      <Button
                        type='button'
                        variant='ghost'
                        onClick={() => copyToClipboard(affiliateLink)}
                        className='bg-muted/40 border-border/60 flex h-9 w-full items-center justify-between gap-2 px-3 font-mono text-xs select-none'
                        aria-label={t('Copy referral link')}
                      >
                        <span className='truncate leading-none'>{affiliateLink}</span>
                        {linkCopied ? (
                          <Check className='text-success size-4 shrink-0' />
                        ) : (
                          <Copy className='size-4 shrink-0' />
                        )}
                      </Button>
                    </div>
                  </div>
                  {linkLoading ? <Skeleton className='mt-3 h-4 w-48' /> : null}
                </CardContent>
              </Card>

            </div>

            <Card className='bg-card-surface py-0'>
              <CardHeader className='border-border/60 border-b px-4 py-4 sm:px-5 sm:py-5'>
                <div className='flex items-center gap-2.5'>
                  <Users className='text-muted-foreground size-4' />
                  <div>
                    <CardTitle className='text-sm font-semibold'>
                      {t('Invited Users')}
                    </CardTitle>
                    <CardDescription className='text-xs'>
                      {t('Users who signed up through your referral link')}
                    </CardDescription>
                  </div>
                </div>
              </CardHeader>
              <CardContent className='p-4 sm:p-5'>
                {(user?.aff_count ?? 0) === 0 ? (
                  <div className='border-border/60 text-muted-foreground flex h-40 items-center justify-center rounded-xl border border-dashed text-sm'>
                    {t('No invited users yet')}
                  </div>
                ) : (
                  <p className='text-muted-foreground text-sm'>
                    {t('Invited users: {{count}}', {
                      count: user?.aff_count ?? 0,
                    })}
                  </p>
                )}
              </CardContent>
            </Card>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <TransferDialog
        open={transferOpen}
        onOpenChange={setTransferOpen}
        onConfirm={handleTransfer}
        availableQuota={user?.aff_quota_display ?? 0}
        transferring={transferring}
      />
    </>
  )
}

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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { formatCurrencyFromUSD } from '@/lib/currency'
import { cn } from '@/lib/utils'

import type {
  PricingAnalysisChannel,
  PricingAnalysisRow,
} from '@/features/system-settings/types'

type Props = {
  row: PricingAnalysisRow | null
  groupRatios: Record<string, number>
  onOpenChange: (open: boolean) => void
}

const HEAD =
  'bg-muted/40 text-muted-foreground border-b px-1 py-1.5 text-left text-[9px] font-medium'
const HEAD_NUM = cn(HEAD, 'text-right')
const CELL =
  'border-b px-1 py-1 align-middle text-[9px]'
const CELL_NUM = cn(CELL, 'text-right tabular-nums')
// Summary 6-cell grid only: vertical column separators, scoped so the
// channel detail tables stay without them.
const SUM_CELL =
  'border-b border-r last:border-r-0 px-1 py-1 align-middle text-[9px]'

function formatInteger(value: number): string {
  return value.toLocaleString()
}

// 10,303,721 -> 10.3M; at most 2 decimals, 3 significant digits.
function formatTokens(value: number): string {
  const units = [
    { v: 1e12, s: 'T' },
    { v: 1e9, s: 'B' },
    { v: 1e6, s: 'M' },
    { v: 1e3, s: 'K' },
  ]
  for (const u of units) {
    if (value >= u.v) {
      const n = value / u.v
      return `${Number(n.toFixed(n < 10 ? 2 : 1))}${u.s}`
    }
  }
  return String(value)
}

// Analysis amounts are USD-scale (quota / QuotaPerUnit); the site's display
// currency and exchange rate decide the symbol and the multiplier.
function formatMoney(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—'
  return formatCurrencyFromUSD(value, { digitsLarge: 2, digitsSmall: 4 })
}

function formatPercent(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—'
  return `${value.toFixed(2)}%`
}

// Context windows are token counts; render compact K/M units. Sizes that are
// exact multiples of 1024 use that base (8,192 -> 8K, 131,072 -> 128K), while
// decimal round numbers use 1000 (200,000 -> 200K).
function formatContext(value: number): string {
  const binary = value % 1024 === 0
  const unit = binary ? 1024 : 1000
  if (value >= 1_000_000) {
    const m = value / (binary ? 1_048_576 : 1_000_000)
    return `${Number.isInteger(m) ? m : Number(m.toFixed(1))}M`
  }
  if (value >= 1_000) {
    const k = value / unit
    return `${Number.isInteger(k) ? k : Number(k.toFixed(1))}K`
  }
  return String(value)
}

function valueTone(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return ''
  if (value > 0) return 'text-emerald-600 dark:text-emerald-400'
  if (value < 0) return 'text-red-600 dark:text-red-400'
  return ''
}
// Editor modes and raw upstream price_mode values must stay distinguishable in the table header.
function modeLabel(mode: string, t: (key: string) => string): string {
  switch (mode) {
    case 'per-token':
    case 'per_token':
    case 'ratio':
      return t('Per token')
    case 'per-request':
    case 'per_call':
      return t('Per request')
    case 'tiered_expr':
      return t('Tiered expression')
    case 'context_tier':
      return t('Context tiers (per token)')
    case 'context_tier_per_call':
      return t('Context tiers (per request)')
    case 'per_call_tiered':
      return t('Context tiers (per request)')
    case 'per_token_tiered':
      return t('Context tiers (per token)')
    default:
      return mode || '—'
  }
}

// Tiered billing covers two very different charges; the raw billing_mode
// ("tiered_expr") alone hides which one a channel uses.
function billingLabel(channel: PricingAnalysisChannel, t: (key: string) => string): string {
  if (channel.price_mode === 'per_call_tiered') return t('Tiered (per request)')
  if (channel.price_mode === 'per_token_tiered') return t('Tiered (per token)')
  return modeLabel(channel.billing_mode || channel.price_mode, t)
}

// A channel with neither price signal has no snapshot of its own: its cost is
// unknown, not zero.
function hasUpstreamPrice(channel: PricingAnalysisChannel): boolean {
  return channel.price_mode !== '' || channel.billing_mode !== ''
}

// Token-weighted mean group ratio across the channel's consume logs; the
// money columns already carry it, this explains the effective local price.
function formatGroupRatio(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—'
  return `×${value.toFixed(2)}`
}

// Revenue cell with the subscription-paid share as a sub-line: the total is
// the quota charged, the sub-line how much of it drew on subscriptions.
function RevenueCell({
  revenue,
  subscription,
}: {
  revenue: number | null | undefined
  subscription: number | null | undefined
}) {
  const { t } = useTranslation()
  return (
    <td className={CELL_NUM}>
      {formatMoney(revenue)}
      {subscription != null && subscription > 0 && (
        <div className='text-muted-foreground text-[8px] whitespace-nowrap'>
          {t('Subscription')} {formatMoney(subscription)}
        </div>
      )}
    </td>
  )
}

function ChannelTable({
  channel,
  groupRatios,
  t,
}: {
  channel: PricingAnalysisChannel
  groupRatios: Record<string, number>
  t: (key: string) => string
}) {
  const priced = hasUpstreamPrice(channel)
  const groups = channel.groups?.length ? channel.groups : []
  const [activeGroup, setActiveGroup] = useState(groups[0] ?? '')
  // The site charges base price × group ratio, so the local side of every
  // comparison must be scaled by the serving group's ratio.
  const groupRatio = activeGroup ? (groupRatios[activeGroup] ?? 1) : 1
  const localPrice = (value: number | null | undefined) =>
    value == null ? null : value * groupRatio
  const groupMargin = (local: number | null | undefined, upstream: number | null | undefined) => {
    const eff = localPrice(local)
    if (eff == null || upstream == null || eff <= 0) return null
    return ((eff - upstream) / eff) * 100
  }
  const hasContextTiers =
    channel.price_mode === 'per_token_tiered' ||
    channel.price_mode === 'per_call_tiered'
  const canonicalWindow =
    channel.windows.find((w) => w.context === 131072) ?? channel.windows[0]

  return (
    <div className='overflow-hidden rounded-[6px] border-x border-t'>
      <div className='bg-muted/30 flex flex-wrap items-center justify-between gap-2 border-b px-2 py-1.5'>
        <div className='flex min-w-0 flex-wrap items-center gap-2'>
          <span className='truncate text-sm font-semibold'>{channel.channel_name}</span>
          <span className='text-muted-foreground text-[11px]'>{channel.model_name}</span>
        </div>
        {priced ? (
          <span className='flex shrink-0 items-center gap-1.5 text-[10px]'>
            <span className='text-muted-foreground'>
              {billingLabel(channel, t)}
            </span>
            {channel.price_source === 'manual' && (
              <span className='bg-muted text-muted-foreground rounded px-1.5 py-0.5 font-medium'>
                {t('Manual price')}
              </span>
            )}
          </span>
        ) : (
          <span className='shrink-0 rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium text-amber-600 dark:text-amber-400'>
            {t('No upstream price')}
          </span>
        )}
      </div>

      {priced && (groups.length > 0 || (channel.upstream_groups?.length ?? 0) > 0) && (
        <div className='flex flex-wrap items-center gap-2 border-b px-2 py-1 text-[10px]'>
          {groups.length > 0 && (
            <div className='flex flex-wrap items-center gap-1'>
              <span className='text-muted-foreground'>{t('Site group')}</span>
              {groups.map((group) => (
                <button
                  key={group}
                  type='button'
                  onClick={() => setActiveGroup(group)}
                  className={cn(
                    'rounded px-1.5 py-0.5 tabular-nums',
                    group === activeGroup
                      ? 'bg-amber-500/20 text-amber-700 dark:text-amber-300'
                      : 'bg-muted text-muted-foreground hover:bg-muted/70'
                  )}
                >
                  {group} ×{(groupRatios[group] ?? 1).toFixed(2)}
                </button>
              ))}
            </div>
          )}
          {(channel.upstream_groups?.length ?? 0) > 0 && (
            <span className='text-muted-foreground ml-auto'>
              {t('Upstream group')} {channel.upstream_groups?.join(' / ')} ×
              {channel.upstream_group_ratio.toFixed(2)}
              {channel.upstream_group_ambiguous ? ` (${t('highest applied')})` : ''}
            </span>
          )}
          {!channel.upstream_cost_comparable && (
            <span className='text-amber-600 dark:text-amber-400'>
              {t('Upstream prices in tokens: cost not comparable')}
            </span>
          )}
        </div>
      )}

      <table className='w-full table-fixed border-collapse text-[10px]'>
        <colgroup>
          <col className='w-[16%]' />
          <col className='w-[11%]' />
          <col className='w-[11%]' />
          <col className='w-[9%]' />
          <col className='w-[9%]' />
          <col className='w-[16%]' />
          <col className='w-[9%]' />
          <col className='w-[10%]' />
          <col className='w-[9%]' />
        </colgroup>
        <thead>
          <tr>
            <th className={HEAD}>{priced ? t('Billing') : t('Context')}</th>
            <th className={HEAD_NUM}>{t('Local')}</th>
            <th className={HEAD_NUM}>{t('Upstream')}</th>
            <th className={HEAD_NUM}>{t('Group ratio')}</th>
            <th className={HEAD_NUM}>{t('Req')}</th>
            <th className={HEAD_NUM}>{t('Revenue')}</th>
            <th className={HEAD_NUM}>{t('Cost')}</th>
            <th className={HEAD_NUM}>{t('Profit')}</th>
            <th className={HEAD_NUM}>{t('Margin %')}</th>
          </tr>
        </thead>
        <tbody>
          {priced && hasContextTiers
            ? channel.windows.map((w) => (
                <tr key={w.context}>
                  <td className={CELL}>
                    {t('Context')} {formatContext(w.context)}
                  </td>
                  <td className={CELL_NUM}>{formatMoney(localPrice(w.local))}</td>
                  <td className={CELL_NUM}>{formatMoney(w.upstream)}</td>
                  <td className={CELL_NUM}>{`×${groupRatio.toFixed(2)}`}</td>
                  <td className={CELL_NUM}>
                    {w.requests == null ? '—' : formatInteger(w.requests)}
                  </td>
                  <RevenueCell
                    revenue={w.revenue}
                    subscription={w.subscription_revenue}
                  />
                  <td className={CELL_NUM}>{formatMoney(w.cost)}</td>
                  <td className={cn(CELL_NUM, valueTone(w.profit))}>
                    {formatMoney(w.profit)}
                  </td>
                  <td className={cn(CELL_NUM, valueTone(w.margin_pct))}>
                    {formatPercent(
                      w.usage_margin_pct ?? groupMargin(w.local, w.upstream) ?? w.margin_pct
                    )}
                  </td>
                </tr>
              ))
            : priced && (
                <tr>
                  <td className={CELL}>{billingLabel(channel, t)}</td>
                  <td className={CELL_NUM}>{formatMoney(localPrice(canonicalWindow?.local))}</td>
                  <td className={CELL_NUM}>{formatMoney(canonicalWindow?.upstream)}</td>
                  <td className={CELL_NUM}>{`×${groupRatio.toFixed(2)}`}</td>
                  <td className={CELL_NUM}>—</td>
                  <td className={CELL_NUM}>—</td>
                  <td className={CELL_NUM}>—</td>
                  <td className={CELL_NUM}>—</td>
                  <td className={CELL_NUM}>—</td>
                </tr>
              )}
          <tr className='bg-muted/30 font-medium'>
            <td className={CELL}>{t('Total')}</td>
            <td className={CELL_NUM}>—</td>
            <td className={CELL_NUM}>—</td>
            <td className={CELL_NUM}>{formatGroupRatio(channel.group_ratio)}</td>
            <td className={CELL_NUM}>{formatInteger(channel.requests)}</td>
            <RevenueCell
              revenue={channel.revenue}
              subscription={channel.subscription_revenue}
            />
            <td className={CELL_NUM}>{formatMoney(channel.cost)}</td>
            <td className={cn(CELL_NUM, valueTone(channel.profit))}>
              {formatMoney(channel.profit)}
            </td>
            <td className={cn(CELL_NUM, valueTone(channel.margin_pct))}>
              {formatPercent(channel.margin_pct)}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  )
}

export function PricingAnalysisDialog({ row, groupRatios, onOpenChange }: Props) {
  const { t } = useTranslation()
  const open = row !== null

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={row?.alias ?? ''}
      contentClassName='max-w-6xl sm:p-6'
      contentHeight='calc(100vh - 6rem)'
      bodyClassName='space-y-2'
    >
      {row && (
        <div className='space-y-2'>
          <div className='overflow-hidden rounded-[6px] border'>
            <table className='w-full table-fixed border-collapse text-[11px] [&>tbody>tr:last-child>td]:border-b-0'>
              <tbody>
                <tr>
                  <td className={cn(SUM_CELL, 'w-1/3')}>
                    <div className='text-muted-foreground text-[10px]'>{t('Revenue')}</div>
                    <div className={cn('font-semibold tabular-nums', valueTone(row.revenue))}>{formatMoney(row.revenue)}</div>
                  </td>
                  <td className={cn(SUM_CELL, 'w-1/3')}>
                    <div className='text-muted-foreground text-[10px]'>{t('Cost')}</div>
                    <div className='font-semibold tabular-nums'>{formatMoney(row.cost)}</div>
                  </td>
                  <td className={cn(SUM_CELL, 'w-1/3')}>
                    <div className='text-muted-foreground text-[10px]'>{t('Profit')}</div>
                    <div className={cn('font-semibold tabular-nums', valueTone(row.profit))}>{formatMoney(row.profit)}</div>
                  </td>
                </tr>
                <tr>
                  <td className={SUM_CELL}>
                    <div className='text-muted-foreground text-[10px]'>{t('Margin %')}</div>
                    <div className={cn('font-semibold tabular-nums', valueTone(row.margin_pct))}>{formatPercent(row.margin_pct)}</div>
                  </td>
                  <td className={SUM_CELL}>
                    <div className='text-muted-foreground text-[10px]'>{t('Req')}</div>
                    <div className='font-semibold tabular-nums'>{formatInteger(row.requests)}</div>
                  </td>
                  <td className={SUM_CELL}>
                    <div className='text-muted-foreground text-[10px]'>{t('Tokens')}</div>
                    <div className='font-semibold tabular-nums'>{formatTokens(row.tokens)}</div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          {row.channels.length === 0 ? (
            <p className='text-muted-foreground text-sm'>{t('No analysis data')}</p>
          ) : (
            <div className='space-y-3'>
              {row.channels.map((channel) => (
                <ChannelTable
                  key={`${channel.channel_id}-${channel.model_name}`}
                  channel={channel}
                  groupRatios={groupRatios}
                  t={t}
                />
              ))}
            </div>
          )}
        </div>
      )}
    </Dialog>
  )
}

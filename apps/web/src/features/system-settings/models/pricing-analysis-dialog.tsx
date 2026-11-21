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
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { cn } from '@/lib/utils'

import type {
  PricingAnalysisChannel,
  PricingAnalysisRow,
  PricingAnalysisChannelWindow,
} from '../types'

type Props = {
  row: PricingAnalysisRow | null
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

function formatMoney(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—'
  const sign = value < 0 ? '-' : ''
  return `${sign}$${Math.abs(value).toFixed(2)}`
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
      return t('Per token')
    case 'per-request':
      return t('Per request')
    case 'tiered_expr':
      return t('Tiered expression')
    case 'context_tier':
      return t('Context tiers (per token)')
    case 'context_tier_per_call':
      return t('Context tiers (per request)')
    case 'per_call':
      return t('Per request')
    case 'per_token':
      return t('Per token')
    case 'per_call_tiered':
      return t('Context tiers (per request)')
    case 'per_token_tiered':
      return t('Context tiers (per token)')
    default:
      return mode || '—'
  }
}

type Dimension = {
  key: string
  label: string
  window?: PricingAnalysisChannelWindow
  // Non-tiered channels reuse one canonical window whose cost/request/profit
  // figures are channel-wide totals; showing them per row duplicates the
  // Total row, so those metrics are suppressed for that single row.
  showMetrics: boolean
}

function ChannelTable({
  channel,
  t,
}: {
  channel: PricingAnalysisChannel
  t: (key: string) => string
}) {
  const hasContextTiers =
    channel.price_mode === 'per_token_tiered' ||
    channel.price_mode === 'per_call_tiered'
  const canonicalWindow =
    channel.windows.find((w) => w.context === 131072) ?? channel.windows[0]
  const dimensions: Dimension[] = hasContextTiers
    ? channel.windows.map((w) => ({
        key: String(w.context),
        label: `${t('Context')} ${formatContext(w.context)}`,
        window: w,
        showMetrics: true,
      }))
    : [
        {
          key: channel.price_mode || 'single',
          label: modeLabel(channel.billing_mode || channel.price_mode, t),
          window: canonicalWindow,
          showMetrics: false,
        },
      ]

  return (
    <div className='overflow-hidden rounded-[6px] border-x border-t'>
      <div className='bg-muted/30 flex flex-wrap items-center justify-between gap-2 border-b px-2 py-1.5'>
        <div className='flex min-w-0 flex-wrap items-center gap-2'>
          <span className='truncate text-sm font-semibold'>{channel.channel_name}</span>
          <span className='text-muted-foreground text-[11px]'>{channel.model_name}</span>
        </div>
        <span className='text-muted-foreground shrink-0 text-[10px]'>
          {modeLabel(channel.billing_mode || channel.price_mode, t)}
        </span>
      </div>

      <table className='w-full table-fixed border-collapse text-[10px]'>
        <colgroup>
          <col className='w-[14%]' />
          <col className='w-[14.3%]' />
          <col className='w-[14.3%]' />
          <col className='w-[14.3%]' />
          <col className='w-[14.3%]' />
          <col className='w-[14.3%]' />
          <col className='w-[14.5%]' />
        </colgroup>
          <thead>
            <tr>
              <th className={HEAD}>{t('Billing')}</th>
              <th className={HEAD_NUM}>{t('Local')}</th>
              <th className={HEAD_NUM}>{t('Upstream')}</th>
              <th className={HEAD_NUM}>{t('Cost')}</th>
              <th className={HEAD_NUM}>{t('Req')}</th>
              <th className={HEAD_NUM}>{t('Profit')}</th>
              <th className={HEAD_NUM}>{t('Margin %')}</th>
            </tr>
          </thead>
          <tbody>
            {dimensions.map((d) => (
              <tr key={d.key}>
                <td className={CELL}>{d.label}</td>
                <td className={CELL_NUM}>{formatMoney(d.window?.local)}</td>
                <td className={CELL_NUM}>{formatMoney(d.window?.upstream)}</td>
                <td className={CELL_NUM}>
                  {d.showMetrics ? formatMoney(d.window?.cost) : '—'}
                </td>
                <td className={CELL_NUM}>
                  {!d.showMetrics || d.window?.requests == null
                    ? '—'
                    : formatInteger(d.window.requests)}
                </td>
                <td className={cn(CELL_NUM, valueTone(d.window?.profit))}>
                  {d.showMetrics ? formatMoney(d.window?.profit) : '—'}
                </td>
                <td className={cn(CELL_NUM, valueTone(d.window?.margin_pct))}>
                  {d.showMetrics ? formatPercent(d.window?.margin_pct) : '—'}
                </td>
              </tr>
            ))}
            <tr className='bg-muted/30 font-medium'>
              <td className={CELL}>{t('Total')}</td>
              <td className={CELL_NUM}>—</td>
              <td className={CELL_NUM}>—</td>
              <td className={CELL_NUM}>{formatMoney(channel.cost)}</td>
              <td className={CELL_NUM}>{formatInteger(channel.requests)}</td>
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

export function PricingAnalysisDialog({ row, onOpenChange }: Props) {
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
              {row.channels.map((channel, index) => (
                <ChannelTable
                  key={`${channel.channel_id}-${channel.model_name}-${index}`}
                  channel={channel}
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

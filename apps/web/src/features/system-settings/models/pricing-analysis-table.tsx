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
import { ChevronDown, ChevronRight, Loader2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { formatCurrencyFromUSD } from '@/lib/currency'
import { cn } from '@/lib/utils'

import type {
  PricingAnalysisData,
  PricingAnalysisRow,
  PriceFluctuationChange,
} from '../types'

type PricingAnalysisTableProps = {
  data: PricingAnalysisData | null
  changes: PriceFluctuationChange[]
  isLoading: boolean
  vendor: number
  days: number
  threshold: number
  onVendorChange: (vendor: number) => void
  onDaysChange: (days: number) => void
  onThresholdChange: (threshold: number) => void
  onRefresh: () => void
  onRowClick: (row: PricingAnalysisRow) => void
  error: string | null
}

const TIME_RANGE_OPTIONS = [
  { value: '7', labelKey: 'Last 7 days' },
  { value: '30', labelKey: 'Last 30 days' },
  { value: '90', labelKey: 'Last 90 days' },
]

const headerClassName =
  'bg-muted/40 text-muted-foreground border-b px-3 py-2 text-left text-xs font-medium whitespace-nowrap'
const cellClassName = 'border-b px-3 py-2 align-middle whitespace-nowrap'
const numberClassName = 'tabular-nums'

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
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return '—'
  }
  return formatCurrencyFromUSD(value, {
    digitsLarge: 2,
    digitsSmall: 4,
  })
}

function formatPercent(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return '—'
  }
  return `${value.toFixed(2)}%`
}

function valueTone(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return 'text-muted-foreground'
  }
  if (value > 0) return 'text-green-600 dark:text-green-400'
  if (value < 0) return 'text-red-600 dark:text-red-400'
  return ''
}

function formatRawValue(value: unknown): string {
  if (value === null || value === undefined) return '—'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

type FragmentRowProps = {
  row: PricingAnalysisRow
  fluctuationCount: number
  onOpen: () => void
}

function FragmentRow({ row, fluctuationCount, onOpen }: FragmentRowProps) {
  const { t } = useTranslation()
  return (
    <tr className='hover:bg-muted/40 cursor-pointer' onClick={onOpen}>
      <td className={cellClassName}>
        <div className='font-medium'>{row.alias}</div>
        {row.price_model && row.price_model !== row.alias && (
          <div className='text-muted-foreground text-[11px]'>
            {t('Priced as {{model}}', { model: row.price_model })}
          </div>
        )}
      </td>
      <td className={cellClassName}>{row.vendor_name}</td>
      <td className={cn(cellClassName, numberClassName)}>
        {formatInteger(row.matched_count)}
      </td>
      <td className={cn(cellClassName, numberClassName)}>
        {formatInteger(row.requests)}
      </td>
      <td className={cn(cellClassName, numberClassName)}>
        {formatTokens(row.tokens)}
      </td>
      <td className={cn(cellClassName, numberClassName, valueTone(row.revenue))}>
        {formatMoney(row.revenue)}
      </td>
      <td className={cn(cellClassName, numberClassName)}>{formatMoney(row.cost)}</td>
      <td className={cn(cellClassName, numberClassName, valueTone(row.profit))}>
        {formatMoney(row.profit)}
      </td>
      <td className={cn(cellClassName, numberClassName, valueTone(row.margin_pct))}>
        {formatPercent(row.margin_pct)}
      </td>
      <td className={cellClassName}>
        {fluctuationCount > 0 && (
          <span className='bg-muted text-muted-foreground inline-flex min-w-6 items-center justify-center rounded-full px-2 py-0.5 text-xs'>
            {fluctuationCount}
          </span>
        )}
      </td>
      <td className={cellClassName}>
        <ChevronRight className='text-muted-foreground h-4 w-4' />
      </td>
    </tr>
  )
}

// Compact list item for narrow screens: one alias per card, each metric shown
// as a small label + value; short pairs share a row to avoid crowding.
function MobileRow({ row, fluctuationCount, onOpen }: FragmentRowProps) {
  const { t } = useTranslation()
  const metricCell = (
    label: string,
    value: React.ReactNode,
    tone = ''
  ) => (
    <td className='border-b border-r px-2 py-1.5 align-middle last:border-r-0'>
      <div className='text-muted-foreground text-[10px]'>{label}</div>
      <div className={cn('truncate text-xs tabular-nums', tone)}>{value}</div>
    </td>
  )
  return (
    <div
      role='button'
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          onOpen()
        }
      }}
      className='hover:bg-muted/30 focus-visible:ring-ring w-full cursor-pointer rounded-[3px] border-b px-3 py-2.5 text-left outline-none focus-visible:ring-1 last:border-b-0'
    >
      <div className='flex items-center justify-between gap-2'>
        <span className='min-w-0 flex-1 truncate text-sm font-medium'>
          {row.alias}
          <span className='text-muted-foreground ml-1 text-[10px] font-normal'>
            {formatInteger(row.matched_count)}{t('Matched models')}
          </span>
        </span>
        {row.vendor_name && (
          <span className='text-muted-foreground shrink-0 text-[10px]'>
            {row.vendor_name}
          </span>
        )}
        <ChevronRight className='text-muted-foreground h-4 w-4 shrink-0' />
      </div>
      {row.price_model && row.price_model !== row.alias && (
        <div className='text-muted-foreground mt-0.5 text-[10px]'>
          {t('Priced as {{model}}', { model: row.price_model })}
        </div>
      )}
      <div className='mt-2 overflow-hidden rounded-[6px] border'>
        <table className='w-full table-fixed border-collapse text-[11px]'>
        <tbody>
          <tr>
            {metricCell(t('Revenue'), formatMoney(row.revenue), valueTone(row.revenue))}
            {metricCell(t('Cost'), formatMoney(row.cost))}
            {metricCell(t('Profit'), formatMoney(row.profit), valueTone(row.profit))}
          </tr>
          <tr className='[&>td]:border-b-0'>
            {metricCell(t('Margin %'), formatPercent(row.margin_pct), valueTone(row.margin_pct))}
            {metricCell(t('Req'), formatInteger(row.requests))}
            {metricCell(t('Tokens'), formatTokens(row.tokens))}
          </tr>
        </tbody>
      </table>
      </div>
      {fluctuationCount > 0 && (
        <div className='text-muted-foreground mt-1.5 flex items-center gap-2 text-[10px]'>
          <span className='bg-muted rounded px-1.5 py-0.5'>
            {t('Fluctuation')} {fluctuationCount}
          </span>
        </div>
      )}
    </div>
  )
}
export function PricingAnalysisTable({
  data,
  changes,
  isLoading,
  vendor,
  days,
  threshold,
  onVendorChange,
  onDaysChange,
  onThresholdChange,
  onRefresh,
  onRowClick,
  error,
}: PricingAnalysisTableProps) {
  const { t } = useTranslation()
  const [fluctuationOpen, setFluctuationOpen] = useState(false)

  const rows = data?.rows ?? []
  const vendors = data?.vendors ?? []

  const fluctuationCountByAlias = useMemo(() => {
    const counts = new Map<string, number>()
    for (const row of rows) {
      if (!row || !Array.isArray(row.channels)) continue
      const models = new Set<string>([
        row.alias,
        ...row.channels.map((channel) => channel.model_name),
      ])
      counts.set(row.alias, changes.filter((c) => models.has(c.model)).length)
    }
    return counts
  }, [rows, changes])

  return (
    <div className='flex h-full min-h-0 flex-col gap-4'>
      <div className='flex shrink-0 flex-col gap-2 lg:flex-row lg:items-center'>
        <Select
          items={[
            { value: '0', label: t('All vendors') },
            ...vendors.map((item) => ({
              value: String(item.id),
              label: item.name,
            })),
          ]}
          value={String(vendor)}
          onValueChange={(value) => value !== null && onVendorChange(Number(value))}
          disabled={isLoading}
        >
          <SelectTrigger className='w-full lg:w-56' aria-label={t('Vendor')}>
            <SelectValue placeholder={t('All vendors')} />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              <SelectItem value='0'>{t('All vendors')}</SelectItem>
              {vendors.map((item) => (
                <SelectItem key={item.id} value={String(item.id)}>
                  {item.name}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>

        <Select
          items={TIME_RANGE_OPTIONS.map((option) => ({
            value: option.value,
            label: t(option.labelKey),
          }))}
          value={String(days)}
          onValueChange={(value) => value !== null && onDaysChange(Number(value))}
          disabled={isLoading}
        >
          <SelectTrigger className='w-full lg:w-48' aria-label={t('Time range')}>
            <SelectValue placeholder={t('Time range')} />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              {TIME_RANGE_OPTIONS.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {t(option.labelKey)}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>

        <Input
          type='number'
          min={0}
          value={threshold}
          onChange={(event) => {
            const next = Number(event.target.value)
            if (Number.isFinite(next) && next >= 0) onThresholdChange(next)
          }}
          disabled={isLoading}
          className='w-full lg:w-40'
          aria-label={t('Fluctuation alerts')}
          placeholder={t('Fluctuation alerts')}
        />


      </div>

      {isLoading && !data ? (
        <div className='flex min-h-64 flex-col items-center justify-center gap-3 rounded-md border'>
          <Loader2 className='text-muted-foreground h-8 w-8 animate-spin' />
          <p className='text-muted-foreground text-sm'>{t('Loading analysis')}</p>
        </div>
      ) : error ? (
        <div className='flex min-h-64 flex-col items-center justify-center gap-3 rounded-md border'>
          <p className='text-sm text-red-600 dark:text-red-400'>{error}</p>
          <Button size='sm' variant='outline' onClick={onRefresh}>
            {t('Retry')}
          </Button>
        </div>
      ) : rows.length === 0 ? (
        <div className='flex min-h-64 items-center justify-center rounded-md border'>
          <p className='text-muted-foreground text-sm'>{t('No analysis data')}</p>
        </div>
      ) : (
        <>
          <div className='min-h-0 flex-1 overflow-auto rounded-[3px] border p-px sm:hidden'>
            {rows.map((row) => (
              <MobileRow
                key={`${row.vendor_id}-${row.alias}-${row.price_model}`}
                row={row}
                fluctuationCount={fluctuationCountByAlias.get(row.alias) ?? 0}
                onOpen={() => onRowClick(row)}
              />
            ))}
          </div>
          <div className='hidden min-h-0 flex-1 overflow-auto rounded-md border sm:block'>
            <table className='w-full min-w-max border-collapse text-sm'>
              <thead>
                <tr>
                  <th className={headerClassName}>{t('Alias')}</th>
                  <th className={headerClassName}>{t('Vendor')}</th>
                  <th className={headerClassName}>{t('Matched models')}</th>
                  <th className={headerClassName}>{t('Requests')}</th>
                  <th className={headerClassName}>{t('Tokens')}</th>
                  <th className={headerClassName}>{t('Revenue')}</th>
                  <th className={headerClassName}>{t('Cost')}</th>
                  <th className={headerClassName}>{t('Profit')}</th>
                  <th className={headerClassName}>{t('Margin %')}</th>
                  <th className={headerClassName}>{t('Fluctuation')}</th>
                  <th className={headerClassName}>
                    <span className='sr-only'>{t('Show details')}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <FragmentRow
                    key={`${row.vendor_id}-${row.alias}-${row.price_model}`}
                    row={row}
                    fluctuationCount={fluctuationCountByAlias.get(row.alias) ?? 0}
                    onOpen={() => onRowClick(row)}
                  />
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}

      {changes.length > 0 && (
        <div className='shrink-0 rounded-md border'>
          <button
            type='button'
            className='flex w-full items-center gap-2 px-3 py-2 text-left text-sm font-medium'
            aria-expanded={fluctuationOpen}
            onClick={() => setFluctuationOpen((open) => !open)}
          >
            {fluctuationOpen ? (
              <ChevronDown className='h-4 w-4' />
            ) : (
              <ChevronRight className='h-4 w-4' />
            )}
            {t('Fluctuation alerts')}
            <span className='bg-muted text-muted-foreground rounded-full px-2 py-0.5 text-xs'>
              {changes.length}
            </span>
          </button>
          {fluctuationOpen && (
            <div className='max-h-72 overflow-auto border-t'>
              <table className='w-full border-collapse text-sm'>
                <thead>
                  <tr>
                    <th className={headerClassName}>{t('Source')}</th>
                    <th className={headerClassName}>{t('Model')}</th>
                    <th className={headerClassName}>{t('Field')}</th>
                    <th className={headerClassName}>{t('Old value')}</th>
                    <th className={headerClassName}>{t('New value')}</th>
                    <th className={headerClassName}>{t('Change %')}</th>
                  </tr>
                </thead>
                <tbody>
                  {changes.map((change, index) => (
                    <tr key={`${change.source}-${change.model}-${change.field}-${index}`}>
                      <td className={cellClassName}>{change.source}</td>
                      <td className={cellClassName}>{change.model}</td>
                      <td className={cellClassName}>{change.field}</td>
                      <td className={cellClassName}>{formatRawValue(change.old_value)}</td>
                      <td className={cellClassName}>{formatRawValue(change.new_value)}</td>
                      <td className={cn(cellClassName, valueTone(change.change_pct))}>
                        {formatPercent(change.change_pct)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

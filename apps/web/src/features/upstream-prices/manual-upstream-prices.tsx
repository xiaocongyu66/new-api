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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { StatusBadge } from '@/components/status-badge'
import { Combobox } from '@/components/ui/combobox'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import { getAllModels, getChannel } from '@/features/channels/api'
import {
  deleteManualUpstreamPrice,
  getManualUpstreamPrices,
  getUpstreamChannels,
  upsertManualUpstreamPrice,
} from '@/features/system-settings/api'
import type {
  ManualUpstreamPrice,
  ManualUpstreamPriceMode,
} from '@/features/system-settings/types'

const MODE_LABELS: Record<ManualUpstreamPriceMode, string> = {
  per_token: 'Per token',
  per_call: 'Per request',
  expr: 'Expression',
}

// USD per 1M tokens for per-token rows, USD per request for per-call rows.
function formatPerMillion(value: number): string {
  return `$${value} /1M`
}

function formatPerCall(value: number): string {
  return `$${value} /req`
}

// 输入价格列按计费方式换单位：按次是美元/次，按量是美元/百万 tokens，
// 表达式模式没有单列价格。
// 窄屏卡片里的价格明细：按计费方式只展示有意义的字段。
function mobilePriceFields(
  row: ManualUpstreamPrice,
  t: (key: string) => string
): ReactNode {
  if (row.mode === 'per_call') {
    return (
      <div>
        <dt className='text-muted-foreground'>{t('Price (USD / request)')}</dt>
        <dd className='tabular-nums'>{formatPerCall(row.price_in)}</dd>
      </div>
    )
  }
  if (row.mode === 'per_token') {
    return (
      <>
        <div>
          <dt className='text-muted-foreground'>{t('Input price')}</dt>
          <dd className='tabular-nums'>{formatPerMillion(row.price_in)}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Output price')}</dt>
          <dd className='tabular-nums'>{formatPerMillion(row.price_out)}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Cache read')}</dt>
          <dd className='tabular-nums'>{formatPerMillion(row.price_cache_read)}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Cache write')}</dt>
          <dd className='tabular-nums'>{formatPerMillion(row.price_cache_write)}</dd>
        </div>
      </>
    )
  }
  return null
}

function inputPriceCell(row: ManualUpstreamPrice): string {
  if (row.mode === 'per_call') return formatPerCall(row.price_in)
  if (row.mode === 'per_token') return formatPerMillion(row.price_in)
  return '—'
}

export function ManualUpstreamPrices() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [channelId, setChannelId] = useState<number | null>(null)
  const [modelName, setModelName] = useState('')
  const [mode, setMode] = useState<ManualUpstreamPriceMode>('per_token')
  const [priceIn, setPriceIn] = useState('')
  const [priceOut, setPriceOut] = useState('')
  const [priceCacheRead, setPriceCacheRead] = useState('')
  const [priceCacheWrite, setPriceCacheWrite] = useState('')
  const [expr, setExpr] = useState('')

  const { data: channelsData } = useQuery({
    queryKey: ['upstream-channels'],
    queryFn: getUpstreamChannels,
  })
  // Only real channels can carry a manual price; the synthesized presets have
  // no channel row to attach to.
  const channels = useMemo(
    () => (channelsData?.data ?? []).filter((channel) => channel.id > 0),
    [channelsData]
  )

  // The channel's own model list is the authoritative set for hand-entered
  // prices; a channel with no restriction (empty list) falls back to every
  // model the site knows about.
  const { data: channelDetail } = useQuery({
    queryKey: ['channel-detail-for-manual-price', channelId],
    queryFn: () => getChannel(channelId as number),
    enabled: channelId !== null && channelId > 0,
  })
  const channelModels = useMemo(() => {
    const raw = channelDetail?.data?.models ?? ''
    return raw
      .split(',')
      .map((model) => model.trim())
      .filter(Boolean)
      .sort((a, b) => a.localeCompare(b))
  }, [channelDetail])

  const { data: allModelsData } = useQuery({
    queryKey: ['all-models-for-manual-price'],
    queryFn: getAllModels,
    enabled: channelModels.length === 0,
    staleTime: 5 * 60 * 1000,
  })
  const modelOptions = useMemo(() => {
    const names =
      channelModels.length > 0
        ? channelModels
        : (allModelsData?.data ?? []).map((model) => model.id)
    return names.map((name) => ({ value: name, label: name }))
  }, [channelModels, allModelsData])

  const { data: manualData, isLoading } = useQuery({
    queryKey: ['manual-upstream-prices', channelId],
    queryFn: () => getManualUpstreamPrices(channelId as number),
    enabled: channelId !== null,
  })
  const rows = manualData?.data ?? []

  const invalidate = () => {
    queryClient.invalidateQueries({
      queryKey: ['manual-upstream-prices', channelId],
    })
    queryClient.invalidateQueries({ queryKey: ['pricing-analysis'] })
  }

  const saveMutation = useMutation({
    mutationFn: upsertManualUpstreamPrice,
    onSuccess: (data) => {
      if (data.success) {
        toast.success(t('Manual price saved'))
        setModelName('')
        setPriceIn('')
        setPriceOut('')
        setPriceCacheRead('')
        setPriceCacheWrite('')
        setExpr('')
        invalidate()
      } else {
        toast.error(data.message || t('Failed to save manual price'))
      }
    },
    onError: () => toast.error(t('Failed to save manual price')),
  })

  const deleteMutation = useMutation({
    mutationFn: (row: ManualUpstreamPrice) =>
      deleteManualUpstreamPrice(row.channel_id, row.model_name),
    onSuccess: (data) => {
      if (data.success) {
        toast.success(t('Manual price deleted'))
        invalidate()
      } else {
        toast.error(data.message || t('Failed to delete manual price'))
      }
    },
    onError: () => toast.error(t('Failed to delete manual price')),
  })

  const selectedChannel = channels.find((channel) => channel.id === channelId)
  const isPerToken = mode === 'per_token'
  const isPerCall = mode === 'per_call'
  const isExpr = mode === 'expr'

  // Optional USD/1M coefficient: blank means "not charged for this class".
  const optionalPrice = (value: string): number | null => {
    if (value.trim() === '') return 0
    const parsed = Number(value)
    return Number.isFinite(parsed) && parsed >= 0 ? parsed : null
  }

  const handleSubmit = () => {
    const name = modelName.trim()
    if (channelId === null) {
      toast.error(t('Select a channel first'))
      return
    }
    if (name === '') {
      toast.error(t('Model name is required'))
      return
    }
    if (isExpr) {
      if (expr.trim() === '') {
        toast.error(t('Expression is required'))
        return
      }
      saveMutation.mutate({
        channel_id: channelId,
        model_name: name,
        mode,
        price_in: 0,
        price_out: 0,
        price_cache_read: 0,
        price_cache_write: 0,
        expr: expr.trim(),
      })
      return
    }

    const input = optionalPrice(priceIn)
    if (input === null || (isPerCall && priceIn.trim() === '')) {
      toast.error(t(isPerCall ? 'Price is required' : 'Input price is required'))
      return
    }
    const output = isPerToken ? optionalPrice(priceOut) : 0
    const cacheRead = isPerToken ? optionalPrice(priceCacheRead) : 0
    const cacheWrite = isPerToken ? optionalPrice(priceCacheWrite) : 0
    if (output === null || cacheRead === null || cacheWrite === null) {
      toast.error(t('Prices must be non-negative numbers'))
      return
    }
    saveMutation.mutate({
      channel_id: channelId,
      model_name: name,
      mode,
      price_in: input,
      price_out: output,
      price_cache_read: cacheRead,
      price_cache_write: cacheWrite,
      expr: '',
    })
  }

  // Table body is derived in statements instead of a nested ternary chain:
  // four mutually exclusive states read badly as `a ? x : b ? y : c ? z : w`.
  const emptyRow = (message: string) => (
    <tr>
      <td className='text-muted-foreground px-3 py-6' colSpan={9}>
        {message}
      </td>
    </tr>
  )

  let body: ReactNode
  if (channelId === null) {
    body = emptyRow(t('Select a channel first'))
  } else if (isLoading) {
    body = emptyRow(t('Loading...'))
  } else if (rows.length === 0) {
    body = emptyRow(t('No manual prices for this channel'))
  } else {
    body = rows.map((row) => (
      <tr key={row.model_name} className='border-t'>
        <td className='px-3 py-2 font-mono text-xs'>{row.model_name}</td>
        <td className='px-3 py-2'>{t(MODE_LABELS[row.mode])}</td>
        <td className='px-3 py-2'>{inputPriceCell(row)}</td>
        <td className='px-3 py-2'>
          {row.mode === 'per_token' ? formatPerMillion(row.price_out) : '—'}
        </td>
        <td className='px-3 py-2'>
          {row.mode === 'per_token' ? formatPerMillion(row.price_cache_read) : '—'}
        </td>
        <td className='px-3 py-2'>
          {row.mode === 'per_token' ? formatPerMillion(row.price_cache_write) : '—'}
        </td>
        <td className='text-muted-foreground max-w-64 truncate px-3 py-2 font-mono text-xs' title={row.expr}>
          {row.mode === 'expr' ? row.expr : '—'}
        </td>
        <td className='text-muted-foreground px-3 py-2 text-xs'>
          {row.updated_at
            ? new Date(row.updated_at * 1000).toLocaleString()
            : '—'}
        </td>
        <td className='px-3 py-2 text-right'>
          <Button
            size='sm'
            variant='ghost'
            aria-label={t('Delete')}
            onClick={() => deleteMutation.mutate(row)}
          >
            <Trash2 className='h-4 w-4' />
          </Button>
        </td>
      </tr>
    ))
  }

  const mobileEmpty = (message: string) => (
    <p className='text-muted-foreground rounded-md border px-3 py-6 text-sm'>
      {message}
    </p>
  )

  let mobileList: ReactNode
  if (channelId === null) {
    mobileList = mobileEmpty(t('Select a channel first'))
  } else if (isLoading) {
    mobileList = mobileEmpty(t('Loading...'))
  } else if (rows.length === 0) {
    mobileList = mobileEmpty(t('No manual prices for this channel'))
  } else {
    mobileList = rows.map((row) => (
      <div key={row.model_name} className='rounded-md border px-3 py-2.5'>
        <div className='flex items-start justify-between gap-2'>
          <div className='min-w-0'>
            <div className='truncate font-mono text-xs'>{row.model_name}</div>
            <div className='text-muted-foreground text-[11px]'>
              {t(MODE_LABELS[row.mode])}
            </div>
          </div>
          <Button
            size='sm'
            variant='ghost'
            aria-label={t('Delete')}
            onClick={() => deleteMutation.mutate(row)}
          >
            <Trash2 className='h-4 w-4' />
          </Button>
        </div>

        <dl className='mt-2 grid grid-cols-2 gap-x-3 gap-y-1 text-[11px]'>
          {mobilePriceFields(row, t)}
        </dl>

        {row.mode === 'expr' && (
          <div className='bg-muted mt-2 rounded px-2 py-1 font-mono text-[10px] break-all'>
            {row.expr}
          </div>
        )}

        {row.updated_at && (
          <div className='text-muted-foreground mt-2 text-[10px]'>
            {new Date(row.updated_at * 1000).toLocaleString()}
          </div>
        )}
      </div>
    ))
  }

  return (
    <div className='flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto pb-4'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Manual prices cover upstreams that expose no /api/pricing endpoint. A fetched snapshot always wins over a manual entry.'
        )}
      </p>

      {/* 第一行：渠道 + 模型（窄屏各占一行） */}
      <div className='flex flex-col items-stretch gap-3 sm:flex-row sm:flex-wrap sm:items-end'>
        <div className='flex flex-col gap-1.5'>
          <label className='text-xs font-medium' htmlFor='manual-channel'>
            {t('Channel')}
          </label>
          <Select
            value={channelId === null ? '' : String(channelId)}
            onValueChange={(value) => {
              setChannelId(Number(value))
              setModelName('')
            }}
          >
            <SelectTrigger id='manual-channel' className='w-full sm:w-64'>
              <SelectValue>
                {selectedChannel?.name ?? t('Select a channel')}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {channels.map((channel) => (
                <SelectItem key={channel.id} value={String(channel.id)}>
                  <span className='flex items-center gap-2'>
                    <span className='truncate'>{channel.name}</span>
                    {channel.last_sync_status === 'error' && (
                      <StatusBadge
                        label={t('No pricing API')}
                        variant='danger'
                        size='sm'
                        copyable={false}
                      />
                    )}
                  </span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className='flex flex-col gap-1.5'>
          <label className='text-xs font-medium' htmlFor='manual-model'>
            {t('Model')}
          </label>
          <Combobox
            id='manual-model'
            options={modelOptions}
            value={modelName}
            onValueChange={(value) => setModelName(value ?? '')}
            placeholder={t('Select a model')}
            searchPlaceholder={t('Search model...')}
            emptyText={t('No matching model found.')}
            openOnFocus={false}
            className='w-full sm:w-72'
          />
        </div>
        {selectedChannel?.last_sync_status === 'error' && (
          <span className='text-muted-foreground max-w-md pb-2 text-xs'>
            {selectedChannel.last_sync_error}
          </span>
        )}
      </div>

      {/* 第二行：计费方式 + 该方式下的价格字段（窄屏各占一行） */}
      <div className='flex flex-col items-stretch gap-3 sm:flex-row sm:flex-wrap sm:items-end'>
        <div className='flex flex-col gap-1.5'>
          <label className='text-xs font-medium' htmlFor='manual-mode'>
            {t('Price mode')}
          </label>
          <Select
            value={mode}
            onValueChange={(value) => setMode(value as ManualUpstreamPriceMode)}
          >
            <SelectTrigger id='manual-mode' className='w-full sm:w-40'>
              <SelectValue>{t(MODE_LABELS[mode])}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='per_token'>{t('Per token')}</SelectItem>
              <SelectItem value='per_call'>{t('Per request')}</SelectItem>
              <SelectItem value='expr'>{t('Expression')}</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {isPerCall && (
          <div className='flex flex-col gap-1.5'>
            <label className='text-xs font-medium' htmlFor='manual-price-in'>
              {t('Price (USD / request)')}
            </label>
            <Input
              id='manual-price-in'
              className='w-full sm:w-44'
              inputMode='decimal'
              value={priceIn}
              onChange={(event) => setPriceIn(event.target.value)}
            />
          </div>
        )}

        {isPerToken && (
          <div className='grid grid-cols-2 gap-3 sm:contents'>

            <div className='flex flex-col gap-1.5'>
              <label className='text-xs font-medium' htmlFor='manual-price-in'>
                {t('Input price (USD / 1M tokens)')}
              </label>
              <Input
                id='manual-price-in'
                className='w-full sm:w-44'
                inputMode='decimal'
                value={priceIn}
                onChange={(event) => setPriceIn(event.target.value)}
              />
            </div>
            <div className='flex flex-col gap-1.5'>
              <label className='text-xs font-medium' htmlFor='manual-price-out'>
                {t('Output price (USD / 1M tokens)')}
              </label>
              <Input
                id='manual-price-out'
                className='w-full sm:w-44'
                inputMode='decimal'
                value={priceOut}
                onChange={(event) => setPriceOut(event.target.value)}
              />
            </div>
            <div className='flex flex-col gap-1.5'>
              <label className='text-xs font-medium' htmlFor='manual-price-cache-read'>
                {t('Cache read price (USD / 1M tokens)')}
              </label>
              <Input
                id='manual-price-cache-read'
                className='w-full sm:w-44'
                inputMode='decimal'
                value={priceCacheRead}
                onChange={(event) => setPriceCacheRead(event.target.value)}
              />
            </div>
            <div className='flex flex-col gap-1.5'>
              <label className='text-xs font-medium' htmlFor='manual-price-cache-write'>
                {t('Cache write price (USD / 1M tokens)')}
              </label>
              <Input
                id='manual-price-cache-write'
                className='w-full sm:w-44'
                inputMode='decimal'
                value={priceCacheWrite}
                onChange={(event) => setPriceCacheWrite(event.target.value)}
              />
            </div>
          </div>
        )}

        {isExpr && (
          <div className='flex flex-col gap-1.5 sm:min-w-96 sm:flex-1'>
            <label className='text-xs font-medium' htmlFor='manual-expr'>
              {t('Billing expression')}
            </label>
            <Textarea
              id='manual-expr'
              rows={2}
              className='min-h-16 font-mono text-xs'
              placeholder='tier("base", p * 3 + c * 15 + cr * 0.3 + cc * 3.75)'
              value={expr}
              onChange={(event) => setExpr(event.target.value)}
            />
          </div>
        )}

        <Button
          onClick={handleSubmit}
          disabled={saveMutation.isPending || channelId === null}
          className='w-full sm:w-auto'
        >
          <Plus className='mr-1 h-4 w-4' />
          {t('Add manual price')}
        </Button>
      </div>

      {/* 窄屏用卡片列表：9 列表格在手机上会把表头挤成竖排单字 */}
      <div className='flex flex-col gap-2 sm:hidden'>
        {mobileList}
      </div>

      <div className='hidden overflow-x-auto rounded-md border sm:block'>
        <table className='w-full text-sm'>
          <thead className='bg-muted/50'>
            <tr className='text-left'>
              <th className='px-3 py-2 font-medium'>{t('Model')}</th>
              <th className='px-3 py-2 font-medium'>{t('Price mode')}</th>
              <th className='px-3 py-2 font-medium'>{t('Input price')}</th>
              <th className='px-3 py-2 font-medium'>{t('Output price')}</th>
              <th className='px-3 py-2 font-medium'>{t('Cache read')}</th>
              <th className='px-3 py-2 font-medium'>{t('Cache write')}</th>
              <th className='px-3 py-2 font-medium'>{t('Expression')}</th>
              <th className='px-3 py-2 font-medium'>{t('Updated at')}</th>
              <th className='px-3 py-2 font-medium' />
            </tr>
          </thead>
          <tbody>{body}</tbody>
        </table>
      </div>
    </div>
  )
}

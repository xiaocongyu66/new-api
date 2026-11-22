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
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import { getAllModels } from '@/features/channels/api'
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

// Prices typed here are USD (per request, or per 1M tokens), matching the
// USD/1M coefficients the pricing expression evaluator already uses.
function parsePrice(value: string): number | null {
  const trimmed = value.trim()
  if (trimmed === '') return null
  const price = Number(trimmed)
  if (!Number.isFinite(price) || price < 0) return null
  return price
}

// USD shown as stored: per request, or per 1M tokens for the two-coefficient mode.
function formatManualPrice(
  row: ManualUpstreamPrice,
  side: 'in' | 'out'
): string {
  const value = side === 'in' ? row.price_in : row.price_out
  const unit = row.mode === 'per_token' ? '/1M' : '/req'
  return `$${value} ${unit}`
}

export function ManualUpstreamPrices() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [channelId, setChannelId] = useState<number | null>(null)
  const [modelName, setModelName] = useState('')
  const [mode, setMode] = useState<ManualUpstreamPriceMode>('per_token')
  const [priceIn, setPriceIn] = useState('')
  const [priceOut, setPriceOut] = useState('')

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

  const { data: modelsData } = useQuery({
    queryKey: ['all-models-for-manual-price'],
    queryFn: getAllModels,
    staleTime: 5 * 60 * 1000,
  })
  const modelOptions = useMemo(
    () => (modelsData?.data ?? []).map((model) => model.id),
    [modelsData]
  )

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
    const input = parsePrice(priceIn)
    if (input === null) {
      toast.error(t(isPerToken ? 'Input price is required' : 'Price is required'))
      return
    }
    const output = isPerToken ? parsePrice(priceOut) : 0
    if (output === null) {
      toast.error(t('Output price is required'))
      return
    }
    saveMutation.mutate({
      channel_id: channelId,
      model_name: name,
      mode,
      price_in: input,
      price_out: output,
    })
  }

  // Table body is derived in statements instead of a nested ternary chain:
  // four mutually exclusive states read badly as `a ? x : b ? y : c ? z : w`.
  const emptyRow = (message: string) => (
    <tr>
      <td className='text-muted-foreground px-3 py-6' colSpan={6}>
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
        <td className='px-3 py-2'>
          {row.mode === 'per_token' ? t('Per token') : t('Per request')}
        </td>
        <td className='px-3 py-2'>
          {formatManualPrice(row, 'in')}
        </td>
        <td className='px-3 py-2'>
          {row.mode === 'per_token' ? formatManualPrice(row, 'out') : '—'}
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

  return (
    <div className='flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto pb-4'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Manual prices cover upstreams that expose no /api/pricing endpoint. A fetched snapshot always wins over a manual entry.'
        )}
      </p>

      <div className='flex flex-wrap items-end gap-3'>
        <div className='flex min-w-56 flex-col gap-1.5'>
          <label className='text-xs font-medium' htmlFor='manual-channel'>
            {t('Channel')}
          </label>
          <Select
            value={channelId === null ? '' : String(channelId)}
            onValueChange={(value) => setChannelId(Number(value))}
          >
            <SelectTrigger id='manual-channel' className='w-64'>
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
        {selectedChannel?.last_sync_status === 'error' && (
          <span className='text-muted-foreground max-w-md text-xs'>
            {selectedChannel.last_sync_error}
          </span>
        )}
      </div>

      <div className='flex flex-wrap items-end gap-3'>
        <div className='flex flex-col gap-1.5'>
          <label className='text-xs font-medium' htmlFor='manual-model'>
            {t('Model')}
          </label>
          <Input
            id='manual-model'
            list='manual-model-options'
            className='w-64'
            placeholder='gpt-4o'
            value={modelName}
            onChange={(event) => setModelName(event.target.value)}
          />
          <datalist id='manual-model-options'>
            {modelOptions.map((model) => (
              <option key={model} value={model} />
            ))}
          </datalist>
        </div>
        <div className='flex flex-col gap-1.5'>
          <label className='text-xs font-medium' htmlFor='manual-mode'>
            {t('Price mode')}
          </label>
          <Select
            value={mode}
            onValueChange={(value) => setMode(value as ManualUpstreamPriceMode)}
          >
            <SelectTrigger id='manual-mode' className='w-40'>
              <SelectValue>
                {isPerToken ? t('Per token') : t('Per request')}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='per_token'>{t('Per token')}</SelectItem>
              <SelectItem value='per_call'>{t('Per request')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className='flex flex-col gap-1.5'>
          <label className='text-xs font-medium' htmlFor='manual-price-in'>
            {isPerToken
              ? t('Input price (USD / 1M tokens)')
              : t('Price (USD / request)')}
          </label>
          <Input
            id='manual-price-in'
            className='w-44'
            inputMode='decimal'
            value={priceIn}
            onChange={(event) => setPriceIn(event.target.value)}
          />
        </div>
        {isPerToken && (
          <div className='flex flex-col gap-1.5'>
            <label className='text-xs font-medium' htmlFor='manual-price-out'>
              {t('Output price (USD / 1M tokens)')}
            </label>
            <Input
              id='manual-price-out'
              className='w-44'
              inputMode='decimal'
              value={priceOut}
              onChange={(event) => setPriceOut(event.target.value)}
            />
          </div>
        )}
        <Button
          onClick={handleSubmit}
          disabled={saveMutation.isPending || channelId === null}
        >
          <Plus className='mr-1 h-4 w-4' />
          {t('Add manual price')}
        </Button>
      </div>

      <div className='overflow-x-auto rounded-md border'>
        <table className='w-full text-sm'>
          <thead className='bg-muted/50'>
            <tr className='text-left'>
              <th className='px-3 py-2 font-medium'>{t('Model')}</th>
              <th className='px-3 py-2 font-medium'>{t('Price mode')}</th>
              <th className='px-3 py-2 font-medium'>{t('Input price')}</th>
              <th className='px-3 py-2 font-medium'>{t('Output price')}</th>
              <th className='px-3 py-2 font-medium'>{t('Updated at')}</th>
              <th className='px-3 py-2 font-medium' />
            </tr>
          </thead>
          <tbody>
            {body}
          </tbody>
        </table>
      </div>
    </div>
  )
}
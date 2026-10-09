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
import { Loader2, RefreshCw, PowerOff, RotateCcw, Zap } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Button } from '@/components/ui/button'

import {
  getChannelModelHealth,
  updateChannelModelHealth,
  type ChannelModelHealthRow,
} from '../../api'
import { useChannels } from '../channels-provider'

type RouteHealthDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

const STATE_TONE: Record<ChannelModelHealthRow['state'], StatusVariant> = {
  ok: 'success',
  cooling: 'warning',
  slow_start: 'info',
  terminal: 'danger',
}

// formatCountdown renders the server-provided remaining cooldown in
// milliseconds. The server sends the remaining time rather than only an
// absolute deadline, so a browser clock that drifts from the server cannot
// invent or hide a cooldown window.
function formatCountdown(ms: number) {
  if (ms <= 0) return '—'
  if (ms < 1000) return `${Math.ceil(ms)}ms`
  const totalSeconds = Math.round(ms / 1000)
  if (totalSeconds < 60) return `${totalSeconds}s`
  const minutes = Math.floor(totalSeconds / 60)
  const rest = totalSeconds % 60
  if (minutes < 60) return rest > 0 ? `${minutes}m ${rest}s` : `${minutes}m`
  const hours = Math.floor(minutes / 60)
  return `${hours}h ${minutes % 60}m`
}

export function RouteHealthDialog({
  open,
  onOpenChange,
}: RouteHealthDialogProps) {
  const { t } = useTranslation()
  const { currentRow } = useChannels()
  const [rows, setRows] = useState<ChannelModelHealthRow[]>([])
  const [isLoading, setIsLoading] = useState(false)
  const [pendingRoute, setPendingRoute] = useState<string | null>(null)

  const channelId = currentRow?.id

  const load = useCallback(async () => {
    if (!channelId) return
    setIsLoading(true)
    try {
      const res = await getChannelModelHealth(channelId)
      if (!res.success) {
        throw new Error(res.message || t('Failed to load route health'))
      }
      setRows(res.data ?? [])
    } catch (error: unknown) {
      toast.error(
        error instanceof Error ? error.message : t('Failed to load route health')
      )
    } finally {
      setIsLoading(false)
    }
  }, [channelId, t])

  useEffect(() => {
    if (!open) return
    load()
  }, [open, load])

  const runAction = async (
    action: 'disable' | 'recover' | 'force_recall',
    keyIndex: number,
    model: string
  ) => {
    if (!channelId) return
    const routeId = `${keyIndex}:${model}`
    setPendingRoute(routeId)
    try {
      const res = await updateChannelModelHealth(
        action,
        channelId,
        keyIndex,
        model
      )
      if (!res.success) {
        throw new Error(res.message || t('Failed to update route health'))
      }
      toast.success(
        action === 'recover'
          ? t('Route recovered')
          : action === 'force_recall'
            ? t('Route recalled')
            : t('Route disabled')
      )
      await load()
    } catch (error: unknown) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to update route health')
      )
    } finally {
      setPendingRoute(null)
    }
  }

  if (!currentRow) return null

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Route health')}
      description={
        <>
          {t('Per-model isolation state for:')}{' '}
          <strong>{currentRow.name}</strong>
        </>
      }
      contentHeight='auto'
      footer={
        <div className='flex gap-2'>
          <Button variant='outline' onClick={load} disabled={isLoading}>
            {isLoading ? (
              <Loader2 className='mr-2 size-4 animate-spin' />
            ) : (
              <RefreshCw className='mr-2 size-4' />
            )}
            {t('Refresh')}
          </Button>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Close')}
          </Button>
        </div>
      }
    >
      <div className='space-y-3 py-2'>
        {rows.length === 0 && (
          <p className='text-muted-foreground text-sm'>
            {isLoading
              ? t('Loading...')
              : t(
                  'No isolation recorded. Every model on this channel is routable.'
                )}
          </p>
        )}

        {rows.length > 0 && (
          <div className='overflow-x-auto rounded-lg border'>
            <table className='w-full text-sm'>
              <thead className='bg-muted/50 text-muted-foreground'>
                <tr>
                  <th className='px-3 py-2 text-left font-medium'>
                    {t('Model')}
                  </th>
                  <th className='px-3 py-2 text-left font-medium'>
                    {t('Key index')}
                  </th>
                  <th className='px-3 py-2 text-left font-medium'>
                    {t('State')}
                  </th>
                  <th className='px-3 py-2 text-left font-medium'>
                    {t('Cooldowns in')}
                  </th>
                  <th className='px-3 py-2 text-left font-medium'>
                    {t('Score')}
                  </th>
                  <th className='px-3 py-2 text-left font-medium'>
                    {t('Slow start')}
                  </th>
                  <th className='px-3 py-2 text-left font-medium'>
                    {t('Last outcome')}
                  </th>
                  <th className='px-3 py-2 text-right font-medium'>
                    {t('Actions')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <tr key={`${row.key_index}:${row.model}`} className='border-t'>
                    <td className='px-3 py-2 font-mono text-xs'>{row.model}</td>
                    <td className='px-3 py-2 font-mono text-xs'>{row.key_index}</td>
                    <td className='px-3 py-2'>
                      <StatusBadge variant={STATE_TONE[row.state]}>
                        {row.state}
                      </StatusBadge>
                    </td>
                    <td className='px-3 py-2'>
                      {formatCountdown(row.remaining_cooldown_ms)}
                    </td>
                    <td className='px-3 py-2'>{row.ewma_score.toFixed(2)}</td>
                    <td className='px-3 py-2'>
                      {row.state === 'slow_start'
                        ? `${Math.round(row.slow_start_factor * 100)}%`
                        : '—'}
                    </td>
                    <td className='px-3 py-2'>{row.last_cooling_outcome || '—'}</td>
                    <td className='px-3 py-2'>
                      <div className='flex justify-end gap-2'>
                        {row.state === 'cooling' && (
                          <Button
                            variant='outline'
                            size='sm'
                            disabled={
                              pendingRoute === `${row.key_index}:${row.model}`
                            }
                            onClick={() =>
                              runAction('force_recall', row.key_index, row.model)
                            }
                          >
                            <Zap className='mr-1 size-3.5' />
                            {t('Force recall')}
                          </Button>
                        )}
                        <Button
                          variant='outline'
                          size='sm'
                          disabled={
                            pendingRoute === `${row.key_index}:${row.model}` ||
                            row.state !== 'terminal'
                          }
                          onClick={() =>
                            runAction('recover', row.key_index, row.model)
                          }
                        >
                          <RotateCcw className='mr-1 size-3.5' />
                          {t('Recover')}
                        </Button>
                        <Button
                          variant='outline'
                          size='sm'
                          className='text-destructive hover:text-destructive'
                          disabled={
                            pendingRoute === `${row.key_index}:${row.model}` ||
                            row.state === 'terminal'
                          }
                          onClick={() =>
                            runAction('disable', row.key_index, row.model)
                          }
                        >
                          <PowerOff className='mr-1 size-3.5' />
                          {t('Disable')}
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </Dialog>
  )
}

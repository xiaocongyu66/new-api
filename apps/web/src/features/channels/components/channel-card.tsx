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
import { flexRender, type Row } from '@tanstack/react-table'
import { memo, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { GroupBadge } from '@/components/group-badge'
import { StatusBadge } from '@/components/status-badge'
import { toIntlLocale } from '@/i18n/languages'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'

import type { ChannelRequestStat } from '../api'
import { CHANNEL_STATUS } from '../constants'
import {
  formatRelativeTime,
  formatResponseTime,
  getChannelTypeIcon,
  getChannelTypeLabel,
  getResponseTimeConfig,
  isTagAggregateRow,
  parseGroupsList,
  parseModelsList,
  type RouteHealthSummary,
} from '../lib'
import type { Channel } from '../types'
import { ChannelRowActionsLayoutContext } from './channel-row-actions-context'
import { useChannels } from './channels-provider'

const SENSITIVE_MASK = '••••'
const VISIBLE_MODEL_COUNT = 4
const VISIBLE_GROUP_COUNT = 3

/**
 * Card view for one channel. The table's own cells stay the source of truth for
 * interactive parts (selection, status tooltips, actions, balance update), while
 * the information the table cannot show side by side — 24h request aggregate,
 * route-unit health, model list — gets its own compact strip so operators can
 * judge a channel without opening the edit dialog.
 */
function ChannelCardComponent({
  row,
  requestStat,
  routeHealth,
}: {
  row: Row<Channel>
  requestStat?: ChannelRequestStat
  routeHealth?: RouteHealthSummary
}) {
  const { t, i18n } = useTranslation()
  const { sensitiveVisible, setCurrentRow, setOpen } = useChannels()
  const channel = row.original
  const isTagRow = isTagAggregateRow(channel)
  const cells = row.getAllCells()

  const renderCell = (id: string) => {
    const cell = cells.find((c) => c.column.id === id)
    if (!cell || !cell.column.columnDef.cell) {
      return null
    }
    return flexRender(cell.column.columnDef.cell, cell.getContext())
  }

  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const numberFormat = useMemo(() => new Intl.NumberFormat(locale), [locale])
  const compactFormat = useMemo(
    () =>
      new Intl.NumberFormat(locale, {
        notation: 'compact',
        maximumFractionDigits: 1,
      }),
    [locale]
  )

  const selectCell = renderCell('select')
  const nameCell = renderCell('name')
  const statusCell = renderCell('status')
  const actionsCell = renderCell('actions')
  const tagCell = channel.tag ? renderCell('tag') : null
  const groups = parseGroupsList(channel.group ?? '')

  // Aggregate tag rows collapse many channels; traffic and health are per
  // channel facts, so that layout stays as it was.
  if (isTagRow) {
    return (
      <ChannelRowActionsLayoutContext.Provider value='card'>
        <div className='flex flex-col gap-1.5 px-3 py-2.5'>
          <div className='flex h-5 items-center justify-between gap-2'>
            <div className='flex min-w-0 flex-1 items-center gap-2'>
              {selectCell && <span className='shrink-0'>{selectCell}</span>}
              <div className='min-w-0 flex-1 text-sm'>{nameCell}</div>
            </div>
            <div className='-my-1 flex shrink-0 items-center gap-1.5 [&_button]:size-7'>
              {statusCell}
              {actionsCell}
            </div>
          </div>
          <div className='flex flex-wrap gap-1'>
            {groups.map((group) => (
              <GroupBadge
                key={group}
                group={group}
                label={sensitiveVisible ? undefined : SENSITIVE_MASK}
                size='sm'
              />
            ))}
          </div>
        </div>
      </ChannelRowActionsLayoutContext.Provider>
    )
  }

  // In card view the enable/disable state is already conveyed by the inline
  // power toggle, so only the informative states (auto-disabled, unknown) keep
  // a badge.
  const showStatusBadge =
    channel.status !== CHANNEL_STATUS.ENABLED &&
    channel.status !== CHANNEL_STATUS.MANUAL_DISABLED

  const models = parseModelsList(channel.models)
  const hiddenModels = models.slice(VISIBLE_MODEL_COUNT)
  const visibleGroups = groups.slice(0, VISIBLE_GROUP_COUNT)
  const hiddenGroups = groups.slice(VISIBLE_GROUP_COUNT)
  const testTime = channel.test_time
  const responseTime = channel.response_time
  const successRate =
    requestStat && requestStat.request_count > 0
      ? (requestStat.success_count / requestStat.request_count) * 100
      : null

  const statCells = requestStat
    ? [
        {
          label: t('Requests (24h)'),
          value: numberFormat.format(requestStat.request_count),
          tone: undefined,
        },
        {
          label: t('Success rate'),
          value: successRate === null ? '-' : `${successRate.toFixed(1)}%`,
          tone:
            successRate === null || successRate >= 99
              ? undefined
              : successRate >= 90
                ? 'text-warning'
                : 'text-destructive',
        },
        {
          label: t('Avg duration'),
          value:
            requestStat.avg_duration_ms > 0
              ? formatResponseTime(requestStat.avg_duration_ms, t)
              : '-',
          tone: undefined,
        },
        {
          label: t('Tokens'),
          value: compactFormat.format(requestStat.tokens),
          tone: undefined,
        },
      ]
    : []

  const healthTone = !routeHealth
    ? null
    : routeHealth.terminal > 0
      ? 'bg-destructive'
      : routeHealth.cooling > 0
        ? 'bg-warning'
        : routeHealth.slowStart > 0
          ? 'bg-info'
          : 'bg-success'

  const healthTitle = routeHealth
    ? [
        `${t('Healthy')}: ${routeHealth.healthy}`,
        `${t('Cooling')}: ${routeHealth.cooling}`,
        `${t('Slow start')}: ${routeHealth.slowStart}`,
        `${t('Terminal')}: ${routeHealth.terminal}`,
      ].join(' · ')
    : undefined

  return (
    <ChannelRowActionsLayoutContext.Provider value='card'>
      <div
        role='button'
        tabIndex={0}
        onClick={(event) => {
          if (event.defaultPrevented) return
          // Only the primary button opens the editor; right/middle clicks keep
          // the browser's own behavior (context menu, new tab).
          if (event.button !== 0) return
          // Inner controls (checkbox, action icons, chips) own their clicks;
          // only a click on the card surface opens the editor.
          const target = event.target as HTMLElement
          if (
            target.closest(
              'button, a, input, label, [role="checkbox"], [role="menuitem"]'
            )
          ) {
            return
          }
          setCurrentRow(channel)
          setOpen('update-channel')
        }}
        onKeyDown={(event) => {
          if (event.key !== 'Enter' && event.key !== ' ') return
          // Same guard as the click path: a focused inner control keeps its
          // own Enter/Space behavior instead of also opening the editor.
          const target = event.target as HTMLElement
          if (
            target.closest(
              'button, a, input, label, [role="checkbox"], [role="menuitem"]'
            )
          ) {
            return
          }
          event.preventDefault()
          setCurrentRow(channel)
          setOpen('update-channel')
        }}
        className='relative flex h-full cursor-pointer flex-col rounded-lg focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none'
      >
        {/* Header: type icon + name cell (name, remark, pass-through hints);
            icon actions float over the card's right edge so the content keeps
            the full width — upper rows reserve right padding for them */}
        <div className='flex items-start gap-2 pl-3 pr-16 pt-2.5'>
          {selectCell && (
            <span className='shrink-0 pt-0.5'>{selectCell}</span>
          )}
          <span className='bg-muted/60 flex size-7 shrink-0 items-center justify-center rounded-md'>
            {getLobeIcon(`${getChannelTypeIcon(channel.type)}.Color`, 16)}
          </span>
          <div className='min-w-0 flex-1 text-sm'>{nameCell}</div>
        </div>
        <div className='absolute right-2 top-2 z-10 flex flex-col items-end gap-0.5 [&>div]:ml-0 [&>div]:flex-col-reverse [&>div]:items-end [&>div]:gap-0.5 [&_button]:size-6 [&_button_svg]:size-3.5'>
          {actionsCell}
        </div>

        {/* Meta: id, type, informative status, groups (max 3 + overflow), tag,
            key count — everything that identifies or scopes the channel */}
        <div className='text-muted-foreground mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 pl-3 pr-12 text-[11px]'>
          <span className='shrink-0 font-mono tabular-nums'>
            #{sensitiveVisible ? channel.id : SENSITIVE_MASK}
          </span>
          <span className='truncate'>
            {t(getChannelTypeLabel(channel.type))}
          </span>
          {showStatusBadge && statusCell}
          {visibleGroups.map((group) => (
            <GroupBadge
              key={group}
              group={group}
              label={sensitiveVisible ? undefined : SENSITIVE_MASK}
              size='sm'
            />
          ))}
          {hiddenGroups.length > 0 && (
            <span
              title={sensitiveVisible ? hiddenGroups.join(', ') : undefined}
              className='shrink-0 tabular-nums'
            >
              +{hiddenGroups.length}
            </span>
          )}
          {tagCell}
          {channel.channel_info.is_multi_key &&
            channel.channel_info.multi_key_size > 0 && (
              <span className='shrink-0 whitespace-nowrap'>
                {t('Keys')}: {channel.channel_info.multi_key_size}
              </span>
            )}
        </div>

        {/* 24h request aggregate */}
        {requestStat && (
          <div className='ml-3 mr-12 mt-2 grid grid-cols-2 divide-x divide-y overflow-hidden rounded-md border bg-muted/20'>
            {statCells.map((cell) => (
              <div key={cell.label} className='min-w-0 px-2 py-1.5'>
                <div
                  className='text-muted-foreground/80 truncate text-[10px]'
                  title={cell.label}
                >
                  {cell.label}
                </div>
                <div
                  className={cn(
                    'truncate text-xs font-medium tabular-nums',
                    cell.tone
                  )}
                >
                  {cell.value}
                </div>
              </div>
            ))}
          </div>
        )}

        {/* Routing health + last test */}
        <div className='mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 pl-3 pr-12 text-[11px]'>
          {routeHealth && routeHealth.total > 0 && (
            <button
              type='button'
              title={healthTitle}
              onClick={(event) => {
                event.stopPropagation()
                setCurrentRow(channel)
                setOpen('route-health')
              }}
              className='hover:bg-muted -ml-1.5 flex items-center gap-1.5 rounded px-1.5 py-0.5 transition-colors'
            >
              <span
                className={cn(
                  'size-1.5 shrink-0 rounded-full',
                  healthTone
                )}
              />
              <span className='text-muted-foreground'>
                {t('Route health')}
              </span>
              <span className='font-medium tabular-nums'>
                {routeHealth.healthy}/{routeHealth.total}
              </span>
            </button>
          )}
          <span className='text-muted-foreground flex items-center gap-1'>
            <span>{t('Last Tested')}</span>
            <span className='text-foreground/80'>
              {testTime > 0 ? formatRelativeTime(testTime, locale) : t('Never')}
            </span>
          </span>
          {responseTime > 0 && (
            <StatusBadge
              label={formatResponseTime(responseTime, t)}
              variant={getResponseTimeConfig(responseTime).variant}
              size='sm'
              copyable={false}
            />
          )}
          {requestStat && requestStat.last_request_at > 0 && (
            <span
              className='text-muted-foreground flex items-center gap-1'
              title={t('Last request')}
            >
              <span>{t('Last request')}</span>
              <span className='text-foreground/80'>
                {formatRelativeTime(requestStat.last_request_at, locale)}
              </span>
            </span>
          )}
        </div>

        {/* Model list */}
        {models.length > 0 && (
          <div className='mt-1.5 flex flex-wrap items-center gap-1 pl-3 pr-12'>
            {models.slice(0, VISIBLE_MODEL_COUNT).map((model) => (
              <span
                key={model}
                title={model}
                className='bg-muted/60 max-w-32 truncate rounded px-1.5 py-0.5 font-mono text-[10px]'
              >
                {model}
              </span>
            ))}
            {hiddenModels.length > 0 && (
              <span
                title={hiddenModels.join(', ')}
                className='text-muted-foreground text-[10px] tabular-nums'
              >
                +{hiddenModels.length}
              </span>
            )}
          </div>
        )}

        {/* Footer: balance split by the shared cell (used left, remaining right,
            click the remaining value to refresh) */}
        <div className='mt-auto border-t px-3 py-1.5'>
          {renderCell('balance')}
        </div>
      </div>
    </ChannelRowActionsLayoutContext.Provider>
  )
}

/**
 * Memoized so each card only re-renders when its own react-table row reference
 * or aggregate payload changes, instead of every card re-rendering whenever the
 * parent table state (filters, pagination, sensitive toggle, etc.) updates.
 */
export const ChannelCard = memo(ChannelCardComponent)

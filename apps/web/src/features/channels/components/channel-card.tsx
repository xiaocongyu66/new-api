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
import { memo } from 'react'

import { GroupBadge } from '@/components/group-badge'

import { CHANNEL_STATUS } from '../constants'
import { isTagAggregateRow, parseGroupsList } from '../lib'
import type { Channel } from '../types'
import { ChannelRowActionsLayoutContext } from './channel-row-actions-context'
import { useChannels } from './channels-provider'

const SENSITIVE_MASK = '••••'

/**
 * Bespoke channel card for the card view. Reuses every column's existing cell
 * renderer via `flexRender`, so the table's information and interactions are
 * preserved: row selection, status (with tooltips), groups, tag
 * expand-collapse, and the per-row (or per-tag) actions menu.
 *
 * Two lines by design: the channel name with its id, then the groups.
 */
function ChannelCardComponent({
  row,
  isSelected,
}: {
  row: Row<Channel>
  isSelected: boolean
}) {
  const { sensitiveVisible } = useChannels()
  const isTagRow = isTagAggregateRow(row.original)
  const cells = row.getAllCells()

  const renderCell = (id: string) => {
    const cell = cells.find((c) => c.column.id === id)
    if (!cell || !cell.column.columnDef.cell) {
      return null
    }
    return flexRender(cell.column.columnDef.cell, cell.getContext())
  }

  const groups = parseGroupsList(row.original.group ?? '')

  const selectCell = renderCell('select')
  const nameCell = renderCell('name')
  const statusCell = renderCell('status')
  const actionsCell = renderCell('actions')

  const displayName = sensitiveVisible ? row.original.name : SENSITIVE_MASK

  // In card view the enable/disable state is already conveyed by the inline
  // power toggle, so the plain "Enabled"/"Disabled" badge is redundant. Keep
  // only the informative states (e.g. auto-disabled, unknown) and tag rows.
  const showStatusBadge =
    isTagRow ||
    (row.original.status !== CHANNEL_STATUS.ENABLED &&
      row.original.status !== CHANNEL_STATUS.MANUAL_DISABLED)

  return (
    <ChannelRowActionsLayoutContext.Provider value='card'>
      <div
        data-state={isSelected ? 'selected' : undefined}
        className='flex flex-col gap-1.5'
      >
        {/* Line 1: selection + channel name with the id trailing it, plus the
          status badge and actions menu. The action buttons are capped to the
          text row height so the control strip does not add vertical bulk. */}
        <div className='flex h-5 items-center justify-between gap-2'>
          <div className='flex min-w-0 flex-1 items-center gap-2'>
            {!isTagRow && selectCell && (
              <span className='shrink-0'>{selectCell}</span>
            )}
            {isTagRow ? (
              // Aggregate rows keep their expand control and channel count.
              <div className='min-w-0 flex-1 text-sm'>{nameCell}</div>
            ) : (
              <div className='flex min-w-0 flex-1 items-center gap-1.5 text-sm'>
                <span className='min-w-0 truncate font-medium'>
                  {displayName}
                </span>
                <span className='text-muted-foreground shrink-0 text-[11px] tabular-nums select-none'>
                  #{sensitiveVisible ? row.original.id : SENSITIVE_MASK}
                </span>
              </div>
            )}
          </div>
          <div className='-my-1 flex shrink-0 items-center gap-1.5 [&_button]:size-7'>
            {showStatusBadge && statusCell}
            {actionsCell}
          </div>
        </div>

        {/* Line 2: groups span the full width, showing every group (no label) */}
        <div className='min-w-0'>
          {groups.length > 0 ? (
            <div className='-ml-1.5 flex flex-wrap gap-1'>
              {groups.map((g) => (
                <GroupBadge
                  key={g}
                  group={g}
                  label={sensitiveVisible ? undefined : SENSITIVE_MASK}
                  size='sm'
                />
              ))}
            </div>
          ) : (
            <span className='text-muted-foreground text-sm'>-</span>
          )}
        </div>
      </div>
    </ChannelRowActionsLayoutContext.Provider>
  )
}

/**
 * Memoized so each card only re-renders when its own react-table row reference
 * changes, instead of every card re-rendering whenever the parent table state
 * (filters, pagination, sensitive toggle, etc.) updates.
 */
export const ChannelCard = memo(ChannelCardComponent)

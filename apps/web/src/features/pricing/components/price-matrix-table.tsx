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
import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

export type PriceMatrixCell = {
  /** Lines rendered stacked inside the cell (e.g. input over output). */
  lines: string[]
  muted?: boolean
}

export type PriceMatrixRow = {
  key: string
  /** Row label: context interval, group name, or `*` for flat pricing. */
  header: ReactNode
  cells: PriceMatrixCell[]
}

type PriceMatrixTableProps = {
  /** Top-left header cell describing the row labels (e.g. Interval / Group). */
  corner: ReactNode
  columns: ReactNode[]
  rows: PriceMatrixRow[]
  className?: string
  /** Optional click handler makes the whole table act as a toggle. */
  onToggle?: (event: React.MouseEvent) => void
  expanded?: boolean
  toggleLabel?: string
}

/**
 * Compact price matrix used by the model plaza card and the admin pricing
 * cards: columns are price lanes (request / input / output / cache), rows are
 * the dimensions that vary (context interval or group). The style mirrors the
 * plaza group-price table so both surfaces read the same.
 */
export function PriceMatrixTable({
  corner,
  columns,
  rows,
  className,
  onToggle,
  expanded,
  toggleLabel,
}: PriceMatrixTableProps) {
  if (rows.length === 0) return null
  return (
    <div
      className={cn(
        'border-border/50 no-scrollbar overflow-x-auto overflow-y-hidden rounded-lg border',
        onToggle && 'cursor-pointer',
        className
      )}
      onClick={onToggle}
      role={onToggle ? 'button' : undefined}
      aria-expanded={onToggle ? expanded : undefined}
      aria-label={onToggle ? toggleLabel : undefined}
      tabIndex={onToggle ? 0 : undefined}
      onKeyDown={
        onToggle
          ? (event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault()
                onToggle(event as unknown as React.MouseEvent)
              }
            }
          : undefined
      }
    >
      <div
        className='grid min-w-max items-center text-xs'
        style={{
          gridTemplateColumns: `minmax(4.5rem, auto) repeat(${columns.length}, auto)`,
        }}
      >
        <div className='contents'>
          <span className='bg-muted/30 text-muted-foreground/70 px-2 py-1 text-left text-[10px] font-medium whitespace-nowrap'>
            {corner}
          </span>
          {columns.map((column, index) => (
            <span
              key={index}
              className='bg-muted/30 text-muted-foreground/70 px-2 py-1 text-right text-[10px] font-medium whitespace-nowrap'
            >
              {column}
            </span>
          ))}
        </div>
        {rows.map((row) => (
          <div key={row.key} className='contents'>
            <span className='border-border/50 text-foreground/80 h-full truncate border-t px-2 py-1 text-left text-[11px] font-medium whitespace-nowrap'>
              {row.header}
            </span>
            {row.cells.map((cell, index) => (
              <span
                key={index}
                className={cn(
                  'border-border/50 flex h-full flex-col border-t px-2 py-1 text-right font-mono text-[11px] leading-tight whitespace-nowrap tabular-nums',
                  cell.muted ? 'text-muted-foreground' : 'text-foreground'
                )}
              >
                {cell.lines.length === 0
                  ? '—'
                  : cell.lines.map((line, lineIndex) => (
                      <span key={lineIndex}>{line}</span>
                    ))}
              </span>
            ))}
          </div>
        ))}
      </div>
    </div>
  )
}

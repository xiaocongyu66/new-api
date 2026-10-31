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
import { cn } from '@/lib/utils'

import type { DataTableColumnClassName, DataTablePinnedColumn } from './types'

export function getResolvedColumnClassNameFromMap(
  getColumnClassName?: DataTableColumnClassName,
  pinnedColumnById?: Map<string, DataTablePinnedColumn>
): DataTableColumnClassName {
  return (columnId, kind) => {
    const customClassName = getColumnClassName?.(columnId, kind)
    const pinnedColumn = pinnedColumnById?.get(columnId)

    if (!pinnedColumn) {
      return customClassName
    }

    return cn(customClassName, getPinnedColumnClassName(pinnedColumn, kind))
  }
}

export function getPinnedColumnMap(pinnedColumns?: DataTablePinnedColumn[]) {
  if (!pinnedColumns?.length) {
    return undefined
  }

  return new Map(pinnedColumns.map((column) => [column.columnId, column]))
}

function getPinnedColumnClassName(
  pinnedColumn: DataTablePinnedColumn,
  kind: 'header' | 'cell'
) {
  const edgeClassName =
    pinnedColumn.side === 'left'
      ? 'shadow-[8px_0_10px_-10px_hsl(var(--foreground))]'
      : 'shadow-[-8px_0_10px_-10px_var(--foreground)]'

  return cn(
    'sticky whitespace-nowrap',
    pinnedColumn.side === 'left' ? 'left-0' : 'right-0',
    edgeClassName,
    // The pinned strip exists to keep its column reachable while the rest
    // scrolls underneath it, so it has to hide what passes behind. Both the row
    // band and the header band are far too transparent for that job (the
    // neighbouring role/invite columns stayed readable next to the action
    // icons), and a backdrop blur cannot help: a pinned cell only covers its own
    // row's siblings, which Chromium leaves out of its backdrop. So the strip
    // uses the opaque variants of both bands, hover and selected states
    // included - opaque means no backdrop blur is needed (and none is added:
    // `backdrop-filter` would also make the cell a containing block for any
    // absolutely positioned descendant).
    //
    // The header cell deliberately ignores the `--table-header-bg` hook that
    // split-header tables use to thin their band: the pinned strip has to
    // occlude, and that hook resolves to a 12% veil once the background image is
    // up, which would leave the header columns sliding under it readable.
    kind === 'header'
      ? '[background-color:var(--table-header-solid)] group-hover:[background-color:var(--table-header-hover-solid)] z-30'
      : '[background-color:var(--table-pinned)] z-10 group-hover:[background-color:color-mix(in_oklch,var(--muted-solid)_50%,var(--background))] group-data-[state=selected]:[background-color:var(--muted-solid)]',
    pinnedColumn.className,
    kind === 'header'
      ? pinnedColumn.headerClassName
      : pinnedColumn.cellClassName
  )
}

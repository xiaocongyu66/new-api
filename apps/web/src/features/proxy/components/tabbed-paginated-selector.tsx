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
import { Check, ChevronLeft, ChevronRight, Plus } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

export type TabbedItem = {
  id: string
  label: string
  sublabel?: string
}

export type TabCategory = {
  id: string
  label: string
  items: TabbedItem[]
}

type TabbedPaginatedSelectorProps = {
  categories: TabCategory[]
  selectedIds: Set<string>
  onToggle: (id: string) => void
  pageSize?: number
  className?: string
}

export function TabbedPaginatedSelector({
  categories,
  selectedIds,
  onToggle,
  pageSize = 30, // 默认单页容量充足，排满 3 行以上再分页
  className,
}: TabbedPaginatedSelectorProps) {
  const { t } = useTranslation()
  const [activeTab, setActiveTab] = useState<string>(
    () => categories[0]?.id ?? ''
  )
  const [page, setPage] = useState<number>(1)

  const activeCategory = useMemo(() => {
    return (
      categories.find((category) => category.id === activeTab) ??
      categories[0] ?? { id: '', label: '', items: [] }
    )
  }, [categories, activeTab])

  const totalPages = Math.max(
    1,
    Math.ceil(activeCategory.items.length / pageSize)
  )
  const safePage = Math.min(Math.max(1, page), totalPages)

  const visibleItems = useMemo(() => {
    const start = (safePage - 1) * pageSize
    return activeCategory.items.slice(start, start + pageSize)
  }, [activeCategory.items, safePage, pageSize])

  const handleTabChange = (tabId: string) => {
    setActiveTab(tabId)
    setPage(1)
  }

  // Generate pagination page numbers (smart 3-page visible rule)
  const pageNumbers = useMemo(() => {
    if (totalPages <= 3) {
      return Array.from({ length: totalPages }, (_, i) => i + 1)
    }
    if (safePage === 1) {
      return [1, 2, 3]
    }
    if (safePage === totalPages) {
      return [totalPages - 2, totalPages - 1, totalPages]
    }
    return [safePage - 1, safePage, safePage + 1]
  }, [totalPages, safePage])

  const selectedCount = activeCategory.items.filter((item) =>
    selectedIds.has(item.id)
  ).length

  return (
    <div
      className={cn(
        'flex flex-col rounded-lg border bg-background/50 overflow-hidden',
        className
      )}
    >
      {/* 顶层第一行：左侧两个 Tab (渠道 / 模型别名)，右侧分页与跳转按钮 */}
      <header className='flex items-center justify-between border-b px-3 py-2 bg-muted/20 gap-2 flex-wrap'>
        {/* 左侧 Tabs */}
        <div className='flex items-center gap-1.5'>
          {categories.map((category) => {
            const isActive = category.id === (activeTab || categories[0]?.id)
            return (
              <button
                key={category.id}
                type='button'
                onClick={() => handleTabChange(category.id)}
                className={cn(
                  'inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-semibold transition-all',
                  isActive
                    ? 'bg-background text-foreground shadow-2xs border border-border/80'
                    : 'text-muted-foreground hover:text-foreground hover:bg-muted/40'
                )}
              >
                <span>{category.label}</span>
                <span className='rounded-full bg-muted px-1.5 py-0.2 text-[10px] font-medium text-muted-foreground'>
                  {category.items.length}
                </span>
              </button>
            )
          })}
        </div>

        {/* 右侧分页组件：仅当实际有多页 (totalPages > 1) 时完整展示分页器，单页时不展示多余的翻页按钮 */}
        <div className='flex items-center gap-1 text-xs'>
          <span className='text-[11px] text-muted-foreground mr-1.5'>
            {t('Selected')}: {selectedCount}
          </span>
          {totalPages > 1 && (
            <>
              <button
                type='button'
                disabled={safePage <= 1}
                onClick={() => setPage((p) => Math.max(1, p - 1))}
                className='inline-flex size-6 items-center justify-center rounded-md border border-border/60 bg-background text-muted-foreground hover:text-foreground hover:border-border disabled:opacity-30 disabled:pointer-events-none transition-colors'
                title={t('Previous Page')}
              >
                <ChevronLeft className='size-3.5' />
              </button>

              <div className='flex items-center gap-1'>
                {pageNumbers.map((p) => {
                  const isCurrent = p === safePage
                  return (
                    <button
                      key={p}
                      type='button'
                      onClick={() => setPage(p)}
                      className={cn(
                        'inline-flex size-6 items-center justify-center rounded-md text-xs font-medium transition-colors border',
                        isCurrent
                          ? 'border-primary bg-primary text-primary-foreground font-semibold shadow-2xs'
                          : 'border-border/60 bg-background text-muted-foreground hover:text-foreground hover:border-border'
                      )}
                    >
                      {p}
                    </button>
                  )
                })}
              </div>

              <button
                type='button'
                disabled={safePage >= totalPages}
                onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                className='inline-flex size-6 items-center justify-center rounded-md border border-border/60 bg-background text-muted-foreground hover:text-foreground hover:border-border disabled:opacity-30 disabled:pointer-events-none transition-colors'
                title={t('Next Page')}
              >
                <ChevronRight className='size-3.5' />
              </button>
            </>
          )}
        </div>
      </header>

      {/* 第二行主体：固定最小高度面板，防止换页时因条目少而高度塌陷抖动 */}
      <div className='p-3 min-h-[148px] overflow-y-auto'>
        <div className='flex flex-wrap gap-2 content-start'>
          {visibleItems.length === 0 ? (
            <p className='text-xs text-muted-foreground py-4 px-1'>
              {t('No items available')}
            </p>
          ) : (
            visibleItems.map((item) => {
              const isSelected = selectedIds.has(item.id)
              return (
                <button
                  key={item.id}
                  type='button'
                  onClick={() => onToggle(item.id)}
                  title={item.sublabel ? `${item.label} (${item.sublabel})` : item.label}
                  className={cn(
                    'group inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium transition-all shadow-2xs border',
                    isSelected
                      ? 'border-primary bg-primary text-primary-foreground shadow-xs ring-1 ring-primary/30'
                      : 'border-border bg-background hover:bg-accent/60 hover:border-accent-foreground/20 text-muted-foreground hover:text-foreground'
                  )}
                >
                  {isSelected ? (
                    <Check className='size-3.5 shrink-0 stroke-[2.5]' />
                  ) : (
                    <Plus className='size-3.5 shrink-0 text-muted-foreground/60 group-hover:text-foreground' />
                  )}
                  <span className='truncate'>{item.label}</span>
                  {item.sublabel && (
                    <span
                      className={cn(
                        'text-[10px] font-mono px-1 py-0.2 rounded-xs',
                        isSelected
                          ? 'bg-primary-foreground/20 text-primary-foreground'
                          : 'bg-muted text-muted-foreground'
                      )}
                    >
                      {item.sublabel}
                    </span>
                  )}
                </button>
              )
            })
          )}
        </div>
      </div>
    </div>
  )
}

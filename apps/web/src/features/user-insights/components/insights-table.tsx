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
import { getRouteApi } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'

import { useUserInsights } from '../hooks/use-user-insights'
import type { UserInsight, UserInsightFilters } from '../types'
import { useInsightColumns } from './insight-columns'

const route = getRouteApi('/_authenticated/user-insights/')

/** 排序选项。后端在内存里排序，字段与 sortInsightViews 的 case 一致。 */
const SORT_OPTIONS: {
  value: NonNullable<UserInsightFilters['sort']>
  labelKey: string
}[] = [
  { value: 'last_seen', labelKey: 'Last active' },
  { value: 'requests', labelKey: 'Request count' },
  { value: 'risk', labelKey: 'Risk score' },
  { value: 'code', labelKey: 'Coding volume' },
  { value: 'roleplay', labelKey: 'Roleplay volume' },
]

const CATEGORY_OPTIONS = [
  { value: 'code', labelKey: 'Coding' },
  { value: 'roleplay', labelKey: 'Roleplay' },
  { value: 'qa', labelKey: 'Q&A' },
  { value: 'translate', labelKey: 'Translation' },
  { value: 'other', labelKey: 'Other' },
]

const RISK_OPTIONS = [
  { value: 'confirmed', labelKey: 'Confirmed jailbreak' },
  { value: 'likely', labelKey: 'Likely jailbreak' },
  { value: 'suspect', labelKey: 'Suspected jailbreak' },
  { value: 'none', labelKey: 'Clean' },
]

type InsightsTableProps = {
  onViewEvidence: (item: UserInsight) => void
}

/**
 * 画像列表。
 *
 * 与 users / usage-logs 走同一套表格栈（DataTablePage + useDataTable +
 * useTableUrlState），因此筛选、排序、分页、列显隐、移动端卡片布局
 * 的外观与交互全站一致——之前这个页面是手写表格加手写翻页按钮，
 * 才显得和其它页面不是一个产品。
 *
 * 服务端分页与筛选：manualPagination / manualFiltering / manualSorting
 * 全开，列上的 filterFn 只用于 faceted filter 的取值语义，实际过滤
 * 在后端完成（派生字段用 SQL 表达会牺牲跨库兼容性）。
 */
export function InsightsTable({ onViewEvidence }: InsightsTableProps) {
  const { t } = useTranslation()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const columns = useInsightColumns({ onViewEvidence })

  const {
    globalFilter,
    onGlobalFilterChange,
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: { defaultPage: 1, defaultPageSize: isMobile ? 10 : 20 },
    globalFilter: { enabled: true, key: 'filter' },
    columnFilters: [
      { columnId: 'primary_category', searchKey: 'category', type: 'array' },
      { columnId: 'risk_level', searchKey: 'risk', type: 'array' },
      { columnId: 'clients', searchKey: 'relay', type: 'array' },
    ],
  })

  const search = route.useSearch()
  const navigate = route.useNavigate()

  const categoryFilter =
    (columnFilters.find((f) => f.id === 'primary_category')?.value as
      | string[]
      | undefined) ?? []
  const riskFilter =
    (columnFilters.find((f) => f.id === 'risk_level')?.value as
      | string[]
      | undefined) ?? []
  const relayFilter =
    (columnFilters.find((f) => f.id === 'clients')?.value as
      | string[]
      | undefined) ?? []

  // 排序放在 URL 的独立参数里而不是 TanStack 的 sorting state：
  // 可排序维度是后端定义的五个派生指标，不与具体列一一对应
  // （"风险分"排序对应的是 jailbreak_max_score，不是 risk_level 列）。
  const sort = (search as { sort?: UserInsightFilters['sort'] }).sort ?? 'last_seen'

  const filters: UserInsightFilters = {
    category: (categoryFilter[0] ?? '') as UserInsightFilters['category'],
    risk: (riskFilter[0] ?? '') as UserInsightFilters['risk'],
    relayOnly: relayFilter.includes('relay'),
    sort,
    keyword: globalFilter,
  }

  const insightsQuery = useUserInsights(
    pagination.pageIndex + 1,
    pagination.pageSize,
    filters
  )

  const items = insightsQuery.data?.data.items ?? []
  const total = insightsQuery.data?.data.total ?? 0

  const { table } = useDataTable({
    data: items,
    columns,
    columnFilters,
    globalFilter,
    pagination,
    columnVisibilityStorageKey: 'user-insights:column-visibility',
    enableRowSelection: false,
    onPaginationChange,
    onGlobalFilterChange,
    onColumnFiltersChange,
    manualPagination: true,
    manualFiltering: true,
    manualSorting: true,
    totalCount: total,
    ensurePageInRange,
  })

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={insightsQuery.isLoading}
      isFetching={insightsQuery.isFetching}
      emptyTitle={t('No profiled users yet')}
      emptyDescription={t(
        'Profiles are built from relayed requests. They will appear here once traffic comes in.'
      )}
      skeletonKeyPrefix='user-insight-skeleton'
      applyHeaderSize
      mobileProps={{ getRowKey: (row) => row.original.user_id }}
      toolbarProps={{
        searchPlaceholder: t('Filter by username or ID...'),
        searchDebounceMs: 500,
        filters: [
          {
            columnId: 'primary_category',
            title: t('Primary use'),
            options: CATEGORY_OPTIONS.map((option) => ({
              value: option.value,
              label: t(option.labelKey),
            })),
            singleSelect: true,
          },
          {
            columnId: 'risk_level',
            title: t('Risk'),
            options: RISK_OPTIONS.map((option) => ({
              value: option.value,
              label: t(option.labelKey),
            })),
            singleSelect: true,
          },
          {
            columnId: 'clients',
            title: t('Source'),
            options: [{ value: 'relay', label: t('Relay traffic only') }],
            singleSelect: true,
          },
        ],
        // 排序做成一组小按钮而不是下拉：只有五个选项，且管理员经常
        // 在"风险分"与"最近活跃"之间来回切，少一次展开更顺手。
        additionalSearch: (
          <div className='flex flex-wrap items-center gap-1'>
            {SORT_OPTIONS.map((option) => (
              <Button
                key={option.value}
                variant={sort === option.value ? 'default' : 'outline'}
                size='sm'
                className='h-8'
                onClick={() =>
                  navigate({
                    search: (prev) => ({
                      ...prev,
                      page: undefined,
                      sort:
                        option.value === 'last_seen' ? undefined : option.value,
                    }),
                  })
                }
              >
                {t(option.labelKey)}
              </Button>
            ))}
          </div>
        ),
        hasAdditionalFilters: sort !== 'last_seen',
        onReset: () =>
          navigate({
            search: (prev) => ({ ...prev, sort: undefined, page: undefined }),
          }),
      }}
    />
  )
}
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
import { useQuery } from '@tanstack/react-query'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { getPerfMetricsSummary } from '@/features/performance-metrics/api'

import { DEFAULT_PRICING_PAGE_SIZE, DEFAULT_TOKEN_UNIT } from '../constants'
import type { ModelPerfBadgeData, PricingModel, TokenUnit } from '../types'
import { ModelCard } from './model-card'

export interface ModelCardGridProps {
  models: PricingModel[]
  onModelClick: (modelName: string) => void
  priceRate?: number
  usdExchangeRate?: number
  tokenUnit?: TokenUnit
  showRechargePrice?: boolean
  selectedGroup?: string
}

export function ModelCardGrid(props: ModelCardGridProps) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const pageSize = DEFAULT_PRICING_PAGE_SIZE
  const tokenUnit = props.tokenUnit ?? DEFAULT_TOKEN_UNIT
  const totalPages = Math.max(1, Math.ceil(props.models.length / pageSize))
  const currentPage = Math.min(page, totalPages)

  const perfQuery = useQuery({
    queryKey: ['perf-metrics-summary', 24],
    queryFn: () => getPerfMetricsSummary(24),
    staleTime: 60 * 1000,
    retry: false,
  })

  const pagedModels = useMemo(() => {
    const start = (currentPage - 1) * pageSize
    return props.models.slice(start, start + pageSize)
  }, [currentPage, pageSize, props.models])

  const perfMap = useMemo(() => {
    const map = new Map<string, ModelPerfBadgeData>()
    for (const model of perfQuery.data?.data?.models ?? []) {
      map.set(model.model_name, model)
    }
    return map
  }, [perfQuery.data])

  if (props.models.length === 0) {
    return null
  }

  return (
    <div className='@container space-y-4 sm:space-y-5'>
      {/* Column count tracks the grid's own width, not the viewport: a
          sidebar/icon-rail steals layout width without moving viewport
          breakpoints, so viewport queries misjudge the available room.
          1 (mobile) → 2 (≥640) → 3 (≥1024) → 4 (≥1440, 1080p + sidebar)
          → 5 (≥1920, 2K+ displays). */}
      <div className='grid grid-cols-1 gap-3 @min-[640px]:grid-cols-2 @min-[640px]:gap-4 @min-[1024px]:grid-cols-3 @min-[1440px]:grid-cols-4 @min-[1920px]:grid-cols-5'>
        {pagedModels.map((model) => (
          <ModelCard
            key={model.id ?? model.model_name}
            model={model}
            tokenUnit={tokenUnit}
            priceRate={props.priceRate}
            usdExchangeRate={props.usdExchangeRate}
            showRechargePrice={props.showRechargePrice}
            selectedGroup={props.selectedGroup}
            perf={perfMap.get(model.model_name || '')}
            onClick={() => props.onModelClick(model.model_name || '')}
          />
        ))}
      </div>

      {totalPages > 1 && (
        <nav aria-label={t('Pagination')} className='text-muted-foreground flex flex-col items-center justify-between gap-3 border-t px-4 py-3 text-sm sm:flex-row'>
          <p className='text-muted-foreground'>
            {t('Page {{current}} of {{total}}', {
              current: currentPage,
              total: totalPages,
            })}
          </p>
          <div className='flex items-center gap-2'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => setPage((current) => Math.max(1, current - 1))}
              disabled={currentPage <= 1}
              className='gap-1.5'
            >
              <ChevronLeft className='size-4' />
              {t('Previous page')}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() =>
                setPage((current) => Math.min(totalPages, current + 1))
              }
              disabled={currentPage >= totalPages}
              className='gap-1.5'
            >
              {t('Next page')}
              <ChevronRight className='size-4' />
            </Button>
          </div>
        </nav>
      )}
    </div>
  )
}

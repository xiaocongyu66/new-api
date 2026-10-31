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
import { ArrowUp, ChevronLeft, ChevronRight } from 'lucide-react'
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { getPerfMetricsSummary } from '@/features/performance-metrics/api'

import { DEFAULT_PRICING_PAGE_SIZE, DEFAULT_TOKEN_UNIT } from '../constants'
import type { PricingModel, TokenUnit } from '../types'
import type { ModelPerfBadgeData } from './model-perf-badge'
import { ModelCard } from './model-card'

// Every masonry item spans as many of these virtual grid rows as its measured
// height needs; the +gap padding baked into the span becomes the vertical gap.
const MASONRY_ROW_HEIGHT = 2

/**
 * One masonry cell. The grid measures its height and assigns a row span; this
 * component additionally watches the cell's viewport visibility so the card's
 * numbers only start counting up once the card is (nearly) on screen, instead
 * of burning through the animation somewhere below the fold.
 */
function MasonryItem(props: {
  span?: number
  elementRef: (element: HTMLDivElement | null) => void
  itemKey: string
  onCardHeight?: (key: string, height: number) => void
  model: PricingModel
  tokenUnit: TokenUnit
  priceRate?: number
  usdExchangeRate?: number
  showRechargePrice?: boolean
  selectedGroup?: string
  perf?: ModelPerfBadgeData
  onClick: () => void
}) {
  // Environments without IntersectionObserver (tests, old webviews) render
  // the card as immediately visible rather than stuck at zero.
  const [visible, setVisible] = useState(
    () => typeof IntersectionObserver === 'undefined'
  )
  const nodeRef = useRef<HTMLDivElement | null>(null)
  const cardRef = useRef<HTMLDivElement | null>(null)

  // Self-managed ResizeObserver: observe this item's own card node and report
  // height changes to the grid so spans are recomputed per card lifecycle.
  // Cleanup disconnects on unmount, so detached nodes are never observed.
  useEffect(() => {
    const card = cardRef.current
    if (!card || !props.onCardHeight) return
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const height = entry.contentRect.height
        if (height > 0) {
          props.onCardHeight(props.itemKey, height)
        }
      }
    })
    observer.observe(card)
    return () => observer.disconnect()
  }, [props.itemKey, props.onCardHeight])

  useEffect(() => {
    if (typeof IntersectionObserver === 'undefined') {
      setVisible(true)
      return
    }
    const node = nodeRef.current
    if (!node) {
      return
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          setVisible(true)
          observer.disconnect()
        }
      },
      // Start slightly before the card enters the viewport so the count-up
      // finishes about when the card becomes readable.
      { rootMargin: '160px 0px' }
    )
    observer.observe(node)
    return () => observer.disconnect()
  }, [])

  return (
    <div
      ref={(element) => {
        nodeRef.current = element
        props.elementRef(element)
      }}
      style={props.span ? { gridRowEnd: `span ${props.span}` } : undefined}
    >
      <div ref={cardRef}>
        <ModelCard
          model={props.model}
          tokenUnit={props.tokenUnit}
          priceRate={props.priceRate}
          usdExchangeRate={props.usdExchangeRate}
          showRechargePrice={props.showRechargePrice}
          selectedGroup={props.selectedGroup}
          perf={props.perf}
          visible={visible}
          onClick={props.onClick}
        />
      </div>
    </div>
  )
}

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

  // Masonry via measured grid-row spans: each wrapper spans just enough 2px
  // rows to cover its card plus the column gap. The browser then auto-places
  // every next card into the shortest column, which is what produces the
  // staggered, organic packing that CSS columns cannot (columns balance their
  // heights and fill top-to-bottom in source order).
  //
  // Measuring the CARD, not the wrapper, is deliberate: the wrapper's height
  // is the span itself, so observing it would feed the layout back into the
  // measurement. The ResizeObserver also keeps the layout correct when a card
  // grows or shrinks after mount — e.g. a collapsible group table.
  const gridRef = useRef<HTMLDivElement>(null)
  const itemRefs = useRef<Record<string, HTMLDivElement>>({})
  const [cardHeights, setCardHeights] = useState<Record<string, number>>({})
  const [gap, setGap] = useState(16)

  // Reported by each MasonryItem's self-managed ResizeObserver when its card's
  // measured height changes. Only update when the delta exceeds one virtual
  // row (2px) so sub-pixel measurement jitter doesn't trigger span churn or
  // re-render loops (F-4).
  const handleCardHeight = useCallback((key: string, height: number) => {
    setCardHeights((previous) => {
      const oldHeight = previous[key]
      if (
        oldHeight !== undefined &&
        Math.abs(height - oldHeight) <= MASONRY_ROW_HEIGHT
      ) {
        return previous
      }
      return { ...previous, [key]: height }
    })
  }, [])

  const spans = useMemo(() => {
    const result: Record<string, number> = {}
    for (const [key, height] of Object.entries(cardHeights)) {
      result[key] = Math.max(
        1,
        Math.ceil((height + gap) / MASONRY_ROW_HEIGHT)
      )
    }
    return result
  }, [cardHeights, gap])

  useLayoutEffect(() => {
    const grid = gridRef.current
    if (!grid) return

    const initialGap =
      Number.parseFloat(getComputedStyle(grid).columnGap) || 16
    setGap(initialGap)

    // Grid-width ResizeObserver only: column count / gap changes at breakpoints
    // invalidate spans because the gap term in the span formula changes.
    const gridObserver = new ResizeObserver(() => {
      const newGap =
        Number.parseFloat(getComputedStyle(grid).columnGap) || 16
      setGap((current) => (current === newGap ? current : newGap))
    })
    gridObserver.observe(grid)

    return () => {
      gridObserver.disconnect()
    }
  }, [])

  // FLIP: when the masonry re-places cards (a group table expands, the sort
  // changes, a breakpoint flips), every card animates from its previous slot
  // to the new one instead of teleporting. Positions come from
  // offsetLeft/offsetTop, which ignore an in-flight transform, so a second
  // reflow mid-animation still measures the true layout slot.
  const previousPositions = useRef<Record<string, { x: number; y: number }>>({})

  useLayoutEffect(() => {
    const wrappers = Object.entries(itemRefs.current)
    const nextPositions: Record<string, { x: number; y: number }> = {}
    wrappers.forEach(([key, wrapper]) => {
      nextPositions[key] = { x: wrapper.offsetLeft, y: wrapper.offsetTop }
    })

    const reducedMotion = window.matchMedia(
      '(prefers-reduced-motion: reduce)'
    ).matches
    if (!reducedMotion) {
      wrappers.forEach(([key, wrapper]) => {
        const previous = previousPositions.current[key]
        if (!previous) return
        const deltaX = previous.x - nextPositions[key].x
        const deltaY = previous.y - nextPositions[key].y
        if (Math.abs(deltaX) < 1 && Math.abs(deltaY) < 1) return
        wrapper.style.transition = 'none'
        wrapper.style.transform = `translate(${deltaX}px, ${deltaY}px)`
        requestAnimationFrame(() => {
          wrapper.style.transition =
            'transform 420ms cubic-bezier(0.22, 1, 0.36, 1)'
          wrapper.style.transform = ''
        })
      })
    }

    previousPositions.current = nextPositions
  }, [spans, pagedModels])

  if (props.models.length === 0) {
    return null
  }

  return (
    <div className='@container space-y-4 sm:space-y-5'>
      {/* Masonry grid: the column count follows the grid's own width (a
          sidebar steals layout width without moving viewport breakpoints):
          1 (mobile) → 2 (≥640) → 3 (≥1024) → 4 (≥1440, 1080p + sidebar)
          → 5 (≥1920, 2K+ displays). Each item's row span is measured from
          its card height, so cards keep their content height and the browser
          packs every next card into the shortest column. */}
      <div
        ref={gridRef}
        className='grid grid-cols-1 items-start gap-x-3 @min-[640px]:grid-cols-2 @min-[640px]:gap-x-4 @min-[1024px]:grid-cols-3 @min-[1440px]:grid-cols-4 @min-[1920px]:grid-cols-5'
        style={{ gridAutoRows: `${MASONRY_ROW_HEIGHT}px` }}
      >
        {pagedModels.map((model) => {
          const key = String(model.id ?? model.model_name)
          return (
            <MasonryItem
              key={key}
              itemKey={key}
              span={spans[key]}
              onCardHeight={handleCardHeight}
              elementRef={(element) => {
                if (element) {
                  itemRefs.current[key] = element
                } else {
                  delete itemRefs.current[key]
                }
              }}
              model={model}
              tokenUnit={tokenUnit}
              priceRate={props.priceRate}
              usdExchangeRate={props.usdExchangeRate}
              showRechargePrice={props.showRechargePrice}
              selectedGroup={props.selectedGroup}
              perf={perfMap.get(model.model_name || '')}
              onClick={() => props.onModelClick(model.model_name || '')}
            />
          )
        })}
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

      {/* Floating page controls: always available at the bottom-right, so a
          long masonry page never forces a scroll back down to paginate. */}
      <div className='fixed right-4 bottom-4 z-40 flex flex-col gap-2 sm:right-6 sm:bottom-6'>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                type='button'
                variant='outline'
                size='icon'
                className='size-8 rounded-full shadow-md sm:size-9'
                aria-label={t('Back to top')}
                onClick={() =>
                  window.scrollTo({ top: 0, behavior: 'smooth' })
                }
              />
            }
          >
            <ArrowUp className='size-4' />
          </TooltipTrigger>
          <TooltipContent side='left' className='text-xs'>
            {t('Back to top')}
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                type='button'
                variant='outline'
                size='icon'
                className='size-8 rounded-full shadow-md sm:size-9'
                aria-label={t('Previous page')}
                disabled={currentPage <= 1}
                onClick={() => {
                  setPage((current) => Math.max(1, current - 1))
                  gridRef.current?.scrollIntoView({
                    behavior: 'smooth',
                    block: 'start',
                  })
                }}
              />
            }
          >
            <ChevronLeft className='size-4' />
          </TooltipTrigger>
          <TooltipContent side='left' className='text-xs'>
            {t('Previous page')}
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                type='button'
                variant='outline'
                size='icon'
                className='size-8 rounded-full shadow-md sm:size-9'
                aria-label={t('Next page')}
                disabled={currentPage >= totalPages}
                onClick={() => {
                  setPage((current) => Math.min(totalPages, current + 1))
                  gridRef.current?.scrollIntoView({
                    behavior: 'smooth',
                    block: 'start',
                  })
                }}
              />
            }
          >
            <ChevronRight className='size-4' />
          </TooltipTrigger>
          <TooltipContent side='left' className='text-xs'>
            {t('Next page')}
          </TooltipContent>
        </Tooltip>
      </div>
    </div>
  )
}

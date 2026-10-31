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
import { Check, ChevronDown, Copy } from 'lucide-react'
import { memo, useEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { CountUp } from '@/components/count-up'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'

import { DEFAULT_TOKEN_UNIT } from '../constants'
import {
  getDynamicDisplayGroupRatio,
  getDynamicPricingSummary,
} from '../lib/dynamic-price'
import { getConfiguredGroupRatio, isTokenBasedModel } from '../lib/model-helpers'
import {
  formatFixedPrice,
  formatGroupPrice,
  formatPrice,
  formatRequestPrice,
} from '../lib/price'
import type { PricingModel, TokenUnit } from '../types'
import type { ModelPerfBadgeData } from './model-perf-badge'
import { ModelCardMetrics } from './model-card-metrics'

export interface ModelCardProps {
  model: PricingModel
  onClick: () => void
  priceRate?: number
  usdExchangeRate?: number
  tokenUnit?: TokenUnit
  showRechargePrice?: boolean
  selectedGroup?: string
  perf?: ModelPerfBadgeData
  /** False until the card scrolls near the viewport; gates number count-up. */
  visible?: boolean
}

export const ModelCard = memo(function ModelCard(props: ModelCardProps) {
  const { t } = useTranslation()
  const { copyToClipboard } = useCopyToClipboard()
  const tokenUnit = props.tokenUnit ?? DEFAULT_TOKEN_UNIT
  const priceRate = props.priceRate ?? 1
  const usdExchangeRate = props.usdExchangeRate ?? 1
  const showRechargePrice = props.showRechargePrice ?? false
  const inView = props.visible ?? true
  const isTokenBased = isTokenBasedModel(props.model)
  const tokenUnitLabel = tokenUnit === 'K' ? '1K' : '1M'
  const groups = props.model.enable_groups || []
  const endpoints = props.model.supported_endpoint_types || []
  const modelIconKey = props.model.icon || props.model.vendor_icon
  const modelIcon = modelIconKey ? getLobeIcon(modelIconKey, 28) : null
  const initial = props.model.model_name?.charAt(0).toUpperCase() || '?'
  const isDynamicPricing =
    props.model.billing_mode === 'tiered_expr' &&
    Boolean(props.model.billing_expr)
  const dynamicSummary = isDynamicPricing
    ? getDynamicPricingSummary(props.model, {
        tokenUnit,
        showRechargePrice,
        priceRate,
        usdExchangeRate,
        groupRatioMultiplier: getDynamicDisplayGroupRatio(
          props.model,
          props.selectedGroup
        ),
      })
    : null

  // Price table rows: the first two enabled groups with their ratio and the
  // per-group prices. Dynamic-expression models have no stable per-group
  // numbers to tabulate, so they keep the summary row only.
  const groupRows = isDynamicPricing
    ? []
    : groups.map((group) => ({
        group,
        ratio: getConfiguredGroupRatio(props.model.group_ratio || {}, group),
        input: formatGroupPrice(
          props.model,
          group,
          'input',
          tokenUnit,
          showRechargePrice,
          priceRate,
          usdExchangeRate,
          props.model.group_ratio || {}
        ),
        output: formatGroupPrice(
          props.model,
          group,
          'output',
          tokenUnit,
          showRechargePrice,
          priceRate,
          usdExchangeRate,
          props.model.group_ratio || {}
        ),
        price: formatFixedPrice(
          props.model,
          group,
          showRechargePrice,
          priceRate,
          usdExchangeRate,
          props.model.group_ratio || {}
        ),
      }))

  // Copy feedback: the title button swaps its icon to a check for a moment
  // after a successful copy, then reverts. Local to this card.
  const [copied, setCopied] = useState(false)
  // Group table: collapsed shows the header plus the first two groups; a
  // click/Enter on the table expands it to every enabled group.
  const [tableExpanded, setTableExpanded] = useState(false)
  const copyResetRef = useRef<number | undefined>(undefined)
  useEffect(() => {
    return () => clearTimeout(copyResetRef.current)
  }, [])

  const handleCopy = async (e: React.MouseEvent) => {
    // The card itself is the details trigger, so the copy button must not
    // also fire the click that opens the details drawer.
    e.stopPropagation()
    const ok = await copyToClipboard(props.model.model_name || '')
    if (!ok) {
      return
    }
    setCopied(true)
    clearTimeout(copyResetRef.current)
    copyResetRef.current = window.setTimeout(() => setCopied(false), 1500)
  }

  const handleCardKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    // A div with role="button" needs each key that a native button accepts
    // to be wired explicitly; without this the card is keyboard-dead while
    // it still looks clickable. Keys pressed on a child control (the copy
    // button) keep their own meaning, so only the card's own focus counts.
    if (e.target !== e.currentTarget) {
      return
    }
    if (e.key !== 'Enter' && e.key !== ' ') {
      return
    }
    e.preventDefault()
    props.onClick()
  }

  let priceSummary: ReactNode
  if (dynamicSummary) {
    if (dynamicSummary.isSpecialExpression) {
      priceSummary = (
        <span className='min-w-0'>
          <span className='text-foreground font-medium'>
            {t('Special billing expression')}
          </span>
          <code className='text-muted-foreground/70 mt-0.5 line-clamp-1 block font-mono text-[11px] break-all'>
            {dynamicSummary.rawExpression}
          </code>
        </span>
      )
    } else if (dynamicSummary.primaryEntries.length > 0) {
      priceSummary = (
        <>
          {dynamicSummary.primaryEntries.map((entry) => (
            <span
              key={entry.key}
              className='text-muted-foreground whitespace-nowrap'
            >
              {t(entry.shortLabel)}{' '}
              <span className='text-foreground font-mono font-semibold'>
                <CountUp text={entry.formatted} active={inView} />
              </span>
            </span>
          ))}
        </>
      )
    } else {
      priceSummary = (
        <span className='text-muted-foreground text-sm'>
          {t('Dynamic Pricing')}
        </span>
      )
    }
  }

  // Price row: a billing-mode chip (按量 / 按次) leads, then the prices.
  // Per-token billing splits the remaining width into two equal halves and
  // centres each price in its half; per-request billing centres the single
  // price. Built with if/else so the JSX has no nested ternaries.
  let billingModeLabel = t('Per-request')
  if (isDynamicPricing) {
    billingModeLabel = t('Dynamic Pricing')
  } else if (isTokenBased) {
    billingModeLabel = t('Per-token')
  }
  const billingModeChip = (
    <span className='bg-muted/50 text-muted-foreground shrink-0 rounded px-1.5 py-0.5 text-xs leading-none font-medium whitespace-nowrap'>
      {billingModeLabel}
    </span>
  )

  let priceSummaryBlock: ReactNode
  if (dynamicSummary) {
    priceSummaryBlock = (
      <div className='flex flex-wrap items-center gap-x-3 gap-y-1 pl-1 text-sm'>
        {billingModeChip}
        {priceSummary}
      </div>
    )
  } else if (isTokenBased) {
    priceSummaryBlock = (
      <div className='flex items-center gap-3 pl-1 text-sm'>
        {billingModeChip}
        <div className='grid flex-1 grid-cols-2 items-center gap-2'>
          {(['input', 'output'] as const).map((type) => (
            <span
              key={type}
              className='flex min-w-0 items-baseline justify-center gap-1 whitespace-nowrap'
            >
              <span className='text-muted-foreground text-xs'>
                {type === 'input' ? t('Input') : t('Output')}
              </span>
              <span className='text-foreground font-mono font-semibold'>
                <CountUp
                  text={formatPrice(
                    props.model,
                    type,
                    tokenUnit,
                    showRechargePrice,
                    priceRate,
                    usdExchangeRate,
                    props.selectedGroup
                  )}
                  active={inView}
                />
              </span>
              <span className='text-muted-foreground/70 text-xs'>
                / {tokenUnitLabel}
              </span>
            </span>
          ))}
        </div>
      </div>
    )
  } else {
    priceSummaryBlock = (
      <div className='flex items-center gap-3 pl-1 text-sm'>
        {billingModeChip}
        <span className='flex flex-1 items-baseline justify-center gap-1 whitespace-nowrap'>
          <span className='text-foreground font-mono font-semibold'>
            <CountUp
              text={formatRequestPrice(
                props.model,
                showRechargePrice,
                priceRate,
                usdExchangeRate,
                props.selectedGroup
              )}
              active={inView}
            />
          </span>
          <span className='text-muted-foreground/70 text-xs'>
            / {t('request')}
          </span>
        </span>
      </div>
    )
  }

  return (
    <Card
      role='button'
      tabIndex={0}
      aria-label={`${t('View details')} · ${props.model.model_name}`}
      onClick={props.onClick}
      onKeyDown={handleCardKeyDown}
      className={cn(
        // No h-full: in the masonry columns the card is exactly as tall as its
        // content, which is what lets cards with a description sit taller.
        '@container cursor-pointer gap-3 select-none rounded-xl sm:gap-4',
        'transition-[transform,box-shadow] duration-300 ease-out motion-reduce:transition-none',
        // Hover only adds a soft shadow; the press is a small scale that
        // eases back out. No hover transform at all.
        'hover:shadow-lg hover:ring-border/50',
        'focus-visible:ring-primary/50',
        'active:scale-[0.97]'
      )}
    >
      {/* Header: the name itself is the copy control, provider icon and
          provider name included; details live on the card. The name stays on
          one line and truncates — the full string is in the button's label
          and on copy. */}
      <CardHeader className='gap-1.5'>
        <button
          type='button'
          onClick={handleCopy}
          title={`${t('Copy model name')} · ${props.model.model_name}`}
          aria-label={`${t('Copy model name')} · ${props.model.model_name}`}
          className='group/name hover:bg-foreground/[0.06] focus-visible:bg-foreground/[0.06] focus-visible:ring-primary/50 flex min-w-0 max-w-full items-center gap-2 self-start rounded-lg bg-foreground/[0.03] p-1 pr-2.5 text-left transition-colors focus-visible:ring-1 focus-visible:outline-none sm:gap-2.5'
        >
          <span className='bg-muted/40 flex size-8 shrink-0 items-center justify-center rounded-md sm:size-9 sm:rounded-lg'>
            {modelIcon || (
              <span className='text-muted-foreground text-sm font-bold'>
                {initial}
              </span>
            )}
          </span>
          <span className='flex min-w-0 flex-1 flex-col'>
            <span className='text-foreground group-hover/name:text-primary truncate font-mono text-sm leading-snug font-semibold transition-colors'>
              {props.model.model_name}
            </span>
            {(props.model.vendor_name || endpoints.length > 0) && (
              <span className='text-muted-foreground flex min-w-0 items-center gap-1 text-[10px] leading-tight'>
                {props.model.vendor_name && (
                  <span className='truncate'>{props.model.vendor_name}</span>
                )}
                {props.model.vendor_name && endpoints.length > 0 && (
                  <span className='text-muted-foreground/40 shrink-0'>·</span>
                )}
                {endpoints.length > 0 && (
                  <span className='truncate'>{endpoints.join(' · ')}</span>
                )}
              </span>
            )}
          </span>
          {/* Copy affordance: appears at the right edge of the title button on
              button hover so the header reads as "click to copy"; a check
              replaces it briefly after a successful copy. */}
          {copied ? (
            <Check className='text-success size-3 shrink-0' />
          ) : (
            <Copy className='text-muted-foreground/50 size-3 shrink-0 opacity-0 transition-opacity group-hover/name:opacity-100' />
          )}
        </button>
      </CardHeader>

      <CardContent className='flex flex-col gap-4'>
      {/* Description: only when the model has one; left edge lines up with
          the icon inside the title button above (button p-1 = 4px inset). */}
      {props.model.description && (
        <p className='text-muted-foreground line-clamp-2 pl-1 text-sm leading-relaxed'>
          {props.model.description}
        </p>
      )}

      {priceSummaryBlock}

      {/* Group price table. The numeric columns are `auto` tracks on one
          grid (rows are `display: contents`), so each number column takes
          the width of its widest value — digits get a fixed slot per column
          and never shift between rows. */}
      {groupRows.length > 0 && (
        <div
          aria-expanded={tableExpanded}
          aria-label={
            tableExpanded
              ? t('Collapse group price table')
              : t('Expand group price table')
          }
          onClick={(event) => {
            event.stopPropagation()
            setTableExpanded((expanded) => !expanded)
          }}
          className='border-border/50 cursor-pointer overflow-hidden rounded-lg border'
        >
          <div
            className={cn(
              'grid items-center text-xs',
              // Content-sized columns spread across the row: every number
              // keeps a slot as wide as its widest peer, and the free space
              // is distributed between columns instead of pooling after the
              // group name.
              isTokenBased
                ? 'grid-cols-[auto_auto_auto_auto] justify-between'
                : 'grid-cols-[auto_auto_auto] justify-between'
            )}
          >
            <div className='contents'>
              <span className='bg-muted/30 text-muted-foreground/70 flex items-center gap-1 px-2.5 py-1.5 text-[10px] font-medium'>
                {t('Group')}
                <button
                  type='button'
                  tabIndex={0}
                  aria-expanded={tableExpanded}
                  aria-label={
                    tableExpanded
                      ? t('Collapse group price table')
                      : t('Expand group price table')
                  }
                  onClick={(event) => {
                    event.stopPropagation()
                    setTableExpanded((expanded) => !expanded)
                  }}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter' || event.key === ' ') {
                      event.preventDefault()
                      event.stopPropagation()
                      setTableExpanded((expanded) => !expanded)
                    }
                  }}
                  className='p-0 m-0 border-none bg-transparent text-current inline'
                >
                  <ChevronDown
                    className={cn(
                      'size-3 transition-transform duration-200',
                      tableExpanded && 'rotate-180'
                    )}
                  />
                </button>
              </span>
              <span className='bg-muted/30 text-muted-foreground/70 px-2.5 py-1.5 text-left text-[10px] font-medium'>
                {t('Ratio')}
              </span>
              {isTokenBased ? (
                <>
                  <span className='bg-muted/30 text-muted-foreground/70 px-2.5 py-1.5 text-left text-[10px] font-medium'>
                    {t('Input')}
                  </span>
                  <span className='bg-muted/30 text-muted-foreground/70 px-2.5 py-1.5 text-left text-[10px] font-medium'>
                    {t('Output')}
                  </span>
                </>
              ) : (
                <span className='bg-muted/30 text-muted-foreground/70 px-2.5 py-1.5 text-left text-[10px] font-medium'>
                  {t('Price')}
                </span>
              )}
            </div>
            {(groupRows.length > 2
              ? tableExpanded
                ? groupRows
                : groupRows.slice(0, 2)
              : tableExpanded
                ? []
                : groupRows
            ).map((row) => (
              <div key={row.group} className='contents'>
                <span className='border-border/50 max-w-[12rem] truncate border-t px-2.5 py-1.5 font-medium'>
                  {row.group}
                </span>
                <span className='text-muted-foreground border-border/50 border-t px-2.5 py-1.5 text-left font-mono tabular-nums'>
                  <CountUp text={`×${row.ratio}`} active={inView} />
                </span>
                {isTokenBased ? (
                  <>
                    <span className='text-foreground border-border/50 border-t px-2.5 py-1.5 text-left font-mono tabular-nums'>
                      <CountUp text={row.input} active={inView} />
                    </span>
                    <span className='text-foreground border-border/50 border-t px-2.5 py-1.5 text-left font-mono tabular-nums'>
                      <CountUp text={row.output} active={inView} />
                    </span>
                  </>
                ) : (
                  <span className='text-foreground border-border/50 border-t px-2.5 py-1.5 text-left font-mono tabular-nums'>
                    <CountUp text={row.price} active={inView} />
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Middle: 24h latency/success combo chart + headline numbers */}
      <ModelCardMetrics perf={props.perf} visible={inView} />
      </CardContent>
    </Card>
  )
})

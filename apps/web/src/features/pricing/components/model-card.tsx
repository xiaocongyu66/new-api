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
import { Check, Copy } from 'lucide-react'
import { memo, useEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'

import { DEFAULT_TOKEN_UNIT } from '../constants'
import {
  getDynamicDisplayGroupRatio,
  getDynamicPricingSummary,
} from '../lib/dynamic-price'
import { parseTags } from '../lib/filters'
import { getConfiguredGroupRatio, isTokenBasedModel } from '../lib/model-helpers'
import {
  formatFixedPrice,
  formatGroupPrice,
  formatPrice,
  formatRequestPrice,
} from '../lib/price'
import type { ModelPerfBadgeData, PricingModel, TokenUnit } from '../types'
import { ModelBillingModeBadge } from './model-billing-mode-badge'
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
}

export const ModelCard = memo(function ModelCard(props: ModelCardProps) {
  const { t } = useTranslation()
  const { copyToClipboard } = useCopyToClipboard()
  const tokenUnit = props.tokenUnit ?? DEFAULT_TOKEN_UNIT
  const priceRate = props.priceRate ?? 1
  const usdExchangeRate = props.usdExchangeRate ?? 1
  const showRechargePrice = props.showRechargePrice ?? false
  const isTokenBased = isTokenBasedModel(props.model)
  const tokenUnitLabel = tokenUnit === 'K' ? '1K' : '1M'
  const tags = parseTags(props.model.tags)
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

  const primaryGroup = groups[0]
  const bottomTags = [...endpoints.slice(0, 2), ...tags.slice(0, 2)]
  const hiddenCount =
    Math.max(groups.length - 1, 0) +
    Math.max(endpoints.length - 2, 0) +
    Math.max(tags.length - 2, 0)

  // Price table rows: the first two enabled groups with their ratio and the
  // per-group prices. Dynamic-expression models have no stable per-group
  // numbers to tabulate, so they keep the summary row only.
  const groupRows = isDynamicPricing
    ? []
    : groups.slice(0, 2).map((group) => ({
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
                {entry.formatted}
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

  // Price summary above the group table. Per-token billing packs the input
  // and output prices left with a generous gap (no mid-card column start);
  // per-request billing is a single price; dynamic expressions keep the
  // flexible summary. Built with if/else so the JSX has no nested ternaries.
  let priceSummaryBlock: ReactNode
  if (dynamicSummary) {
    priceSummaryBlock = (
      <div className='flex flex-wrap items-baseline gap-x-3 gap-y-1 pl-1 text-sm'>
        {priceSummary}
      </div>
    )
  } else if (isTokenBased) {
    priceSummaryBlock = (
      <div className='flex flex-wrap items-center gap-x-5 gap-y-1 pl-1 text-sm'>
        {(['input', 'output'] as const).map((type) => (
          <span
            key={type}
            className='flex min-w-0 items-baseline gap-1 whitespace-nowrap'
          >
            <span className='text-muted-foreground'>
              {type === 'input' ? t('Input') : t('Output')}
            </span>
            <span className='text-foreground font-mono font-semibold'>
              {formatPrice(
                props.model,
                type,
                tokenUnit,
                showRechargePrice,
                priceRate,
                usdExchangeRate,
                props.selectedGroup
              )}
            </span>
            <span className='text-muted-foreground/70 text-xs'>
              / {tokenUnitLabel}
            </span>
          </span>
        ))}
      </div>
    )
  } else {
    priceSummaryBlock = (
      <div className='flex items-baseline gap-1 pl-1 text-sm'>
        <span className='text-foreground font-mono font-semibold'>
          {formatRequestPrice(
            props.model,
            showRechargePrice,
            priceRate,
            usdExchangeRate,
            props.selectedGroup
          )}
        </span>
        <span className='text-muted-foreground/70 text-xs'>
          / {t('request')}
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
        '@container h-full cursor-pointer gap-3 select-none sm:gap-4',
        'transition-[box-shadow,background-color] motion-reduce:transition-none',
        // The card's visible edge is a ring, so hover/active retint that ring
        // instead of adding a border that would shift the content width.
        'hover:ring-primary/40 hover:bg-primary/[0.03]',
        'focus-visible:ring-primary/50',
        'active:ring-primary/70 active:bg-primary/[0.06]'
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

      <CardContent className='flex flex-1 flex-col gap-4'>
      {/* Description: only when the model has one; left edge lines up with
          the icon inside the title button above (button p-1 = 4px inset). */}
      {props.model.description && (
        <p className='text-muted-foreground line-clamp-2 flex-1 pl-1 text-sm leading-relaxed'>
          {props.model.description}
        </p>
      )}

      {priceSummaryBlock}

      {/* Group price table, mirroring the details sheet: 4 columns for
          per-token billing (分组 · 倍率 · 输入 · 输出), 3 for per-request
          (分组 · 倍率 · 价格). Equal-width columns, ratio centered. */}
      {groupRows.length > 0 && (
        <div className='border-border/50 overflow-hidden rounded-lg border'>
          <div
            className={cn(
              'bg-muted/30 text-muted-foreground/70 grid gap-2 border-b px-2.5 py-1.5 text-[10px] font-medium',
              isTokenBased ? 'grid-cols-4' : 'grid-cols-3'
            )}
          >
            <span className='truncate'>{t('Group')}</span>
            <span className='text-center'>{t('Ratio')}</span>
            {isTokenBased ? (
              <>
                <span className='text-right'>{t('Input')}</span>
                <span className='text-right'>{t('Output')}</span>
              </>
            ) : (
              <span className='text-right'>{t('Price')}</span>
            )}
          </div>
          {groupRows.map((row) => (
            <div
              key={row.group}
              className={cn(
                'grid items-center gap-2 px-2.5 py-1.5 text-xs',
                isTokenBased ? 'grid-cols-4' : 'grid-cols-3'
              )}
            >
              <span className='truncate font-medium'>{row.group}</span>
              <span className='text-muted-foreground text-center font-mono tabular-nums'>
                ×{row.ratio}
              </span>
              {isTokenBased ? (
                <>
                  <span className='text-foreground text-right font-mono tabular-nums'>
                    {row.input}
                  </span>
                  <span className='text-foreground text-right font-mono tabular-nums'>
                    {row.output}
                  </span>
                </>
              ) : (
                <span className='text-foreground text-right font-mono tabular-nums'>
                  {row.price}
                </span>
              )}
            </div>
          ))}
        </div>
      )}

      {/* Middle: 24h latency/success combo chart + headline numbers */}
      <ModelCardMetrics perf={props.perf} />

      {/* Footer: left metadata and right performance summary share row alignment */}
      <div className='grid grid-cols-[minmax(0,1fr)_auto] items-start gap-x-2 gap-y-2 border-t pt-3'>
        <div className='flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1'>
          {primaryGroup && (
            <span className='text-muted-foreground text-sm font-medium'>
              {primaryGroup}
            </span>
          )}
          <ModelBillingModeBadge model={props.model} />
        </div>

        <div className='flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-0.5 sm:gap-x-3 sm:gap-y-1'>
          {bottomTags.map((item) => (
            <span key={item} className='text-muted-foreground/70 text-xs'>
              {item}
            </span>
          ))}
          {hiddenCount > 0 && (
            <span className='text-muted-foreground/40 text-xs'>
              +{hiddenCount}
            </span>
          )}
        </div>
      </div>
      </CardContent>
    </Card>
  )
})

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
import type { ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'

import {
  BadgeCell,
  BadgeListCell,
  DataTableColumnHeader,
} from '@/components/data-table'
import { GroupBadge } from '@/components/group-badge'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { getLobeIcon } from '@/lib/lobe-icon'

import { DEFAULT_TOKEN_UNIT } from '../constants'
import {
  getDynamicDisplayGroupRatio,
  getDynamicPricingSummary,
  getDynamicPricingTiers,
  getDynamicPriceEntries,
  getTierBoundLabel,
  PRIMARY_DYNAMIC_FIELDS,
} from '../lib/dynamic-price'
import { parseTags } from '../lib/filters'
import { isTokenBasedModel } from '../lib/model-helpers'
import {
  formatPrice,
  formatRequestPrice,
  stripTrailingZeros,
} from '../lib/price'
import type { PricingModel, TokenUnit } from '../types'
import { ModelBillingModeBadge } from './model-billing-mode-badge'

// ----------------------------------------------------------------------------
// Pricing Table Columns
// ----------------------------------------------------------------------------

export interface PricingColumnsOptions {
  tokenUnit?: TokenUnit
  priceRate?: number
  usdExchangeRate?: number
  showRechargePrice?: boolean
  selectedGroup?: string
  onModelClick?: (modelName: string) => void
}

export function usePricingColumns(
  options: PricingColumnsOptions = {}
): ColumnDef<PricingModel>[] {
  const { t } = useTranslation()
  const {
    tokenUnit = DEFAULT_TOKEN_UNIT,
    priceRate = 1,
    usdExchangeRate = 1,
    showRechargePrice = false,
    selectedGroup,
  } = options

  const tokenUnitLabel = tokenUnit === 'K' ? '1K' : '1M'

  return [
    // Model column
    {
      accessorKey: 'model_name',
      meta: { label: t('Model') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Model')} />
      ),
      cell: ({ row }) => {
        const model = row.original
        const modelIconKey = model.icon || model.vendor_icon
        const modelIcon = modelIconKey ? getLobeIcon(modelIconKey, 14) : null

        return (
          <div className='flex max-w-full min-w-0 items-center gap-2'>
            {modelIcon}
            <Button
              variant='ghost'
              size='sm'
              className='h-auto min-w-0 justify-start px-0 font-mono text-sm'
              onClick={(event) => {
                event.stopPropagation()
                options.onModelClick?.(model.model_name)
              }}
              aria-label={`${t('Details')} · ${model.model_name}`}
            >
              <span className='truncate'>{model.model_name}</span>
            </Button>
          </div>
        )
      },
      minSize: 200,
    },

    // Type column
    {
      accessorKey: 'quota_type',
      header: t('Type'),
      cell: ({ row }) => (
        <ModelBillingModeBadge model={row.original} className='-ml-1.5' />
      ),
      size: 110,
      enableSorting: false,
    },

    // Price column
    {
      accessorKey: 'price',
      meta: { label: t('Price') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Price')} />
      ),
      cell: ({ row }) => {
        const model = row.original
        const dynamicSummary = getDynamicPricingSummary(model, {
          tokenUnit,
          showRechargePrice,
          priceRate,
          usdExchangeRate,
          groupRatioMultiplier: getDynamicDisplayGroupRatio(
            model,
            selectedGroup
          ),
        })

        if (dynamicSummary) {
          if (dynamicSummary.isSpecialExpression) {
            return (
              <div className='max-w-full min-w-0'>
                <div className='text-foreground text-xs font-medium'>
                  {t('Special billing expression')}
                </div>
                <div className='text-muted-foreground text-[11px]'>
                  {t('Unable to parse structured pricing')}
                </div>
                <code className='text-muted-foreground/70 mt-1 line-clamp-2 block font-mono text-[10px] leading-relaxed break-all'>
                  {dynamicSummary.rawExpression}
                </code>
              </div>
            )
          }

          const primaryEntries = dynamicSummary.primaryEntries.slice(0, 2)
          if (primaryEntries.length === 0) {
            return (
              <span className='text-muted-foreground text-xs'>
                {t('Dynamic Pricing')}
              </span>
            )
          }

          // Per-tier ladder: bound + the primary prices for that tier, so the
          // table shows the full interval structure, not just tier #1.
          const tierLadder = getDynamicPricingTiers(model)
            .slice(0, 3)
            .map((tier, index) => {
              const bound = getTierBoundLabel(
                getDynamicPricingTiers(model),
                index
              )
              const entries = getDynamicPriceEntries(tier, {
                tokenUnit,
                showRechargePrice,
                priceRate,
                usdExchangeRate,
                groupRatioMultiplier: getDynamicDisplayGroupRatio(
                  model,
                  selectedGroup
                ),
              })
                .filter((entry) => PRIMARY_DYNAMIC_FIELDS.has(entry.field))
                .map((entry) => stripTrailingZeros(entry.formatted))
              return { bound, entries, index }
            })
            .filter((tier) => tier.entries.length > 0)

          return (
            <div className='max-w-full min-w-0'>
              <span className='font-mono text-sm tabular-nums'>
                {primaryEntries.map((entry, index) => (
                  <span key={entry.key}>
                    {index > 0 && (
                      <span className='text-muted-foreground/40 mx-1'>/</span>
                    )}
                    {t(entry.shortLabel)} {stripTrailingZeros(entry.formatted)}
                  </span>
                ))}
              </span>
              <div className='text-muted-foreground/50 text-[10px]'>
                /{' '}
                {primaryEntries.every((entry) => entry.variable.isPerRequest)
                  ? t('request')
                  : `${tokenUnitLabel} tokens`}
                {dynamicSummary.tierCount > 1 &&
                  ` · ${t('{{count}} tiers', {
                    count: dynamicSummary.tierCount,
                  })}`}
              </div>
              {tierLadder.length > 1 && (
                <div className='text-muted-foreground/60 mt-0.5 flex flex-col font-mono text-[10px] leading-4'>
                  {tierLadder.map((tier) => (
                    <span key={tier.index} className='truncate'>
                      <span className='text-muted-foreground/80'>
                        {tier.bound || t('Default')}
                      </span>{' '}
                      {tier.entries.join(' / ')}
                    </span>
                  ))}
                </div>
              )}
            </div>
          )
        }

        const isTokenBased = isTokenBasedModel(model)

        if (isTokenBased) {
          const inputPrice = stripTrailingZeros(
            formatPrice(
              model,
              'input',
              tokenUnit,
              showRechargePrice,
              priceRate,
              usdExchangeRate,
              selectedGroup
            )
          )
          const outputPrice = stripTrailingZeros(
            formatPrice(
              model,
              'output',
              tokenUnit,
              showRechargePrice,
              priceRate,
              usdExchangeRate,
              selectedGroup
            )
          )

          return (
            <div className='max-w-full min-w-0'>
              <span className='font-mono text-sm tabular-nums'>
                {t('Input')} {inputPrice}
                <span className='text-muted-foreground/40 mx-1'>/</span>
                {t('Output')} {outputPrice}
              </span>
              <div className='text-muted-foreground/50 text-[10px]'>
                / {tokenUnitLabel} tokens
              </div>
            </div>
          )
        }

        const price = stripTrailingZeros(
          formatRequestPrice(
            model,
            showRechargePrice,
            priceRate,
            usdExchangeRate,
            selectedGroup
          )
        )

        return (
          <div className='max-w-full min-w-0'>
            <span className='font-mono text-sm tabular-nums'>{price}</span>
            <div className='text-muted-foreground/50 text-[10px]'>
              / {t('request')}
            </div>
          </div>
        )
      },
      size: 180,
      enableSorting: false,
    },

    // Ratio column: the model multiplier for token-based pricing, so the
    // table shows both the price and the underlying rate; tiered/per-request
    // models carry no flat ratio and render a dash.
    {
      id: 'model_ratio',
      meta: { label: t('Ratio') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Ratio')} />
      ),
      cell: ({ row }) => {
        const model = row.original
        if (
          model.billing_mode === 'tiered_expr' ||
          model.billing_mode === 'context_tier' ||
          model.billing_mode === 'context_tier_per_call'
        ) {
          return (
            <span className='text-muted-foreground font-mono text-xs'>—</span>
          )
        }
        if (!isTokenBasedModel(model)) {
          return (
            <span className='text-muted-foreground font-mono text-xs'>—</span>
          )
        }
        const ratio = stripTrailingZeros(String(model.model_ratio ?? 0))
        return (
          <div className='max-w-full min-w-0'>
            <span className='font-mono text-sm tabular-nums'>×{ratio}</span>
            <div className='text-muted-foreground/50 text-[10px]'>
              {t('Model ratio')}
            </div>
          </div>
        )
      },
      size: 110,
      enableSorting: false,
    },

    // Cached price column (Vercel AI Gateway style)
    {
      id: 'cached_price',
      header: t('Cached'),
      cell: ({ row }) => {
        const model = row.original
        const dynamicSummary = getDynamicPricingSummary(model, {
          tokenUnit,
          showRechargePrice,
          priceRate,
          usdExchangeRate,
          groupRatioMultiplier: getDynamicDisplayGroupRatio(
            model,
            selectedGroup
          ),
        })

        if (dynamicSummary) {
          if (dynamicSummary.isSpecialExpression) {
            return (
              <span className='text-muted-foreground/50 text-xs'>
                {t('Special billing expression')}
              </span>
            )
          }

          const cacheEntry = dynamicSummary.entries.find(
            (entry) => entry.field === 'cacheReadPrice'
          )
          if (!cacheEntry) {
            return <span className='text-muted-foreground/30 text-xs'>—</span>
          }

          return (
            <div className='max-w-full min-w-0'>
              <span className='font-mono text-sm tabular-nums'>
                {stripTrailingZeros(cacheEntry.formatted)}
              </span>
              <div className='text-muted-foreground/50 text-[10px]'>
                / {tokenUnitLabel}
              </div>
            </div>
          )
        }

        const isTokenBased = isTokenBasedModel(model)

        if (!isTokenBased || model.cache_ratio == null) {
          return <span className='text-muted-foreground/30 text-xs'>—</span>
        }

        const cachedPrice = stripTrailingZeros(
          formatPrice(
            model,
            'cache',
            tokenUnit,
            showRechargePrice,
            priceRate,
            usdExchangeRate,
            selectedGroup
          )
        )

        return (
          <div className='max-w-full min-w-0'>
            <span className='font-mono text-sm tabular-nums'>
              {cachedPrice}
            </span>
            <div className='text-muted-foreground/50 text-[10px]'>
              / {tokenUnitLabel}
            </div>
          </div>
        )
      },
      size: 110,
      enableSorting: false,
    },

    // Vendor column
    {
      accessorKey: 'vendor_name',
      header: t('Vendor'),
      cell: ({ row }) => {
        const model = row.original
        if (!model.vendor_name) {
          return <span className='text-muted-foreground/50 text-xs'>—</span>
        }
        const vendorIcon = model.vendor_icon
          ? getLobeIcon(model.vendor_icon, 12)
          : null
        return (
          <BadgeCell className='gap-1.5'>
            {vendorIcon}
            <StatusBadge
              label={model.vendor_name}
              autoColor={model.vendor_name}
              size='sm'
              copyable={false}
            />
          </BadgeCell>
        )
      },
      size: 130,
      enableSorting: false,
    },

    // Tags column
    {
      accessorKey: 'tags',
      header: t('Tags'),
      cell: ({ row }) => {
        const tags = parseTags(row.original.tags)
        return (
          <BadgeListCell
            items={tags.map((tag) => (
              <StatusBadge
                key={tag}
                label={tag}
                autoColor={tag}
                size='sm'
                copyable={false}
              />
            ))}
          />
        )
      },
      size: 140,
      enableSorting: false,
    },

    // Endpoints column
    {
      accessorKey: 'supported_endpoint_types',
      header: t('Endpoints'),
      cell: ({ row }) => {
        const endpoints = row.original.supported_endpoint_types || []
        return (
          <BadgeListCell
            items={endpoints.map((ep) => (
              <StatusBadge
                key={ep}
                label={ep}
                autoColor={ep}
                size='sm'
                copyable={false}
              />
            ))}
          />
        )
      },
      size: 130,
      enableSorting: false,
    },

    // Enable Groups column
    {
      accessorKey: 'enable_groups',
      header: t('Groups'),
      cell: ({ row }) => {
        const groups = row.original.enable_groups || []
        return (
          <BadgeListCell
            items={groups.map((group) => (
              <GroupBadge key={group} group={group} size='sm' />
            ))}
            tooltipClassName='max-w-[280px] p-2'
          />
        )
      },
      size: 130,
      enableSorting: false,
    },
  ]
}

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
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { getPerfMetricsSummary } from '@/features/performance-metrics/api'
import { Main, PublicLayout } from '@/components/layout'
import { Card, CardContent } from '@/components/ui/card'

import {
  LoadingSkeleton,
  EmptyState,
  SearchBar,
  PricingTable,
  PricingToolbar,
  ModelCardGrid,
  ModelDetailsDrawer,
} from './components'
import {
  DEFAULT_TOKEN_UNIT,
  EXCLUDED_GROUPS,
  SORT_OPTIONS,
  VIEW_MODES,
} from './constants'
import { useFilters } from './hooks/use-filters'
import { usePricingData } from './hooks/use-pricing-data'

export function Pricing() {
  const { t } = useTranslation()
  const [selectedModelName, setSelectedModelName] = useState<string | null>(
    null
  )

  const {
    models,
    vendors,
    groupRatio,
    usableGroup,
    endpointMap,
    autoGroups,
    isLoading,
    error,
    refetch,
    priceRate,
    usdExchangeRate,
    displayGroupRatioTable,
  } = usePricingData()

  const {
    searchInput,
    sortBy,
    vendorFilter,
    groupFilter,
    quotaTypeFilter,
    endpointTypeFilter,
    tagFilter,
    viewMode,
    showRechargePrice,
    setSearchInput,
    setSortBy,
    setVendorFilter,
    setGroupFilter,
    setQuotaTypeFilter,
    setEndpointTypeFilter,
    setTagFilter,
    setViewMode,
    filteredModels,
    hasActiveFilters,
    availableTags,
    clearFilters,
    clearSearch,
  } = useFilters(models || [])

  // Success-rate sorting needs the perf summary; the card grid uses the same
  // query key, so both share one cached request.
  const { data: perfSummary } = useQuery({
    queryKey: ['perf-metrics-summary', 24],
    queryFn: () => getPerfMetricsSummary(24),
    staleTime: 60 * 1000,
    retry: false,
  })

  const successRateByName = useMemo(() => {
    const map = new Map<string, number>()
    for (const model of perfSummary?.data?.models ?? []) {
      map.set(model.model_name, model.success_rate)
    }
    return map
  }, [perfSummary])

  const sortedModels = useMemo(() => {
    if (sortBy !== SORT_OPTIONS.SUCCESS_RATE) {
      return filteredModels
    }
    return [...filteredModels].sort((a, b) => {
      const rateA = successRateByName.get(a.model_name) ?? -1
      const rateB = successRateByName.get(b.model_name) ?? -1
      return rateB - rateA || (a.model_name || '').localeCompare(b.model_name || '')
    })
  }, [filteredModels, sortBy, successRateByName])

  const handleModelClick = useCallback((modelName: string) => {
    setSelectedModelName(modelName)
  }, [])

  const selectedModel = useMemo(
    () =>
      selectedModelName
        ? (models || []).find(
            (model) => model.model_name === selectedModelName
          ) || null
        : null,
    [models, selectedModelName]
  )

  const availableGroups = useMemo(
    () =>
      Object.keys(usableGroup || {}).filter(
        (g) => !EXCLUDED_GROUPS.includes(g)
      ),
    [usableGroup]
  )

  const handleClearAll = useCallback(() => {
    clearFilters()
    clearSearch()
  }, [clearFilters, clearSearch])

  const renderPricingContent = () => {
    if (filteredModels.length === 0) {
      return (
        <EmptyState
          searchQuery={searchInput}
          hasActiveFilters={hasActiveFilters}
          onClearFilters={handleClearAll}
        />
      )
    }

    if (viewMode === VIEW_MODES.CARD) {
      return (
        <ModelCardGrid
          models={sortedModels}
          onModelClick={handleModelClick}
          priceRate={priceRate}
          usdExchangeRate={usdExchangeRate}
          tokenUnit={DEFAULT_TOKEN_UNIT}
          showRechargePrice={showRechargePrice}
          selectedGroup={groupFilter}
          displayGroupRatioTable={displayGroupRatioTable}
        />
      )
    }

    return (
      <PricingTable
        models={sortedModels}
        priceRate={priceRate}
        usdExchangeRate={usdExchangeRate}
        tokenUnit={DEFAULT_TOKEN_UNIT}
        showRechargePrice={showRechargePrice}
        selectedGroup={groupFilter}
        onModelClick={handleModelClick}
      />
    )
  }

  if (isLoading) {
    return (
      <PublicLayout showMainContainer={false}>
        <Main aria-busy='true' className='mx-auto w-full max-w-[2200px] px-4 pt-20 pb-10 sm:px-6 sm:pt-24 lg:px-8'>
          <LoadingSkeleton viewMode={viewMode} />
        </Main>
      </PublicLayout>
    )
  }

  return (
    <PublicLayout showMainContainer={false}>
      <Main className='mx-auto w-full max-w-[2200px] gap-6 overflow-visible px-4 pt-20 pb-10 sm:px-6 sm:pt-24 lg:px-8'>
          <header className='flex flex-col gap-5'>
            <div className='min-w-0 pt-4 text-center'>
            <h1 className='text-4xl leading-tight font-bold tracking-tight sm:text-5xl'>
              {t('Model Square')}
            </h1>
            <p className='text-muted-foreground mt-3 text-sm'>
              {t('This site currently has {{count}} models enabled', {
                count: models?.length || 0,
              })}
            </p>
            <p className='text-muted-foreground mx-auto mt-2 max-w-2xl text-sm leading-relaxed'>
              {t(
                'Discover curated AI models, compare pricing and capabilities, and choose the right model for every scenario.'
              )}
            </p>
            </div>
            <SearchBar
              value={searchInput}
              onChange={setSearchInput}
              onClear={clearSearch}
              placeholder={t(
                'Search model name, provider, endpoint, or tag...'
              )}
              className='mx-auto w-full max-w-2xl'
            />
          </header>

          <section aria-label={t('Models')} className='min-w-0 space-y-4'>
            <PricingToolbar
              filteredCount={filteredModels.length}
              sortBy={sortBy}
              onSortChange={setSortBy}
              viewMode={viewMode}
              onViewModeChange={setViewMode}
              quotaTypeFilter={quotaTypeFilter}
              endpointTypeFilter={endpointTypeFilter}
              vendorFilter={vendorFilter}
              groupFilter={groupFilter}
              tagFilter={tagFilter}
              onQuotaTypeChange={setQuotaTypeFilter}
              onEndpointTypeChange={setEndpointTypeFilter}
              onVendorChange={setVendorFilter}
              onGroupChange={setGroupFilter}
              onTagChange={setTagFilter}
              vendors={vendors || []}
              groups={availableGroups}
              groupRatios={groupRatio}
              tags={availableTags}
              models={models || []}
              hasActiveFilters={hasActiveFilters}
              onClearFilters={clearFilters}
            />

            {error ? (
              <Card>
                <CardContent>
                  <ErrorState title={t('Loading failed')} onRetry={() => void refetch()} />
                </CardContent>
              </Card>
            ) : renderPricingContent()}
          </section>

          {selectedModel && (
            <ModelDetailsDrawer
              open={Boolean(selectedModel)}
              onOpenChange={(open) => {
                if (!open) setSelectedModelName(null)
              }}
              model={selectedModel}
              groupRatio={groupRatio || {}}
              usableGroup={usableGroup || {}}
              endpointMap={
                (endpointMap as Record<
                  string,
                  { path?: string; method?: string }
                >) || {}
              }
              autoGroups={autoGroups || []}
              priceRate={priceRate ?? 1}
              usdExchangeRate={usdExchangeRate ?? 1}
              tokenUnit={DEFAULT_TOKEN_UNIT}
              showRechargePrice={showRechargePrice}
            />
          )}
      </Main>
    </PublicLayout>
  )
}

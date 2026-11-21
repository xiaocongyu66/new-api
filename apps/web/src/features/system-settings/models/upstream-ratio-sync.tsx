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

import { useMutation, useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'

import { ArrowUp, RefreshCcw } from 'lucide-react'
import { cn } from '@/lib/utils'
import {
  fetchPricingAnalysis,
  fetchUpstreamRatios,
  getPriceFluctuations,
  getUpstreamChannels,
} from '../api'
import type {
  PriceFluctuationChange,
  PricingAnalysisData,
  PricingAnalysisRow,
  PricingAnalysisRequest,
  UpstreamChannel,
  UpstreamConfig,
} from '../types'
import { PricingAnalysisDialog } from './pricing-analysis-dialog'
import { useIsMobile } from '@/hooks/use-mobile'

import { PricingAnalysisTable } from './pricing-analysis-table'
import { SettingsPageActionsPortal } from '../components/settings-page-context'
import { ChannelSelectorDialog } from './channel-selector-dialog'
import {
  DEFAULT_ENDPOINT,
  MODELS_DEV_PRESET_ENDPOINT,
  MODELS_DEV_PRESET_ID,
  OFFICIAL_CHANNEL_ENDPOINT,
  OFFICIAL_CHANNEL_ID,
  OPENROUTER_CHANNEL_TYPE,
  OPENROUTER_ENDPOINT,
} from './constants'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// The two synthesized presets always carry stable negative IDs assigned by
// `controller/ratio_sync.go`; matching by ID alone is sufficient and avoids
// fragile name/base_url comparisons.
function getDefaultEndpointForChannel(channel: UpstreamChannel): string {
  if (channel.id === MODELS_DEV_PRESET_ID) return MODELS_DEV_PRESET_ENDPOINT
  if (channel.id === OFFICIAL_CHANNEL_ID) return OFFICIAL_CHANNEL_ENDPOINT
  if (channel.type === OPENROUTER_CHANNEL_TYPE) return OPENROUTER_ENDPOINT
  return DEFAULT_ENDPOINT
}
const ANALYSIS_CACHE_KEY = 'pricing-analysis-cache-v1'
// The channel selection decides which upstreams the analysis prices against; losing it
// on reload silently reverted the comparison to the models.dev reference preset.
const SELECTION_STORAGE_KEY = 'pricing-analysis-selected-channels-v1'
const ANALYSIS_REFRESH_MS = 5 * 60 * 1000

type AnalysisSignature = {
  vendor: number
  days: number
  threshold: number
  channelIds: number[]
}

type CachedAnalysis = {
  data: PricingAnalysisData
  changes: PriceFluctuationChange[]
  savedAt: number
  signature: AnalysisSignature
}

function readCachedAnalysis(): CachedAnalysis | null {
  try {
    const raw = sessionStorage.getItem(ANALYSIS_CACHE_KEY)
    if (!raw) return null
    const cached = JSON.parse(raw) as CachedAnalysis
    if (!cached.data || !Array.isArray(cached.data.rows)) return null
    if (!Array.isArray(cached.changes) || typeof cached.savedAt !== 'number') {
      return null
    }
    if (
      !cached.signature ||
      typeof cached.signature.vendor !== 'number' ||
      !Array.isArray(cached.signature.channelIds)
    ) {
      return null
    }
    return cached
  } catch {
    return null
  }
}



// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function UpstreamRatioSync() {
  const { t } = useTranslation()

  const [channelDialogOpen, setChannelDialogOpen] = useState(false)
  const [selectedChannelIds, setSelectedChannelIds] = useState<number[]>(() => {
    try {
      const raw = sessionStorage.getItem(SELECTION_STORAGE_KEY)
      const parsed = raw ? JSON.parse(raw) : null
      return Array.isArray(parsed) ? parsed.filter((id) => typeof id === 'number') : []
    } catch {
      return []
    }
  })
  const [channelEndpoints, setChannelEndpoints] = useState<
    Record<number, string>
  >({})

  const [analysisData, setAnalysisData] = useState<PricingAnalysisData | null>(
    () => readCachedAnalysis()?.data ?? null
  )
  const [analysisLoading, setAnalysisLoading] = useState(false)
  const [analysisError, setAnalysisError] = useState<string | null>(null)
  const [fluctuationChanges, setFluctuationChanges] = useState<PriceFluctuationChange[]>(
    () => readCachedAnalysis()?.changes ?? []
  )
  const [analysisVendor, setAnalysisVendor] = useState<number>(0)
  const [analysisDays, setAnalysisDays] = useState<number>(30)
  const [analysisThreshold, setAnalysisThreshold] = useState<number>(10)
  const [selectedAnalysisRow, setSelectedAnalysisRow] = useState<PricingAnalysisRow | null>(null)
  const analysisLoadedRef = useRef(false)
  // Timestamp of the last successful channel fetch. handleRefreshAnalysis prefetches
  // /fetch before /analysis to pick up fresh prices, but the confirm-selection flow
  // already fetched: a second overlapping fetch writes the same fetched_at second and
  // the snapshot replace-by-second wipes the first batch's rows.
  const lastFetchAtRef = useRef(0)
  const lastFiltersRef = useRef({
    vendor: analysisVendor,
    days: analysisDays,
    threshold: analysisThreshold,
  })

  const {
    data: channelsData,
    isError: channelsError,
  } = useQuery({
    queryKey: ['upstream-channels'],
    queryFn: getUpstreamChannels,
  })

  useEffect(() => {
    // A silent channel-query failure would leave the page permanently empty
    // with no analysis fetch, so surface it at least once.
    if (channelsError) toast.error(t('Failed to load channels'))
  }, [channelsError, t])


  // Memoize the channels list so the effect below only re-runs when the query
  // data actually changes, instead of on every render (the `|| []` fallback
  // would otherwise produce a new array reference each render).
  const channels = useMemo(() => channelsData?.data ?? [], [channelsData?.data])


  // IDs of the channels the current analysis is actually based on; used in
  // the cache signature so switching channels invalidates cached results.
  const effectiveChannelIds = useMemo(() => {
    const selected = channels.filter((ch) => selectedChannelIds.includes(ch.id))
    const list =
      selected.length > 0
        ? selected
        : channels.filter((ch) => ch.id === MODELS_DEV_PRESET_ID)
    return list.map((ch) => ch.id).sort((a, b) => a - b)
  }, [channels, selectedChannelIds])
  useEffect(() => {
    if (channels.length === 0) return
    setChannelEndpoints((prev) => {
      let mutated = false
      const next = { ...prev }
      for (const channel of channels) {
        if (!next[channel.id]) {
          next[channel.id] = getDefaultEndpointForChannel(channel)
          mutated = true
        }
      }
      return mutated ? next : prev
    })
  }, [channels])

  const fetchMutation = useMutation({
    mutationFn: fetchUpstreamRatios,
    onSuccess: (data) => {
      if (!data.success) {
        toast.error(data.message || t('Failed to fetch upstream prices'))
        return
      }

      const { test_results } = data.data

      const errorResults = test_results.filter((r) => r.status === 'error')
      if (errorResults.length > 0) {
        const errorMsg = errorResults
          .map((r) => `${r.name}: ${r.error}`)
          .join(', ')
        toast.warning(t('Some channels failed: {{errorMsg}}', { errorMsg }))
      }

      toast.success(t('Upstream prices fetched successfully'))
      lastFetchAtRef.current = Date.now()

      // Channel selection changed: refresh the analysis so costs/profits
      // reflect the newly selected upstreams.
      void handleRefreshAnalysis()
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to fetch upstream prices'))
    },
  })


  const handleOpenChannelDialog = () => {
    setChannelDialogOpen(true)
  }

  useEffect(() => {
    try {
      sessionStorage.setItem(SELECTION_STORAGE_KEY, JSON.stringify(selectedChannelIds))
    } catch {
      // Storage may be unavailable; the selection still works for this page view.
    }
  }, [selectedChannelIds])

  const handleConfirmChannelSelection = (selectedIds: number[]) => {
    const selectedChannels = channels.filter((ch) =>
      selectedIds.includes(ch.id)
    )

    if (selectedChannels.length === 0) {
      toast.warning(t('Please select at least one channel'))
      return
    }

    const upstreams: UpstreamConfig[] = selectedChannels.map((ch) => ({
      id: ch.id,
      name: ch.name,
      base_url: ch.base_url,
      endpoint: channelEndpoints[ch.id] || DEFAULT_ENDPOINT,
    }))

    fetchMutation.mutate({ upstreams, timeout: 10 })
  }

  const buildAnalysisRequest = (): PricingAnalysisRequest => {
    const selectedChannels = channels.filter((ch) =>
      selectedChannelIds.includes(ch.id)
    )
    // First paint has no channel selection. Use the public models.dev preset
    // as a reference so costs show automatically without manual setup.
    const effectiveChannels =
      selectedChannels.length === 0
        ? channels.filter((ch) => ch.id === MODELS_DEV_PRESET_ID)
        : selectedChannels
    const upstreams: UpstreamConfig[] = effectiveChannels.map((ch) => ({
      id: ch.id,
      name: ch.name,
      base_url: ch.base_url,
      endpoint: channelEndpoints[ch.id] || getDefaultEndpointForChannel(ch),
    }))
    const now = Math.floor(Date.now() / 1000)
    const start = now - analysisDays * 86400
    return {
      upstreams,
      timeout: 10,
      vendor: analysisVendor,
      start,
      end: now,
      windows: [8192, 32768, 131072, 200000, 1000000],
    }
  }

  const handleRefreshAnalysis = async () => {
    setAnalysisLoading(true)
    setAnalysisError(null)
    try {
      const request = buildAnalysisRequest()
      // /analysis reads the latest /fetch snapshot; refresh it first so the
      // comparison reflects current upstream prices after channel selection.
      // Skip when the caller just fetched (confirm-selection flow) to avoid two
      // overlapping /fetch calls racing on the same snapshot batch second.
      const justFetched = Date.now() - lastFetchAtRef.current < 5000
      if (request.upstreams.length > 0 && !justFetched) {
        try {
          await fetchUpstreamRatios({ upstreams: request.upstreams, timeout: request.timeout })
        } catch {
          // Keep the last known snapshot; analysis still shows local prices.
        }
      }
      const analysis = await fetchPricingAnalysis(request)
      if (!analysis.success) {
        const message = analysis.message || t('Failed to load analysis')
        setAnalysisError(message)
        toast.error(message)
        setAnalysisData(null)
        setFluctuationChanges([])
        return
      }
      setAnalysisData(analysis.data)

      const fluctuations = await getPriceFluctuations(analysisThreshold)
      if (!fluctuations.success) {
        toast.error(fluctuations.message || t('Failed to load analysis'))
        setFluctuationChanges([])
        return
      }
      setFluctuationChanges(fluctuations.data.changes ?? [])
      try {
        sessionStorage.setItem(
          ANALYSIS_CACHE_KEY,
          JSON.stringify({
            data: analysis.data,
            changes: fluctuations.data.changes ?? [],
            savedAt: Date.now(),
            signature: {
              vendor: analysisVendor,
              days: analysisDays,
              threshold: analysisThreshold,
              channelIds: effectiveChannelIds,
            },
          } satisfies CachedAnalysis)
        )
      } catch {
        // Storage may be unavailable; analysis still works without cache.
      }
    } catch (error) {
      const message =
        error instanceof Error ? error.message : t('Failed to load analysis')
      setAnalysisError(message)
      toast.error(message)
      setAnalysisData(null)
      setFluctuationChanges([])
    } finally {
      setAnalysisLoading(false)
    }
  }

  const handleDaysChange = (days: number) => {
    setAnalysisDays(days)
  }

  const handleVendorChange = (vendor: number) => {
    setAnalysisVendor(vendor)
  }

  const handleThresholdChange = (threshold: number) => {
    setAnalysisThreshold(threshold)
  }


  const refreshRef = useRef(handleRefreshAnalysis)
  useEffect(() => {
    refreshRef.current = handleRefreshAnalysis
  })

  useEffect(() => {
    if (analysisLoadedRef.current || channels.length === 0) return
    analysisLoadedRef.current = true
    // Reuse cache only if fresh AND produced with the current filters;
    // otherwise we would show data for the wrong vendor/day/threshold.
    const cached = readCachedAnalysis()
    if (
      cached &&
      Date.now() - cached.savedAt < ANALYSIS_REFRESH_MS &&
      cached.signature.vendor === analysisVendor &&
      cached.signature.days === analysisDays &&
      cached.signature.threshold === analysisThreshold &&
      JSON.stringify(cached.signature.channelIds) ===
        JSON.stringify(effectiveChannelIds)
    ) {
      return
    }
    void handleRefreshAnalysis()
  }, [
    handleRefreshAnalysis,
    channels.length,
    analysisVendor,
    analysisDays,
    analysisThreshold,
    effectiveChannelIds,
  ])

  useEffect(() => {
    const id = window.setInterval(() => {
      if (!document.hidden) void refreshRef.current()
    }, ANALYSIS_REFRESH_MS)
    return () => window.clearInterval(id)
  }, [])
  useEffect(() => {
    const lastFilters = lastFiltersRef.current
    if (
      lastFilters.vendor === analysisVendor &&
      lastFilters.days === analysisDays &&
      lastFilters.threshold === analysisThreshold
    ) {
      return
    }
    lastFiltersRef.current = {
      vendor: analysisVendor,
      days: analysisDays,
      threshold: analysisThreshold,
    }
    void handleRefreshAnalysis()
  }, [analysisVendor, analysisDays, analysisThreshold, handleRefreshAnalysis])

  const isLoading = fetchMutation.isPending || analysisLoading
  const isMobile = useIsMobile()
  // Floating actions exist for narrow screens where the table toolbar scrolls
  // out of reach. On desktop they overlap content without adding anything, and
  // they must never float above an open dialog.
  const showFloatingActions = isMobile && !channelDialogOpen && !selectedAnalysisRow
  const unpricedChannels = (analysisData?.rows ?? []).reduce(
    (n, row) =>
      n +
      (row.channels ?? []).filter(
        (ch) => ch.price_mode === '' && ch.billing_mode === ''
      ).length,
    0
  )

  return (
    <div className='flex h-full min-h-0 flex-col gap-4'>
      {(selectedChannelIds.length === 0 || unpricedChannels > 0) && (
        <div className='rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-400'>
          {selectedChannelIds.length === 0
            ? t(
                'No sync channel selected: costs come from each channel\'s last fetched snapshot. Click "Select Sync Channels" and confirm to fetch current prices.'
              )
            : t(
                '{{count}} channel bindings have no upstream price yet. Select those channels and confirm to fetch their prices.',
                { count: unpricedChannels }
              )}
        </div>
      )}
      <SettingsPageActionsPortal>
        <Button
          size='sm'
          className='max-sm:px-2'
          onClick={handleOpenChannelDialog}
          disabled={isLoading}
        >
          <RefreshCcw className='mr-2 h-4 w-4 max-sm:hidden' />
          {t('Select Sync Channels')}
        </Button>
      </SettingsPageActionsPortal>
      <div className='flex min-h-0 flex-1 flex-col'>
        <PricingAnalysisTable
          data={analysisData}
          changes={fluctuationChanges}
          isLoading={analysisLoading}
          error={analysisError}
          vendor={analysisVendor}
          days={analysisDays}
          threshold={analysisThreshold}
          onVendorChange={handleVendorChange}
          onDaysChange={handleDaysChange}
          onThresholdChange={handleThresholdChange}
          onRefresh={handleRefreshAnalysis}
          onRowClick={setSelectedAnalysisRow}
        />
      </div>

      <ChannelSelectorDialog
        open={channelDialogOpen}
        onOpenChange={setChannelDialogOpen}
        channels={channels}
        selectedChannelIds={selectedChannelIds}
        onSelectedChannelIdsChange={setSelectedChannelIds}
        channelEndpoints={channelEndpoints}
        onChannelEndpointsChange={setChannelEndpoints}
        onConfirm={handleConfirmChannelSelection}
      />

      <PricingAnalysisDialog
        row={selectedAnalysisRow}
        groupRatios={analysisData?.group_ratios ?? {}}
        onOpenChange={(open) => {
          if (!open) setSelectedAnalysisRow(null)
        }}
      />
      {showFloatingActions && (
      <div className='fixed right-4 bottom-6 z-30 flex flex-col gap-2'>
        <Button
          size='icon'
          variant='secondary'
          className='rounded-full shadow-lg'
          aria-label={t('Back to top')}
          onClick={() => window.scrollTo({ top: 0, behavior: 'smooth' })}
        >
          <ArrowUp className='h-4 w-4' />
        </Button>
        <Button
          size='icon'
          className='rounded-full shadow-lg'
          aria-label={t('Refresh Analysis')}
          onClick={handleRefreshAnalysis}
          disabled={analysisLoading}
        >
          <RefreshCcw className={cn('h-4 w-4', analysisLoading && 'animate-spin')} />
        </Button>
      </div>
      )}
    </div>
  )
}

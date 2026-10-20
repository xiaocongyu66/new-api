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
import type {
  ColumnFiltersState,
  OnChangeFn,
  PaginationState,
  VisibilityState,
  SortingState,
} from '@tanstack/react-table'
import { Plus, Trash2 } from 'lucide-react'
import {
  useState,
  useMemo,
  memo,
  useCallback,
  useEffect,
  forwardRef,
  useImperativeHandle,
  useRef,
} from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

import {
  DataTableToolbar,
  DataTablePagination,
  useDataTable,
} from '@/components/data-table'
import { Button } from '@/components/ui/button'
import { StatusBadge } from '@/components/status-badge'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@/components/ui/dialog'
import { combineBillingExpr } from '@/features/pricing/lib/billing-expr'

import { safeJsonParse } from '../utils/json-parser'
import { formatPricingDisplay } from './pricing-format'
import {
  createInitialLaneState,
  type PricingMode,
} from './model-pricing-core'
import {
  ModelPricingEditorPanel,
  type ModelPricingEditorPanelHandle,
  type ModelRatioData,
} from './model-pricing-sheet'
import {
  buildModelSnapshots,
  getModeLabel,
  getSnapshotSignature,
  isBasePricingUnset,
  parseCompletionRatioMeta,
  resolveEffectiveCompletionRatio,
  type ModelRow,
} from './model-pricing-snapshots'
import { buildModelRatioColumns } from './model-ratio-table-columns'

type ModelRatioVisualEditorProps = {
  savedModelPrice: string
  savedModelRatio: string
  savedCacheRatio: string
  savedCreateCacheRatio: string
  savedCompletionRatio: string
  savedImageRatio: string
  savedAudioRatio: string
  savedAudioCompletionRatio: string
  savedBillingMode: string
  savedBillingExpr: string
  // Backend-computed effective completion ratio per model (JSON). Read-only.
  completionRatioMeta?: string
  modelPrice: string
  modelRatio: string
  cacheRatio: string
  createCacheRatio: string
  completionRatio: string
  imageRatio: string
  audioRatio: string
  audioCompletionRatio: string
  billingMode: string
  billingExpr: string
  candidateModelNames?: string[]
  candidateModelsLoading?: boolean
  onChange: (field: string, value: string) => void
  onSave: () => void | Promise<void>
  isSaving: boolean
}

export type ModelRatioVisualEditorHandle = {
  commitOpenEditor: () => Promise<boolean>
}

const STORAGE_KEY = 'model-ratio-column-visibility'

const ModelRatioVisualEditorComponent = forwardRef<
  ModelRatioVisualEditorHandle,
  ModelRatioVisualEditorProps
>(function ModelRatioVisualEditor(
  {
    savedModelPrice,
    savedModelRatio,
    savedCacheRatio,
    savedCreateCacheRatio,
    savedCompletionRatio,
    savedImageRatio,
    savedAudioRatio,
    savedAudioCompletionRatio,
    savedBillingMode,
    savedBillingExpr,
    completionRatioMeta,
    modelPrice,
    modelRatio,
    cacheRatio,
    createCacheRatio,
    completionRatio,
    imageRatio,
    audioRatio,
    audioCompletionRatio,
    billingMode,
    billingExpr,
    candidateModelNames,
    candidateModelsLoading,
    onChange,
    onSave,
    isSaving,
  },
  ref
) {
  const { t } = useTranslation()
  const [editorOpen, setEditorOpen] = useState(false)
  const [editData, setEditData] = useState<ModelRatioData | null>(null)
  const [sorting, setSorting] = useState<SortingState>([])
  const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([])
  const [globalFilter, setGlobalFilter] = useState('')
  const editorPanelRef = useRef<ModelPricingEditorPanelHandle>(null)
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 20,
  })
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>(
    () => {
      const saved = localStorage.getItem(STORAGE_KEY)
      if (saved) {
        try {
          return safeJsonParse<VisibilityState>(saved, {
            fallback: {
              cacheRatio: false,
              createCacheRatio: false,
              imageRatio: false,
              audioRatio: false,
              audioCompletionRatio: false,
            },
            silent: true,
          })
        } catch {
          return {
            cacheRatio: false,
            createCacheRatio: false,
            imageRatio: false,
            audioRatio: false,
            audioCompletionRatio: false,
          }
        }
      }
      return {
        cacheRatio: false,
        createCacheRatio: false,
        imageRatio: false,
        audioRatio: false,
        audioCompletionRatio: false,
      }
    }
  )

  useEffect(() => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(columnVisibility))
  }, [columnVisibility])

  const completionRatioMetaMap = useMemo(
    () => parseCompletionRatioMeta(completionRatioMeta),
    [completionRatioMeta]
  )

  const models = useMemo(() => {
    const savedRows = buildModelSnapshots({
      modelPrice: savedModelPrice,
      modelRatio: savedModelRatio,
      cacheRatio: savedCacheRatio,
      createCacheRatio: savedCreateCacheRatio,
      completionRatio: savedCompletionRatio,
      imageRatio: savedImageRatio,
      audioRatio: savedAudioRatio,
      audioCompletionRatio: savedAudioCompletionRatio,
      billingMode: savedBillingMode,
      billingExpr: savedBillingExpr,
    })
    const draftRows = buildModelSnapshots({
      modelPrice,
      modelRatio,
      cacheRatio,
      createCacheRatio,
      completionRatio,
      imageRatio,
      audioRatio,
      audioCompletionRatio,
      billingMode,
      billingExpr,
    })

    const savedByName = new Map(savedRows.map((row) => [row.name, row]))
    const draftByName = new Map(draftRows.map((row) => [row.name, row]))
    const modelNames = new Set([
      ...savedByName.keys(),
      ...draftByName.keys(),
      ...(candidateModelNames ?? []),
    ])

    return [...modelNames]
      .map((name) => {
        const saved = savedByName.get(name)
        const draft = draftByName.get(name)
        const displayed = saved ??
          draft ?? { name, billingMode: 'per-token', hasConflict: false }
        const savedSignature = getSnapshotSignature(saved)
        const draftSignature = getSnapshotSignature(draft)

        return {
          ...displayed,
          saved,
          draft,
          isDraftChanged: savedSignature !== draftSignature,
          isDraftDeleted: Boolean(saved && !draft),
          isDraftNew: Boolean(!saved && draft),
          isPriceUnset: isBasePricingUnset(saved),
        }
      })
      .filter((row) => !row.isDraftDeleted)
      .sort((a, b) => a.name.localeCompare(b.name))
  }, [
    candidateModelNames,
    savedModelPrice,
    savedModelRatio,
    savedCacheRatio,
    savedCreateCacheRatio,
    savedCompletionRatio,
    savedImageRatio,
    savedAudioRatio,
    savedAudioCompletionRatio,
    savedBillingMode,
    savedBillingExpr,
    modelPrice,
    modelRatio,
    cacheRatio,
    createCacheRatio,
    completionRatio,
    imageRatio,
    audioRatio,
    audioCompletionRatio,
    billingMode,
    billingExpr,
  ])

  const modeCounts = useMemo(
    () =>
      models.reduce(
        (acc, model) => {
          const mode =
            model.billingMode === 'per-request' ||
            model.billingMode === 'tiered_expr'
              ? model.billingMode
              : 'per-token'
          acc[mode] += 1
          return acc
        },
        {
          'per-token': 0,
          'per-request': 0,
          tiered_expr: 0,
        } as Record<'per-token' | 'per-request' | 'tiered_expr', number>
      ),
    [models]
  )

  const handleEdit = useCallback(
    (model: ModelRow) => {
      const editableModel = model.draft ?? model.saved ?? model
      let editBillingMode: PricingMode = 'per-token'
      if (editableModel.billingMode === 'tiered_expr') {
        editBillingMode = 'tiered_expr'
      } else if (editableModel.price && editableModel.price !== '') {
        editBillingMode = 'per-request'
      }
      const effectiveCompletionRatio = resolveEffectiveCompletionRatio(
        editableModel.name,
        completionRatioMetaMap,
        editableModel.completionRatio
      )
      setEditData({
        name: editableModel.name,
        price: editableModel.price,
        ratio: editableModel.ratio,
        cacheRatio: editableModel.cacheRatio,
        createCacheRatio: editableModel.createCacheRatio,
        completionRatio: editableModel.completionRatio,
        imageRatio: editableModel.imageRatio,
        audioRatio: editableModel.audioRatio,
        audioCompletionRatio: editableModel.audioCompletionRatio,
        billingMode: editBillingMode,
        billingExpr: editableModel.billingExpr,
        requestRuleExpr: editableModel.requestRuleExpr,
        completionRatioEffective:
          effectiveCompletionRatio !== null
            ? String(effectiveCompletionRatio)
            : '',
        completionRatioLocked:
          completionRatioMetaMap[editableModel.name]?.locked ?? false,
      })
      setEditorOpen(true)
    },
    [completionRatioMetaMap]
  )

  const handleAdd = useCallback(() => {
    setEditData(null)
    setEditorOpen(true)
  }, [])

  const handleGlobalFilterChange = useCallback<OnChangeFn<string>>(
    (updater) => {
      setGlobalFilter((previous) => {
        const next = typeof updater === 'function' ? updater(previous) : updater
        if (next !== previous) {
          setEditData(null)
          setEditorOpen(false)
        }
        return next
      })
    },
    []
  )

  const handleDelete = useCallback(
    (name: string) => {
      const priceMap = safeJsonParse<Record<string, number>>(modelPrice, {
        fallback: {},
        silent: true,
      })
      const ratioMap = safeJsonParse<Record<string, number>>(modelRatio, {
        fallback: {},
        silent: true,
      })
      const cacheMap = safeJsonParse<Record<string, number>>(cacheRatio, {
        fallback: {},
        silent: true,
      })
      const createCacheMap = safeJsonParse<Record<string, number>>(
        createCacheRatio,
        { fallback: {}, silent: true }
      )
      const completionMap = safeJsonParse<Record<string, number>>(
        completionRatio,
        { fallback: {}, silent: true }
      )
      const imageMap = safeJsonParse<Record<string, number>>(imageRatio, {
        fallback: {},
        silent: true,
      })
      const audioMap = safeJsonParse<Record<string, number>>(audioRatio, {
        fallback: {},
        silent: true,
      })
      const audioCompletionMap = safeJsonParse<Record<string, number>>(
        audioCompletionRatio,
        { fallback: {}, silent: true }
      )
      const billingModeMap = safeJsonParse<Record<string, string>>(
        billingMode,
        { fallback: {}, silent: true }
      )
      const billingExprMap = safeJsonParse<Record<string, string>>(
        billingExpr,
        { fallback: {}, silent: true }
      )

      delete priceMap[name]
      delete ratioMap[name]
      delete cacheMap[name]
      delete createCacheMap[name]
      delete completionMap[name]
      delete imageMap[name]
      delete audioMap[name]
      delete audioCompletionMap[name]
      delete billingModeMap[name]
      delete billingExprMap[name]

      onChange('ModelPrice', JSON.stringify(priceMap, null, 2))
      onChange('ModelRatio', JSON.stringify(ratioMap, null, 2))
      onChange('CacheRatio', JSON.stringify(cacheMap, null, 2))
      onChange('CreateCacheRatio', JSON.stringify(createCacheMap, null, 2))
      onChange('CompletionRatio', JSON.stringify(completionMap, null, 2))
      onChange('ImageRatio', JSON.stringify(imageMap, null, 2))
      onChange('AudioRatio', JSON.stringify(audioMap, null, 2))
      onChange(
        'AudioCompletionRatio',
        JSON.stringify(audioCompletionMap, null, 2)
      )
      onChange(
        'billing_setting.billing_mode',
        JSON.stringify(billingModeMap, null, 2)
      )
      onChange(
        'billing_setting.billing_expr',
        JSON.stringify(billingExprMap, null, 2)
      )

      if (editData?.name === name) {
        setEditData(null)
        setEditorOpen(false)
      }
    },
    [
      modelPrice,
      modelRatio,
      cacheRatio,
      createCacheRatio,
      completionRatio,
      imageRatio,
      audioRatio,
      audioCompletionRatio,
      billingMode,
      billingExpr,
      onChange,
      editData,
    ]
  )

  const handleEditorSave = useCallback(async () => {
    try {
      await onSave?.()
      setEditorOpen(false)
    } catch {
      // Keep the editor open if saving fails.
    }
  }, [onSave])

  const columns = useMemo(
    () =>
      buildModelRatioColumns({
        onEdit: handleEdit,
        t,
      }),
    [handleEdit, t]
  )

  const ensurePageInRange = useCallback((pageCount: number) => {
    setPagination((prev) =>
      pageCount > 0 && prev.pageIndex >= pageCount
        ? { ...prev, pageIndex: pageCount - 1 }
        : prev
    )
  }, [])

  const { table } = useDataTable({
    data: models,
    columns,
    getRowId: (row) => row.name,
    ensurePageInRange,
    sorting,
    columnFilters,
    globalFilter,
    columnVisibility,
    pagination,
    enableRowSelection: false,
    onSortingChange: setSorting,
    onColumnFiltersChange: setColumnFilters,
    onGlobalFilterChange: handleGlobalFilterChange,
    onColumnVisibilityChange: setColumnVisibility,
    onPaginationChange: setPagination,
    autoResetPageIndex: false,
    globalFilterFn: (row, _columnId, filterValue) => {
      const searchValue = String(filterValue).toLowerCase()
      return row.original.name.toLowerCase().includes(searchValue)
    },
  })

  const persistPricingData = useCallback(
    (data: ModelRatioData) => {
      const priceMap = safeJsonParse<Record<string, number>>(modelPrice, {
        fallback: {},
        silent: true,
      })
      const ratioMap = safeJsonParse<Record<string, number>>(modelRatio, {
        fallback: {},
        silent: true,
      })
      const cacheMap = safeJsonParse<Record<string, number>>(cacheRatio, {
        fallback: {},
        silent: true,
      })
      const createCacheMap = safeJsonParse<Record<string, number>>(
        createCacheRatio,
        { fallback: {}, silent: true }
      )
      const completionMap = safeJsonParse<Record<string, number>>(
        completionRatio,
        { fallback: {}, silent: true }
      )
      const imageMap = safeJsonParse<Record<string, number>>(imageRatio, {
        fallback: {},
        silent: true,
      })
      const audioMap = safeJsonParse<Record<string, number>>(audioRatio, {
        fallback: {},
        silent: true,
      })
      const audioCompletionMap = safeJsonParse<Record<string, number>>(
        audioCompletionRatio,
        { fallback: {}, silent: true }
      )
      const billingModeMap = safeJsonParse<Record<string, string>>(
        billingMode,
        { fallback: {}, silent: true }
      )
      const billingExprMap = safeJsonParse<Record<string, string>>(
        billingExpr,
        { fallback: {}, silent: true }
      )

      const setIfPresent = (
        target: Record<string, number>,
        key: string,
        value: string | undefined
      ) => {
        if (!value || value === '') return
        const parsed = parseFloat(value)
        if (Number.isFinite(parsed)) target[key] = parsed
      }

      const modelName = data.name

      delete priceMap[modelName]
      delete ratioMap[modelName]
      delete cacheMap[modelName]
      delete createCacheMap[modelName]
      delete completionMap[modelName]
      delete imageMap[modelName]
      delete audioMap[modelName]
      delete audioCompletionMap[modelName]
      delete billingModeMap[modelName]
      delete billingExprMap[modelName]

      if (data.billingMode === 'tiered_expr') {
        const combined = combineBillingExpr(
          data.billingExpr || '',
          data.requestRuleExpr || ''
        )
        if (combined) {
          billingModeMap[modelName] = 'tiered_expr'
          billingExprMap[modelName] = combined
        }
        // Always serialize ratio/price values for tiered_expr models so they
        // serve as fallback during multi-instance sync delays. The backend's
        // ModelPriceHelper checks billing_mode first, so these values are
        // only consulted when billing_setting hasn't propagated yet.
        setIfPresent(priceMap, modelName, data.price)
        setIfPresent(ratioMap, modelName, data.ratio)
        setIfPresent(cacheMap, modelName, data.cacheRatio)
        setIfPresent(createCacheMap, modelName, data.createCacheRatio)
        setIfPresent(completionMap, modelName, data.completionRatio)
        setIfPresent(imageMap, modelName, data.imageRatio)
        setIfPresent(audioMap, modelName, data.audioRatio)
        setIfPresent(audioCompletionMap, modelName, data.audioCompletionRatio)
      } else if (data.price && data.price !== '') {
        setIfPresent(priceMap, modelName, data.price)
      } else {
        setIfPresent(ratioMap, modelName, data.ratio)
        setIfPresent(cacheMap, modelName, data.cacheRatio)
        setIfPresent(createCacheMap, modelName, data.createCacheRatio)
        setIfPresent(completionMap, modelName, data.completionRatio)
        setIfPresent(imageMap, modelName, data.imageRatio)
        setIfPresent(audioMap, modelName, data.audioRatio)
        setIfPresent(audioCompletionMap, modelName, data.audioCompletionRatio)
      }

      onChange('ModelPrice', JSON.stringify(priceMap, null, 2))
      onChange('ModelRatio', JSON.stringify(ratioMap, null, 2))
      onChange('CacheRatio', JSON.stringify(cacheMap, null, 2))
      onChange('CreateCacheRatio', JSON.stringify(createCacheMap, null, 2))
      onChange('CompletionRatio', JSON.stringify(completionMap, null, 2))
      onChange('ImageRatio', JSON.stringify(imageMap, null, 2))
      onChange('AudioRatio', JSON.stringify(audioMap, null, 2))
      onChange(
        'AudioCompletionRatio',
        JSON.stringify(audioCompletionMap, null, 2)
      )
      onChange(
        'billing_setting.billing_mode',
        JSON.stringify(billingModeMap, null, 2)
      )
      onChange(
        'billing_setting.billing_expr',
        JSON.stringify(billingExprMap, null, 2)
      )
    },
    [
      modelPrice,
      modelRatio,
      cacheRatio,
      createCacheRatio,
      completionRatio,
      imageRatio,
      audioRatio,
      audioCompletionRatio,
      billingMode,
      billingExpr,
      onChange,
    ]
  )

  useImperativeHandle(
    ref,
    () => ({
      commitOpenEditor: async () => {
        if (!editorOpen || !editorPanelRef.current) return true
        const data = await editorPanelRef.current.commitDraft()
        if (!data) return false
        persistPricingData(data)
        setEditData(data)
        return true
      },
    }),
    [editorOpen, persistPricingData]
  )

  const hasRows = table.getRowModel().rows.length > 0

  let emptyStateText = candidateModelsLoading
    ? t('Loading...')
    : t('No models configured. Use Add model to get started.')
  if (table.getState().globalFilter) {
    emptyStateText = t('No models match your search')
  }

  return (
    <div className='flex flex-col gap-4'>
      <DataTableToolbar
        table={table}
        searchPlaceholder={t('Search models...')}
        searchDebounceMs={250}
        filters={[
          {
            columnId: 'billingMode',
            title: t('Mode'),
            options: [
              {
                label: 'Per-token',
                value: 'per-token',
                count: modeCounts['per-token'],
              },
              {
                label: 'Per-request',
                value: 'per-request',
                count: modeCounts['per-request'],
              },
              {
                label: 'Expression',
                value: 'tiered_expr',
                count: modeCounts.tiered_expr,
              },
            ],
          },
        ]}
        preActions={
          <Button onClick={handleAdd}>
            <Plus data-icon='inline-start' />
            {t('Add model')}
          </Button>
        }
      />

      {!hasRows ? (
        <div className='text-muted-foreground rounded-lg border border-dashed p-8 text-center'>
          {emptyStateText}
        </div>
      ) : (
        <div className='grid gap-2.5 sm:grid-cols-2 lg:grid-cols-3'>
          {table.getRowModel().rows.map((row) => {
            const model = row.original
            const isEditing = editData?.name === model.name
            const lanes = createInitialLaneState({
              name: model.name,
              price: model.price,
              ratio: model.ratio,
              cacheRatio: model.cacheRatio,
              createCacheRatio: model.createCacheRatio,
              completionRatio: model.completionRatio,
              imageRatio: model.imageRatio,
              audioRatio: model.audioRatio,
              audioCompletionRatio: model.audioCompletionRatio,
            })

            // Billing charges output tokens at the effective completion
            // ratio, which the backend resolves (DB override → hardcoded
            // default). The DB map alone leaves most models without one.
            const effectiveCompletionRatio = resolveEffectiveCompletionRatio(
              model.name,
              completionRatioMetaMap,
              model.completionRatio
            )
            const effectiveOutputPrice =
              lanes.promptPrice && effectiveCompletionRatio !== null
                ? `$${formatPricingDisplay(
                    Number(lanes.promptPrice) * effectiveCompletionRatio
                  )}`
                : '—'

            const priceEntries: Array<{ label: string; value: string }> =
              model.billingMode === 'tiered_expr'
                ? [
                    { label: t('Input'), value: '—' },
                    { label: t('Output'), value: '—' },
                  ]
                : [
                    {
                      label: t('Input'),
                      value:
                        model.billingMode === 'per-request'
                          ? model.price
                            ? `$${model.price}`
                            : '—'
                          : lanes.promptPrice
                            ? `$${lanes.promptPrice}`
                            : '—',
                    },
                    {
                      label: t('Output'),
                      value:
                        model.billingMode === 'per-request'
                          ? '—'
                          : effectiveOutputPrice,
                    },
                  ]

            return (
              <div
                key={row.id}
                className={cn(
                  'group relative rounded-lg border bg-card shadow-sm transition hover:border-primary/40 hover:shadow-md',
                  model.isPriceUnset &&
                    'border-yellow-300 dark:border-yellow-800',
                  isEditing && 'border-primary bg-primary/5'
                )}
              >
                <button
                  type='button'
                  className='flex h-full w-full flex-col gap-2 p-3 pr-8 text-left'
                  onClick={() => handleEdit(model)}
                >
                  <div className='flex min-w-0 flex-col gap-1'>
                    <span className='flex min-w-0 items-center gap-2 text-sm leading-tight font-semibold'>
                      <span className='min-w-0 truncate'>{model.name}</span>
                      {model.billingMode === 'tiered_expr' && (
                        <StatusBadge
                          label={t('Tiered')}
                          variant='info'
                          copyable={false}
                          className='shrink-0'
                        />
                      )}
                      {model.hasConflict && (
                        <StatusBadge
                          label={t('Conflict')}
                          variant='danger'
                          copyable={false}
                          className='shrink-0'
                        />
                      )}
                    </span>
                    <span className='text-muted-foreground/70 text-[11px]'>
                      {t(getModeLabel(model.billingMode))}
                    </span>
                  </div>

                  <div className='flex flex-col gap-1'>
                    {priceEntries.map((entry) => (
                      <div
                        key={entry.label}
                        className='flex min-w-0 items-baseline justify-between gap-x-3'
                      >
                        <span className='text-foreground shrink-0 text-xs font-normal'>
                          {entry.label}
                        </span>
                        <span className='text-muted-foreground min-w-0 flex-1 truncate text-right text-[11px] font-light tabular-nums'>
                          {entry.value}
                        </span>
                      </div>
                    ))}
                  </div>
                </button>

                {!model.isPriceUnset && (
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon-sm'
                    className='text-muted-foreground/50 absolute top-1.5 right-1.5 hover:text-destructive'
                    onClick={(event) => {
                      event.stopPropagation()
                      handleDelete(model.name)
                    }}
                    aria-label={t('Delete')}
                  >
                    <Trash2 className='h-4 w-4' />
                  </Button>
                )}
              </div>
            )
          })}
        </div>
      )}

      {hasRows && <DataTablePagination table={table} />}

      <Dialog open={editorOpen} onOpenChange={setEditorOpen}>
        <DialogContent className='sm:max-w-4xl w-[95vw] max-h-[90vh] overflow-hidden p-0'>
          <DialogTitle className='sr-only'>
            {editData ? t('Edit model pricing') : t('Add model pricing')}
          </DialogTitle>
          <DialogDescription className='sr-only'>
            {editData?.name || t('New model')}
          </DialogDescription>
          <ModelPricingEditorPanel
            ref={editorPanelRef}
            editData={editData}
            onSave={handleEditorSave}
            isSaving={isSaving}
            className='h-[90vh] min-h-0'
          />
        </DialogContent>
      </Dialog>
    </div>
  )
})

export const ModelRatioVisualEditor = memo(
  ModelRatioVisualEditorComponent,
  // Custom equality check - only re-render if JSON props actually changed
  (prevProps, nextProps) => {
    return (
      prevProps.savedModelPrice === nextProps.savedModelPrice &&
      prevProps.savedModelRatio === nextProps.savedModelRatio &&
      prevProps.savedCacheRatio === nextProps.savedCacheRatio &&
      prevProps.savedCreateCacheRatio === nextProps.savedCreateCacheRatio &&
      prevProps.savedCompletionRatio === nextProps.savedCompletionRatio &&
      prevProps.savedImageRatio === nextProps.savedImageRatio &&
      prevProps.savedAudioRatio === nextProps.savedAudioRatio &&
      prevProps.savedAudioCompletionRatio ===
        nextProps.savedAudioCompletionRatio &&
      prevProps.savedBillingMode === nextProps.savedBillingMode &&
      prevProps.savedBillingExpr === nextProps.savedBillingExpr &&
      prevProps.modelPrice === nextProps.modelPrice &&
      prevProps.modelRatio === nextProps.modelRatio &&
      prevProps.cacheRatio === nextProps.cacheRatio &&
      prevProps.createCacheRatio === nextProps.createCacheRatio &&
      prevProps.completionRatio === nextProps.completionRatio &&
      prevProps.imageRatio === nextProps.imageRatio &&
      prevProps.audioRatio === nextProps.audioRatio &&
      prevProps.audioCompletionRatio === nextProps.audioCompletionRatio &&
      prevProps.billingMode === nextProps.billingMode &&
      prevProps.billingExpr === nextProps.billingExpr &&
      prevProps.candidateModelNames === nextProps.candidateModelNames &&
      prevProps.candidateModelsLoading === nextProps.candidateModelsLoading &&
      prevProps.onChange === nextProps.onChange &&
      prevProps.onSave === nextProps.onSave &&
      prevProps.isSaving === nextProps.isSaving
    )
  }
)

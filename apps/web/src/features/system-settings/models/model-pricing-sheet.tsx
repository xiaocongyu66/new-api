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
import { zodResolver } from '@hookform/resolvers/zod'
import { AlertTriangle, Save } from 'lucide-react'
import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useState,
} from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { sideDrawerContentClassName } from '@/components/drawer-layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'

import {
  EMPTY_LANE_ENABLED,
  EMPTY_LANE_PRICES,
  buildPreviewRows,
  createInitialLaneState,
  createModelPricingSchema,
  deriveLanePrice,
  hasValue,
  laneConfigs,
  numericDraftRegex,
  outputLaneConfig,
  ratioFieldByLane,
  toNumberOrNull,
  formatTierValidationError,
  validateContextCallTiersStructured,
  validateContextTiersStructured,
  type TierValidationError,
  type LaneKey,
  type ModelPricingFormValues,
  type ModelRatioData,
  type PricingMode,
} from './model-pricing-core'
import { getModeLabel } from './model-pricing-snapshots'
import { ContextTierTable } from './context-tier-table'
import { PriceInput, PriceLane } from './model-pricing-inputs'
import { formatPricingNumber } from './pricing-format'
import { TieredPricingEditor } from './tiered-pricing-editor'

export type { ModelRatioData } from './model-pricing-core'

type ModelPricingSheetProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  editData?: ModelRatioData | null
  onSave?: () => void | Promise<void>
  isSaving?: boolean
}

type ModelPricingEditorPanelProps = Omit<
  ModelPricingSheetProps,
  'open' | 'onOpenChange'
> & {
  className?: string
}

export type ModelPricingEditorPanelHandle = {
  commitDraft: () => Promise<ModelRatioData | null>
}

export const ModelPricingSheet = forwardRef<
  ModelPricingEditorPanelHandle,
  ModelPricingSheetProps
>(function ModelPricingSheet(
  { open, onOpenChange, editData, onSave, isSaving },
  ref
) {
  const { t } = useTranslation()
  const title = editData ? t('Edit model pricing') : t('Add model pricing')
  const description = editData?.name || t('New model')

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side='right'
        className={sideDrawerContentClassName('sm:max-w-4xl')}
      >
        <SheetHeader className='sr-only'>
          <SheetTitle>{title}</SheetTitle>
          <SheetDescription>{description}</SheetDescription>
        </SheetHeader>
        <ModelPricingEditorPanel
          ref={ref}
          editData={editData}
          onSave={onSave}
          isSaving={isSaving}
          className='h-full rounded-none border-0'
        />
      </SheetContent>
    </Sheet>
  )
})

export const ModelPricingEditorPanel = forwardRef<
  ModelPricingEditorPanelHandle,
  ModelPricingEditorPanelProps
>(function ModelPricingEditorPanel(
  { editData, className, onSave, isSaving },
  ref
) {
  const { t } = useTranslation()
  const [pricingMode, setPricingMode] = useState<PricingMode>('per-token')
  // Which stored method actually bills this model (independent of the tab
  // being edited); persisted into billing_setting.billing_mode.
  const [activeMethod, setActiveMethod] = useState<PricingMode>('per-token')
  const [promptPrice, setPromptPrice] = useState('')
  const [lanePrices, setLanePrices] = useState<Record<LaneKey, string>>({
    ...EMPTY_LANE_PRICES,
  })
  const [laneEnabled, setLaneEnabled] = useState<Record<LaneKey, boolean>>({
    ...EMPTY_LANE_ENABLED,
  })
  const [billingExpr, setBillingExpr] = useState('')
  const [requestRuleExpr, setRequestRuleExpr] = useState('')
  const [contextTiers, setContextTiers] = useState('')
  const [contextTiersError, setContextTiersError] = useState<TierValidationError | null>(null)
  const [contextCallTiers, setContextCallTiers] = useState('')
  const [contextCallTiersError, setContextCallTiersError] = useState<TierValidationError | null>(null)
  const [editorReloadToken, setEditorReloadToken] = useState(0)
  const isEditMode = !!editData
  // The backend owns the default completion ratio; a locked one ignores the
  // stored map, so the output price is not editable for those models.
  const completionRatioLocked = editData?.completionRatioLocked ?? false
  const effectiveCompletionRatio = editData?.completionRatioEffective ?? ''
  // True once the admin typed an output price; until then the output price is
  // derived from the effective completion ratio.
  const [outputPriceOverridden, setOutputPriceOverridden] = useState(false)
  // True once the admin typed an input price; until then the stored ratio is
  // left untouched, because the field only rounds it for display.
  const [inputPriceEdited, setInputPriceEdited] = useState(false)

  const form = useForm<ModelPricingFormValues>({
    resolver: zodResolver(createModelPricingSchema(t)),
    defaultValues: {
      name: '',
      price: '',
      ratio: '',
      cacheRatio: '',
      createCacheRatio: '',
      completionRatio: '',
      imageRatio: '',
      audioRatio: '',
      audioCompletionRatio: '',
    },
  })

  useEffect(() => {
    const nextLaneState = createInitialLaneState(editData)

    if (editData) {
      form.reset({
        name: editData.name,
        price: editData.price || '',
        ratio: editData.ratio || '',
        cacheRatio: editData.cacheRatio || '',
        createCacheRatio: editData.createCacheRatio || '',
        completionRatio: editData.completionRatio || '',
        imageRatio: editData.imageRatio || '',
        audioRatio: editData.audioRatio || '',
        audioCompletionRatio: editData.audioCompletionRatio || '',
      })
      const storedMode: PricingMode =
        editData.billingMode === 'tiered_expr'
          ? 'tiered_expr'
          : editData.billingMode === 'context_tier'
            ? 'context_tier'
            : editData.billingMode === 'context_tier_per_call'
              ? 'context_tier_per_call'
              : editData.price
                ? 'per-request'
                : 'per-token'
      // billingMode stores which method actually bills; the editor opens on it.
      setActiveMethod(storedMode)
      setPricingMode(storedMode)
      setBillingExpr(editData.billingExpr || '')
      setRequestRuleExpr(editData.requestRuleExpr || '')
      setContextTiers(editData.billingContextTier || '')
      setContextCallTiers(editData.billingContextTierPerCall || '')
    } else {
      form.reset({
        name: '',
        price: '',
        ratio: '',
        cacheRatio: '',
        createCacheRatio: '',
        completionRatio: '',
        imageRatio: '',
        audioRatio: '',
        audioCompletionRatio: '',
      })
      setPricingMode('per-token')
      setActiveMethod('per-token')
      setBillingExpr('')
      setRequestRuleExpr('')
      setContextTiers('')
      setContextCallTiers('')
    }
    setContextTiersError(null)
    setContextCallTiersError(null)

    setPromptPrice(nextLaneState.promptPrice)
    setLanePrices(nextLaneState.prices)
    setLaneEnabled(nextLaneState.enabled)
    setOutputPriceOverridden(false)
    setInputPriceEdited(false)
    setEditorReloadToken((token) => token + 1)
  }, [editData, form])

  const setFormValue = (field: keyof ModelPricingFormValues, value: string) => {
    form.setValue(field, value, {
      shouldDirty: true,
      shouldValidate: true,
    })
  }

  const deriveLaneRatio = (
    lane: LaneKey,
    price: string,
    nextPromptPrice = promptPrice,
    nextLanePrices = lanePrices
  ) => {
    const priceNumber = toNumberOrNull(price)
    if (priceNumber === null) return ''

    if (lane === 'audioOutput') {
      const audioInputPrice = toNumberOrNull(nextLanePrices.audioInput)
      if (audioInputPrice === null || audioInputPrice === 0) return ''
      return formatPricingNumber(priceNumber / audioInputPrice)
    }

    const inputPrice = toNumberOrNull(nextPromptPrice)
    if (inputPrice === null || inputPrice === 0) return ''
    return formatPricingNumber(priceNumber / inputPrice)
  }

  const syncLaneRatios = (
    nextPromptPrice = promptPrice,
    nextLanePrices = lanePrices,
    nextLaneEnabled = laneEnabled,
    rewriteInputRatio = inputPriceEdited
  ) => {
    const inputPrice = toNumberOrNull(nextPromptPrice)
    // The input price field is rounded for display, so only rewrite the stored
    // ratio once the admin actually typed one; otherwise the ratio the model
    // was saved with keeps its full precision.
    if (rewriteInputRatio) {
      setFormValue(
        'ratio',
        inputPrice !== null ? formatPricingNumber(inputPrice / 2) : ''
      )
    }

    laneConfigs.forEach(({ key }) => {
      const ratioField = ratioFieldByLane[key]
      if (!nextLaneEnabled[key]) {
        setFormValue(ratioField, '')
        return
      }
      setFormValue(
        ratioField,
        deriveLaneRatio(
          key,
          nextLanePrices[key],
          nextPromptPrice,
          nextLanePrices
        )
      )
    })
  }

  const handlePromptPriceChange = (value: string) => {
    if (!numericDraftRegex.test(value)) return
    setPromptPrice(value)
    setInputPriceEdited(true)

    // The backend default is a multiplier of the input price, so a derived
    // output price follows the input price.
    const nextLanePrices = { ...lanePrices }
    if (!outputPriceOverridden) {
      nextLanePrices.completion = deriveLanePrice(
        effectiveCompletionRatio,
        value
      )
      setLanePrices(nextLanePrices)
    }

    syncLaneRatios(value, nextLanePrices, laneEnabled, true)
  }

  const handleOutputPriceChange = (value: string) => {
    if (!numericDraftRegex.test(value)) return
    const nextLanePrices = { ...lanePrices, completion: value }
    setLanePrices(nextLanePrices)
    setOutputPriceOverridden(true)
    setLaneEnabled({ ...laneEnabled, completion: true })
    setFormValue(
      'completionRatio',
      deriveLaneRatio('completion', value, promptPrice, nextLanePrices)
    )
  }

  const handleLanePriceChange = (lane: LaneKey, value: string) => {
    if (!numericDraftRegex.test(value)) return
    const nextLanePrices = { ...lanePrices, [lane]: value }
    setLanePrices(nextLanePrices)

    if (laneEnabled[lane]) {
      setFormValue(
        ratioFieldByLane[lane],
        deriveLaneRatio(lane, value, promptPrice, nextLanePrices)
      )
    }

    if (lane === 'audioInput' && laneEnabled.audioOutput) {
      setFormValue(
        'audioCompletionRatio',
        deriveLaneRatio(
          'audioOutput',
          nextLanePrices.audioOutput,
          promptPrice,
          nextLanePrices
        )
      )
    }
  }

  const handleLaneToggle = (lane: LaneKey, checked: boolean) => {
    const nextEnabled = { ...laneEnabled, [lane]: checked }
    let nextPrices = lanePrices

    if (!checked) {
      nextPrices = { ...nextPrices, [lane]: '' }
      setFormValue(ratioFieldByLane[lane], '')
      if (lane === 'audioInput') {
        nextEnabled.audioOutput = false
        nextPrices.audioOutput = ''
        setFormValue('audioCompletionRatio', '')
      }
    }

    setLaneEnabled(nextEnabled)
    setLanePrices(nextPrices)

    if (checked) {
      setFormValue(
        ratioFieldByLane[lane],
        deriveLaneRatio(lane, nextPrices[lane], promptPrice, nextPrices)
      )
    }
  }

  const handleModeChange = (value: string) => {
    const nextMode = value as PricingMode
    setPricingMode(nextMode)
    if (nextMode === 'tiered_expr' && !billingExpr) {
      setBillingExpr('tier("base", p * 0 + c * 0)')
    }
  }

  const watchedValues = form.watch()
  const previewRows = useMemo(
    () =>
      [
        {
          key: 'active-method',
          label: t('Active billing method'),
          multiline: true,
          value:
            activeMethod === pricingMode
              ? t(getModeLabel(activeMethod))
              : t(
                  'Editing {{edited}}; requests bill with {{active}}.',
                  {
                    edited: t(getModeLabel(pricingMode)),
                    active: t(getModeLabel(activeMethod)),
                  }
                ),
        },
        ...buildPreviewRows(
          watchedValues,
          pricingMode,
          billingExpr,
          requestRuleExpr,
          promptPrice,
          lanePrices,
          laneEnabled,
          contextTiers,
          contextCallTiers,
          t
        ),
      ],
    [
      activeMethod,
      billingExpr,
      contextCallTiers,
      contextTiers,
      laneEnabled,
      lanePrices,
      pricingMode,
      promptPrice,
      requestRuleExpr,
      t,
      watchedValues,
    ]
  )

  const warnings = useMemo(() => {
    const nextWarnings: string[] = []
    const hasConflict =
      !!editData?.price &&
      [
        editData.ratio,
        editData.completionRatio,
        editData.cacheRatio,
        editData.createCacheRatio,
        editData.imageRatio,
        editData.audioRatio,
        editData.audioCompletionRatio,
      ].some(hasValue)

    if (hasConflict) {
      nextWarnings.push(
        t(
          'This model has both fixed-price and token-price settings. Saving the current mode will rewrite the conflicting fields.'
        )
      )
    }

    if (
      pricingMode === 'per-token' &&
      toNumberOrNull(promptPrice) === null &&
      ((laneEnabled.completion && hasValue(lanePrices.completion)) ||
        laneConfigs.some(
          ({ key }) => laneEnabled[key] && hasValue(lanePrices[key])
        ))
    ) {
      nextWarnings.push(
        t('Input price is required before saving dependent prices.')
      )
    }

    if (
      pricingMode === 'per-token' &&
      laneEnabled.audioOutput &&
      !hasValue(lanePrices.audioInput)
    ) {
      nextWarnings.push(t('Audio output price requires an audio input price.'))
    }

    // Active-method fallback warning: picking a method with no stored prices
    // bills plain per-token ratios instead. The validators return null when a
    // config is valid, so warn on a non-null result.
    if (
      activeMethod === 'context_tier' &&
      validateContextTiersStructured(contextTiers) !== null
    ) {
      nextWarnings.push(
        t(
          'Active method has no stored tier prices; requests will fall back to per-token ratios.'
        )
      )
    }
    if (
      activeMethod === 'context_tier_per_call' &&
      validateContextCallTiersStructured(contextCallTiers) !== null
    ) {
      nextWarnings.push(
        t(
          'Active method has no stored per-call prices; requests will fall back to per-token ratios.'
        )
      )
    }

    return nextWarnings
  }, [
    activeMethod,
    contextCallTiers,
    contextTiers,
    editData,
    laneEnabled,
    lanePrices,
    pricingMode,
    promptPrice,
    t,
  ])

  const validatePricingValues = useCallback(() => {
    if (
      pricingMode === 'per-token' &&
      toNumberOrNull(promptPrice) === null &&
      ((laneEnabled.completion && hasValue(lanePrices.completion)) ||
        laneConfigs.some(
          ({ key }) => laneEnabled[key] && hasValue(lanePrices[key])
        ))
    ) {
      form.setError('ratio', {
        message: t('Input price is required before saving dependent prices.'),
      })
      return false
    }

    if (
      pricingMode === 'per-token' &&
      laneEnabled.audioOutput &&
      !hasValue(lanePrices.audioInput)
    ) {
      form.setError('audioRatio', {
        message: t('Audio output price requires an audio input price.'),
      })
      return false
    }

    if (pricingMode === 'context_tier') {
      const tiersError = validateContextTiersStructured(contextTiers)
      setContextTiersError(tiersError)
      if (tiersError) return false
    }

    if (pricingMode === 'context_tier_per_call') {
      const callTiersError = validateContextCallTiersStructured(contextCallTiers)
      setContextCallTiersError(callTiersError)
      if (callTiersError) return false
    }

    return true
  }, [
    form,
    laneEnabled,
    lanePrices,
    pricingMode,
    promptPrice,
    t,
    contextTiers,
    contextCallTiers,
  ])

  const buildSubmitData = useCallback(
    (values: ModelPricingFormValues) => {
      const data: ModelRatioData = {
        name: values.name.trim(),
        // billingMode is the ACTIVE method (what requests bill with), not the
        // tab being edited; every method keeps its own stored prices.
        billingMode: activeMethod,
        price: values.price || '',
        ratio: values.ratio || '',
        cacheRatio: values.cacheRatio || '',
        createCacheRatio: values.createCacheRatio || '',
        completionRatio: values.completionRatio || '',
        imageRatio: values.imageRatio || '',
        audioRatio: values.audioRatio || '',
        audioCompletionRatio: values.audioCompletionRatio || '',
        // Always ship every method's current editor state; an empty value
        // means "this method has no stored prices" and clears its entry.
        billingExpr: billingExpr,
        requestRuleExpr: requestRuleExpr,
        billingContextTier: contextTiers,
        billingContextTierPerCall: contextCallTiers,
      }

      return data
    },
    [
      activeMethod,
      billingExpr,
      requestRuleExpr,
      contextTiers,
      contextCallTiers,
    ]
  )

  useImperativeHandle(
    ref,
    () => ({
      commitDraft: async () => {
        const isValid = await form.trigger()
        if (!isValid || !validatePricingValues()) return null
        return buildSubmitData(form.getValues())
      },
    }),
    [form, validatePricingValues, buildSubmitData]
  )

  const showActions = Boolean(onSave)

  return (
    <div
      className={cn(
        'bg-card-surface flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border',
        className
      )}
    >
      <div className='border-b p-4'>
        <div className='flex flex-wrap items-start justify-between gap-3'>
          <div className='min-w-0'>
            <h3 className='truncate text-base font-medium'>
              {isEditMode ? t('Edit model pricing') : t('Add model pricing')}
            </h3>
          </div>
        </div>
      </div>

      <Form {...form}>
        <form
          onSubmit={(event) => event.preventDefault()}
          className='flex min-h-0 flex-1 flex-col'
          autoComplete='off'
        >
          <div className='min-h-0 flex-1 overflow-y-auto no-scrollbar p-4 pb-6'>
            <div className='grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(220px,260px)]'>
              <FieldGroup>
                {warnings.length > 0 && (
                  <Alert variant='destructive'>
                    <AlertTriangle data-icon='inline-start' />
                    <AlertDescription>
                      <div className='flex flex-col gap-1'>
                        {warnings.map((warning) => (
                          <span key={warning}>{warning}</span>
                        ))}
                      </div>
                    </AlertDescription>
                  </Alert>
                )}

                <FormField
                  control={form.control}
                  name='name'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Model name')}</FormLabel>
                      <FormControl>
                        <Input
                          placeholder={t('gpt-4')}
                          {...field}
                          disabled={isEditMode}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'The exact model identifier as used in API requests.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <div className='flex flex-col gap-4'>
                  <Field>
                    <FieldLabel>{t('Active billing method')}</FieldLabel>
                    <Select
                      value={activeMethod}
                      onValueChange={(v) => {
                        if (typeof v === 'string' && v !== '') {
                          setActiveMethod(v as PricingMode)
                        }
                      }}
                    >
                      <SelectTrigger className='w-full'>
                        <SelectValue>
                          {(value: string) => t(getModeLabel(value))}
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          <SelectItem value='per-token'>
                            {t('Per-token')}
                          </SelectItem>
                          <SelectItem value='per-request'>
                            {t('Per-request')}
                          </SelectItem>
                          <SelectItem value='tiered_expr'>
                            {t('Expression')}
                          </SelectItem>
                          <SelectItem value='context_tier'>
                            {t('Per-token context tiers')}
                          </SelectItem>
                          <SelectItem value='context_tier_per_call'>
                            {t('Per-call context tiers')}
                          </SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FieldDescription>
                      {t(
                        'This method is what requests actually bill with. Every method below keeps its own stored prices; picking one without prices falls back to per-token ratios.'
                      )}
                    </FieldDescription>
                  </Field>

                  <Tabs
                    value={pricingMode}
                    onValueChange={handleModeChange}
                    className='gap-4'
                  >
                    <TabsList className='no-scrollbar flex w-full justify-start overflow-x-auto sm:grid sm:grid-cols-5'>
                      <TabsTrigger
                        value='per-token'
                        className='shrink-0 text-xs whitespace-nowrap sm:text-sm'
                      >
                        {t('Per-token')}
                      </TabsTrigger>
                      <TabsTrigger
                        value='per-request'
                        className='shrink-0 text-xs whitespace-nowrap sm:text-sm'
                      >
                        {t('Per-request')}
                      </TabsTrigger>
                      <TabsTrigger
                        value='tiered_expr'
                        className='shrink-0 text-xs whitespace-nowrap sm:text-sm'
                      >
                        {t('Expression')}
                      </TabsTrigger>
                      <TabsTrigger
                        value='context_tier'
                        className='shrink-0 text-xs whitespace-nowrap sm:text-sm'
                      >
                        {t('Per-token tiers')}
                      </TabsTrigger>
                      <TabsTrigger
                        value='context_tier_per_call'
                        className='shrink-0 text-xs whitespace-nowrap sm:text-sm'
                      >
                        {t('Per-call tiers')}
                      </TabsTrigger>
                    </TabsList>

                  <TabsContent value='per-token' className='pt-0'>
                    <FieldGroup className='gap-5'>
                      <div className='flex flex-col gap-4'>
                        <Field>
                          <FieldLabel>{t('Input price')}</FieldLabel>
                          <PriceInput
                            value={promptPrice}
                            placeholder='3'
                            onChange={handlePromptPriceChange}
                          />
                          <FieldDescription>
                            {t('USD price per 1M input tokens.')}
                          </FieldDescription>
                        </Field>

                        <Field>
                          <FieldLabel>{t('Output price')}</FieldLabel>
                          <PriceInput
                            value={lanePrices.completion}
                            placeholder={outputLaneConfig.placeholder}
                            disabled={completionRatioLocked}
                            onChange={handleOutputPriceChange}
                          />
                          <FieldDescription>
                            {completionRatioLocked
                              ? t('Locked by the backend default.')
                              : t('USD price per 1M output tokens.')}
                          </FieldDescription>
                        </Field>
                      </div>

                      <div className='grid gap-3 sm:grid-cols-[repeat(auto-fit,minmax(400px,1fr))]'>
                        {laneConfigs.map((lane) => {
                          const disabled =
                            lane.key === 'audioOutput' &&
                            (!laneEnabled.audioInput ||
                              !hasValue(lanePrices.audioInput))
                          return (
                            <PriceLane
                              key={lane.key}
                              title={t(lane.titleKey)}
                              description={t(lane.descriptionKey)}
                              placeholder={lane.placeholder}
                              value={lanePrices[lane.key]}
                              enabled={laneEnabled[lane.key]}
                              disabled={disabled}
                              onEnabledChange={(checked) =>
                                handleLaneToggle(lane.key, checked)
                              }
                              onChange={(value) =>
                                handleLanePriceChange(lane.key, value)
                              }
                            />
                          )
                        })}
                      </div>
                    </FieldGroup>
                  </TabsContent>

                  <TabsContent value='per-request' className='pt-0'>
                    <FieldGroup className='gap-5'>
                      <FormField
                        control={form.control}
                        name='price'
                        render={({ field }) => (
                          <FormItem className='contents'>
                            <Field>
                              <FieldLabel>{t('Fixed price')}</FieldLabel>
                              <FormControl>
                                <InputGroup>
                                  <InputGroupAddon>$</InputGroupAddon>
                                  <InputGroupInput
                                    inputMode='decimal'
                                    placeholder='0.01'
                                    {...field}
                                    onChange={(event) => {
                                      const value = event.target.value
                                      if (numericDraftRegex.test(value)) {
                                        field.onChange(value)
                                      }
                                    }}
                                  />
                                  <InputGroupAddon align='inline-end'>
                                    {t('per request')}
                                  </InputGroupAddon>
                                </InputGroup>
                              </FormControl>
                              <FieldDescription>
                                {t(
                                  'Cost in USD per request, regardless of tokens used.'
                                )}
                              </FieldDescription>
                              <FormMessage />
                            </Field>
                          </FormItem>
                        )}
                      />
                    </FieldGroup>
                  </TabsContent>

                  <TabsContent value='tiered_expr' className='pt-0'>
                    <FieldGroup className='gap-5'>
                      <TieredPricingEditor
                        key={editorReloadToken}
                        modelName={watchedValues.name}
                        billingExpr={billingExpr}
                        requestRuleExpr={requestRuleExpr}
                        onBillingExprChange={setBillingExpr}
                        onRequestRuleExprChange={setRequestRuleExpr}
                      />
                    </FieldGroup>
                  </TabsContent>
                  <TabsContent value='context_tier' className='pt-0'>
                    <FieldGroup className='gap-4'>
                      <p className='text-muted-foreground text-sm'>
                        {t(
                          'Each row prices requests whose input length is at or below its bound; the blank-bound row covers everything above the last bound. Bounds and every price column must increase from top to bottom.'
                        )}
                      </p>
                      <ContextTierTable
                        kind='per_token'
                        value={contextTiers}
                        onChange={(tiers) => {
                          setContextTiers(tiers)
                          setContextTiersError(null)
                        }}
                      />
                      {contextTiersError ? (
                        <p className='text-destructive text-sm'>
                          {formatTierValidationError(contextTiersError, t)}
                        </p>
                      ) : null}
                    </FieldGroup>
                  </TabsContent>
                  <TabsContent value='context_tier_per_call' className='pt-0'>
                    <FieldGroup className='gap-4'>
                      <p className='text-muted-foreground text-sm'>
                        {t(
                          'Each row charges one fixed price per request whose input length is at or below its bound; the blank-bound row covers everything above the last bound. Bounds and prices must increase from top to bottom.'
                        )}
                      </p>
                      <ContextTierTable
                        kind='per_call'
                        value={contextCallTiers}
                        onChange={(tiers) => {
                          setContextCallTiers(tiers)
                          setContextCallTiersError(null)
                        }}
                      />
                      {contextCallTiersError ? (
                        <p className='text-destructive text-sm'>
                          {formatTierValidationError(contextCallTiersError, t)}
                        </p>
                      ) : null}
                    </FieldGroup>
                  </TabsContent>
                </Tabs>
                </div>
              </FieldGroup>

              <aside className='bg-muted/20 sticky top-0 rounded-lg border'>
                <div className='border-b px-3 py-2'>
                  <div className='text-sm font-medium'>{t('Preview')}</div>
                </div>
                <div className='divide-y'>
                  {previewRows.map((row) => (
                    <div key={row.key} className='grid gap-1 px-3 py-2.5'>
                      <span className='text-muted-foreground text-xs'>
                        {row.label}
                      </span>
                      <span
                        className={cn(
                          'min-w-0 text-sm',
                          row.multiline
                            ? 'font-mono text-xs leading-5 break-words whitespace-pre-wrap'
                            : 'truncate'
                        )}
                      >
                        {row.value}
                      </span>
                    </div>
                  ))}
                </div>
              </aside>
            </div>
          </div>
          {showActions && (
            <div className='bg-background/95 supports-[backdrop-filter]:bg-[var(--float-surface)] shrink-0 border-t p-3 backdrop-blur'>
              <div className='flex flex-col-reverse gap-2 sm:flex-row sm:justify-end'>
                {onSave && (
                  <Button
                    type='button'
                    onClick={onSave}
                    disabled={isSaving}
                    className='w-full sm:w-auto'
                  >
                    <Save data-icon='inline-start' />
                    {isSaving ? t('Saving...') : t('Save model prices')}
                  </Button>
                )}
              </div>
            </div>
          )}
        </form>
      </Form>
    </div>
  )
})

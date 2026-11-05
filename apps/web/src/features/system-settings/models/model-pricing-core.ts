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
import * as z from 'zod'

import { combineBillingExpr } from '@/features/pricing/lib/billing-expr'
import { formatTokenCountHint } from '@/features/pricing/lib/dynamic-price'

import { formatPricingDisplay } from './pricing-format'

export const createModelPricingSchema = (t: (key: string) => string) =>
  z.object({
    name: z.string().min(1, t('Model name is required')),
    price: z.string().optional(),
    ratio: z.string().optional(),
    cacheRatio: z.string().optional(),
    createCacheRatio: z.string().optional(),
    completionRatio: z.string().optional(),
    imageRatio: z.string().optional(),
    audioRatio: z.string().optional(),
    audioCompletionRatio: z.string().optional(),
  })

export type ModelPricingFormValues = z.infer<
  ReturnType<typeof createModelPricingSchema>
>

export type PricingMode =
  | 'per-token'
  | 'per-request'
  | 'tiered_expr'
  | 'context_tier'
  | 'context_tier_per_call'

export type LaneKey =
  | 'completion'
  | 'cache'
  | 'createCache'
  | 'image'
  | 'audioInput'
  | 'audioOutput'

export type ModelRatioData = {
  name: string
  price?: string
  ratio?: string
  cacheRatio?: string
  createCacheRatio?: string
  completionRatio?: string
  imageRatio?: string
  audioRatio?: string
  audioCompletionRatio?: string
  billingMode?: PricingMode
  billingExpr?: string
  requestRuleExpr?: string
  billingContextTier?: string
  billingContextTierPerCall?: string
  // Ratio billing actually charges for output tokens, resolved by the backend
  // (DB override → hardcoded default). Display-only: it is never persisted.
  completionRatioEffective?: string
  completionRatioLocked?: boolean
}

export type PreviewRow = {
  key: string
  label: string
  value: string
  multiline?: boolean
}

export const numericDraftRegex = /^(\d+(\.\d*)?|\.\d*)?$/

export const EMPTY_LANE_PRICES: Record<LaneKey, string> = {
  completion: '',
  cache: '',
  createCache: '',
  image: '',
  audioInput: '',
  audioOutput: '',
}

export const EMPTY_LANE_ENABLED: Record<LaneKey, boolean> = {
  completion: false,
  cache: false,
  createCache: false,
  image: false,
  audioInput: false,
  audioOutput: false,
}

export const ratioFieldByLane: Record<LaneKey, keyof ModelPricingFormValues> = {
  completion: 'completionRatio',
  cache: 'cacheRatio',
  createCache: 'createCacheRatio',
  image: 'imageRatio',
  audioInput: 'audioRatio',
  audioOutput: 'audioCompletionRatio',
}

export const outputLaneConfig: {
  key: LaneKey
  titleKey: string
  descriptionKey: string
  placeholder: string
} = {
  key: 'completion',
  titleKey: 'Output price',
  descriptionKey: 'Output token price for generated tokens.',
  placeholder: '15',
}

export const laneConfigs: Array<{
  key: LaneKey
  titleKey: string
  descriptionKey: string
  placeholder: string
}> = [
  {
    key: 'cache',
    titleKey: 'Cache read price',
    descriptionKey: 'Token price for cache reads.',
    placeholder: '0.3',
  },
  {
    key: 'createCache',
    titleKey: 'Cache write price',
    descriptionKey: 'Token price for creating cache entries.',
    placeholder: '3.75',
  },
  {
    key: 'image',
    titleKey: 'Image input price',
    descriptionKey: 'Token price for image input.',
    placeholder: '2.5',
  },
  {
    key: 'audioInput',
    titleKey: 'Audio input price',
    descriptionKey: 'Token price for audio input.',
    placeholder: '3.81',
  },
  {
    key: 'audioOutput',
    titleKey: 'Audio output price',
    descriptionKey: 'Token price for audio output.',
    placeholder: '15.11',
  },
]

export function hasValue(value: unknown): boolean {
  return (
    value !== '' && value !== null && value !== undefined && value !== false
  )
}

export function toNumberOrNull(value: unknown): number | null {
  if (!hasValue(value) && value !== 0) return null
  const num = Number(value)
  return Number.isFinite(num) ? num : null
}

function ratioToBasePrice(ratio: unknown): string {
  const num = toNumberOrNull(ratio)
  if (num === null) return ''
  return formatPricingDisplay(num * 2)
}

export function deriveLanePrice(
  ratio: unknown,
  denominator: unknown,
  fallback = ''
): string {
  const ratioNumber = toNumberOrNull(ratio)
  const denominatorNumber = toNumberOrNull(denominator)
  if (ratioNumber === null || denominatorNumber === null) return fallback
  return formatPricingDisplay(ratioNumber * denominatorNumber)
}

export function createInitialLaneState(data?: ModelRatioData | null) {
  if (!data) {
    return {
      promptPrice: '',
      prices: { ...EMPTY_LANE_PRICES },
      enabled: { ...EMPTY_LANE_ENABLED },
    }
  }

  const promptPrice = ratioToBasePrice(data.ratio)
  const audioInputPrice = deriveLanePrice(data.audioRatio, promptPrice)
  // Output price shows the ratio billing applies, so the panel matches the
  // prices the backend charges even when the DB holds no explicit entry.
  const completionRatio = data.completionRatioEffective || data.completionRatio
  const prices: Record<LaneKey, string> = {
    completion: deriveLanePrice(completionRatio, promptPrice),
    cache: deriveLanePrice(data.cacheRatio, promptPrice),
    createCache: deriveLanePrice(data.createCacheRatio, promptPrice),
    image: deriveLanePrice(data.imageRatio, promptPrice),
    audioInput: audioInputPrice,
    audioOutput: deriveLanePrice(data.audioCompletionRatio, audioInputPrice),
  }

  return {
    promptPrice,
    prices,
    enabled: {
      // Only an explicit DB entry is persisted; the backend default is
      // inherited and must not be written back into the ratio map.
      completion: hasValue(data.completionRatio),
      cache: hasValue(data.cacheRatio),
      createCache: hasValue(data.createCacheRatio),
      image: hasValue(data.imageRatio),
      audioInput: hasValue(data.audioRatio),
      audioOutput: hasValue(data.audioCompletionRatio),
    },
  }
}

const ContextTierSchema = z.object({
  name: z.string().trim().min(1, 'name must be a non-empty string').optional(),
  max_context: z
    .number()
    .int('max_context must be an integer token count')
    .min(0, 'max_context must be a number >= 0 (0 = catch-all)'),
  input_price: z
    .number()
    .finite()
    .min(0, 'input_price must be a number >= 0')
    .optional(),
  output_price: z
    .number()
    .finite()
    .min(0, 'output_price must be a number >= 0')
    .optional(),
  cache_price: z
    .number()
    .finite()
    .min(0, 'cache_price must be a number >= 0')
    .optional(),
})

const ContextTierListSchema = z
  .array(ContextTierSchema)
  .min(1, 'Tiers must be a non-empty array ({"tiers":[...]}).')

const ContextTierPayloadSchema = z.object({ tiers: ContextTierListSchema })

/** A tier validation failure, ready for i18next (messageKey may nest t()). */
export type TierValidationError =
  | { kind: 'plain'; messageKey: string }
  | { kind: 'tier'; index: number; messageKey: string }

/** zod v4 paths are PropertyKey[]; tier positions are the numeric entries. */
function firstTierIssue(
  error: z.ZodError
): TierValidationError {
  const issue = error.issues[0]
  if (!issue) return { kind: 'plain', messageKey: 'Invalid tier configuration.' }
  const tierIndex = issue.path.filter(
    (part): part is number => typeof part === 'number'
  )[0]
  if (tierIndex === undefined) {
    return { kind: 'plain', messageKey: issue.message }
  }
  return {
    kind: 'tier',
    index: tierIndex + 1,
    messageKey: issue.message,
  }
}

/**
 * Both shapes failed: prefer whichever error points into a tier (carries a
 * numeric path segment); the bare-array schema's "expected array" message
 * would otherwise bury the real problem for `{"tiers":[...]}` input.
 */
function pickTierIssue(a: z.ZodError, b: z.ZodError): TierValidationError {
  const hasTierPath = (error: z.ZodError) =>
    error.issues.some((issue) =>
      issue.path.some((part) => typeof part === 'number')
    )
  if (hasTierPath(a)) return firstTierIssue(a)
  if (hasTierPath(b)) return firstTierIssue(b)
  return firstTierIssue(a)
}

/** Shared row-order checks: catch-all last, bounds and each price lane strictly increasing. */
function checkTierRowOrder(
  tiers: Array<{ max_context: number } & Record<string, unknown>>,
  priceKeys: string[]
): TierValidationError | null {
  let prevBound: number | null = null
  for (const [index, tier] of tiers.entries()) {
    if (tier.max_context === 0) {
      if (index !== tiers.length - 1) {
        return {
          kind: 'plain',
          messageKey: 'The catch-all row (blank bound) must be the last row.',
        }
      }
      break
    }
    if (prevBound !== null && tier.max_context <= prevBound) {
      return {
        kind: 'plain',
        messageKey: 'Interval bounds must strictly increase from top to bottom.',
      }
    }
    prevBound = tier.max_context
  }
  for (const key of priceKeys) {
    // A lane no row defines at all is simply unused (e.g. no cache pricing);
    // only mixed or filled columns participate in the ordering check.
    const laneUsed = tiers.some(
      (tier) => typeof tier[key] === 'number' && (tier[key] as number) !== 0
    )
    if (!laneUsed) continue
    let prev: number | null = null
    for (const tier of tiers) {
      const value = typeof tier[key] === 'number' ? (tier[key] as number) : 0
      if (prev !== null && value <= prev) {
        const laneKeys: Record<string, string> = {
          input_price: 'Input prices must increase from top to bottom.',
          output_price: 'Output prices must increase from top to bottom.',
          cache_price: 'Cache read prices must increase from top to bottom.',
          price: 'Per-request prices must increase from top to bottom.',
        }
        return { kind: 'plain', messageKey: laneKeys[key] ?? key }
      }
      prev = value
    }
  }
  return null
}

type ContextTierLike = z.infer<typeof ContextTierSchema>
type ContextCallTierLike = z.infer<typeof ContextCallTierSchema>

/**
 * Validates the stored context-tier fees JSON for `context_tier` billing
 * mode: shape, numeric ranges, row order (bounds strictly increasing,
 * catch-all last) and strictly increasing prices per lane. Accepts either
 * `{"tiers":[...]}` or a bare tier array.
 */
export function validateContextTiersStructured(
  raw: string
): TierValidationError | null {
  const text = (raw ?? '').trim()
  if (!text) {
    return {
      kind: 'plain',
      messageKey: 'Context tier fees are required for this billing mode.',
    }
  }

  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return { kind: 'plain', messageKey: 'Invalid JSON: tiers must be valid JSON.' }
  }

  const asPayload = ContextTierPayloadSchema.safeParse(parsed)
  const asList = asPayload.success
    ? null
    : ContextTierListSchema.safeParse(parsed)

  let tiers: ContextTierLike[]
  if (asPayload.success) {
    tiers = asPayload.data.tiers
  } else if (asList && asList.success) {
    tiers = asList.data
  } else if (asList) {
    return pickTierIssue(asPayload.error, asList.error)
  } else {
    return firstTierIssue(asPayload.error)
  }

  return checkTierRowOrder(tiers, ['input_price', 'output_price', 'cache_price'])
}

const ContextCallTierSchema = z.object({
  name: z.string().trim().min(1, 'name must be a non-empty string').optional(),
  max_context: z
    .number()
    .int('max_context must be an integer token count')
    .min(0, 'max_context must be a number >= 0 (0 = catch-all)'),
  price: z
    .number()
    .finite()
    .min(0, 'price must be a number >= 0'),
})

const ContextCallTierListSchema = z
  .array(ContextCallTierSchema)
  .min(1, 'Tiers must be a non-empty array ({"tiers":[...]}).')

const ContextCallTierPayloadSchema = z.object({
  tiers: ContextCallTierListSchema,
})

/**
 * Validates the stored per-call context-tier JSON for
 * `context_tier_per_call` billing mode: same interval and row-order rules
 * as the per-token variant, but each tier carries exactly one fixed
 * per-request price.
 */
export function validateContextCallTiersStructured(
  raw: string
): TierValidationError | null {
  const text = (raw ?? '').trim()
  if (!text) {
    return {
      kind: 'plain',
      messageKey: 'Context tier fees are required for this billing mode.',
    }
  }

  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return { kind: 'plain', messageKey: 'Invalid JSON: tiers must be valid JSON.' }
  }

  const asPayload = ContextCallTierPayloadSchema.safeParse(parsed)
  const asList = asPayload.success
    ? null
    : ContextCallTierListSchema.safeParse(parsed)

  let tiers: ContextCallTierLike[]
  if (asPayload.success) {
    tiers = asPayload.data.tiers
  } else if (asList && asList.success) {
    tiers = asList.data
  } else if (asList) {
    return pickTierIssue(asPayload.error, asList.error)
  } else {
    return firstTierIssue(asPayload.error)
  }

  return checkTierRowOrder(tiers, ['price'])
}

/** Renders a structured tier error with i18next (messageKey nests t()). */
export function formatTierValidationError(
  error: TierValidationError,
  t: (key: string, values?: Record<string, unknown>) => string
): string {
  if (error.kind === 'tier') {
    return t('Tier #{{index}}: {{message}}', {
      index: error.index,
      message: t(error.messageKey),
    })
  }
  return t(error.messageKey)
}

type PreviewTierRow = { max_context: number; input_price?: number; output_price?: number; cache_price?: number; price?: number }

function parsePreviewTiers(raw: string): PreviewTierRow[] {
  const text = (raw ?? '').trim()
  if (!text) return []
  try {
    const parsed: unknown = JSON.parse(text)
    const list = Array.isArray(parsed)
      ? parsed
      : typeof parsed === 'object' &&
          parsed !== null &&
          'tiers' in parsed &&
          Array.isArray(parsed.tiers)
        ? parsed.tiers
        : null
    if (!list) return []
    return list
      .filter((item): item is PreviewTierRow => {
        return (
          typeof item === 'object' &&
          item !== null &&
          typeof (item as PreviewTierRow).max_context === 'number'
        )
      })
      .sort((a, b) =>
        a.max_context === 0 ? 1 : b.max_context === 0 ? -1 : a.max_context - b.max_context
      )
  } catch {
    return []
  }
}

/** One preview row per interval: bound label plus the lane prices it charges. */
function buildContextTierPreviewRows(
  raw: string,
  kind: 'per_token' | 'per_call',
  t: (key: string) => string
): PreviewRow[] {
  const tiers = parsePreviewTiers(raw)
  if (tiers.length === 0) {
    return [{ key: 'tiers', label: t('Per-token context tiers'), value: t('Empty') }]
  }
  let prevBound: number | null = null
  return tiers.map((tier, index) => {
    const boundHint = formatTokenCountHint(tier.max_context)
    const label =
      tier.max_context === 0
        ? prevBound !== null
          ? `> ${formatTokenCountHint(prevBound)}`
          : t('Any input length')
        : `≤ ${boundHint}`
    prevBound = tier.max_context === 0 ? prevBound : tier.max_context
    const parts: string[] = []
    if (kind === 'per_call') {
      parts.push(`$${tier.price ?? 0} ${t('per request')}`)
    } else {
      parts.push(`${t('Input price')} $${tier.input_price ?? 0}`)
      parts.push(`${t('Output price')} $${tier.output_price ?? 0}`)
      if (tier.cache_price) {
        parts.push(`${t('Cache read price')} $${tier.cache_price}`)
      }
    }
    void index
    return {
      key: `tier-${index}`,
      label,
      value: parts.join(' · '),
    }
  })
}

export function buildPreviewRows(
  values: ModelPricingFormValues,
  mode: PricingMode,
  billingExpr: string,
  requestRuleExpr: string,
  promptPrice: string,
  lanePrices: Record<LaneKey, string>,
  laneEnabled: Record<LaneKey, boolean>,
  contextTiers: string,
  contextCallTiers: string,
  t: (key: string) => string
): PreviewRow[] {
  if (mode === 'tiered_expr') {
    const effectiveExpr = combineBillingExpr(billingExpr, requestRuleExpr)
    return [
      { key: 'mode', label: 'BillingMode', value: 'tiered_expr' },
      {
        key: 'expr',
        label: t('Expression'),
        value: effectiveExpr || t('Empty'),
        multiline: true,
      },
    ]
  }

  if (mode === 'context_tier') {
    return [
      { key: 'mode', label: 'BillingMode', value: 'context_tier' },
      ...buildContextTierPreviewRows(contextTiers, 'per_token', t),
    ]
  }

  if (mode === 'context_tier_per_call') {
    return [
      { key: 'mode', label: 'BillingMode', value: 'context_tier_per_call' },
      ...buildContextTierPreviewRows(contextCallTiers, 'per_call', t),
    ]
  }

  if (mode === 'per-request') {
    return [
      {
        key: 'price',
        label: 'ModelPrice',
        value: values.price || t('Empty'),
      },
    ]
  }

  return [
    {
      key: 'inputPrice',
      label: t('Input price'),
      value: promptPrice ? `$${promptPrice}` : t('Empty'),
    },
    {
      key: 'completion',
      label: t('Output price'),
      value: lanePrices.completion ? `$${lanePrices.completion}` : t('Empty'),
    },
    {
      key: 'cache',
      label: t('Cache read price'),
      value:
        laneEnabled.cache && lanePrices.cache
          ? `$${lanePrices.cache}`
          : t('Empty'),
    },
    {
      key: 'createCache',
      label: t('Cache write price'),
      value:
        laneEnabled.createCache && lanePrices.createCache
          ? `$${lanePrices.createCache}`
          : t('Empty'),
    },
    {
      key: 'image',
      label: t('Image input price'),
      value:
        laneEnabled.image && lanePrices.image
          ? `$${lanePrices.image}`
          : t('Empty'),
    },
    {
      key: 'audio',
      label: t('Audio input price'),
      value:
        laneEnabled.audioInput && lanePrices.audioInput
          ? `$${lanePrices.audioInput}`
          : t('Empty'),
    },
    {
      key: 'audioCompletion',
      label: t('Audio output price'),
      value:
        laneEnabled.audioOutput && lanePrices.audioOutput
          ? `$${lanePrices.audioOutput}`
          : t('Empty'),
    },
  ]
}

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
import { splitBillingExprAndRequestRules } from '@/features/pricing/lib/billing-expr'
import { formatTokenCountHint } from '@/features/pricing/lib/dynamic-price'

import { safeJsonParse } from '../utils/json-parser'
import { formatPricingNumber } from './pricing-format'

export type ModelPricingSnapshotInput = {
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
  billingContextTier?: string
  billingContextTierPerCall?: string
  // Backend-computed effective completion ratio per model. Mirrors the
  // backend's own resolution (DB override → hardcoded default) so the UI can
  // show the price billing actually charges.
  completionRatioMeta?: string
}

export type CompletionRatioMeta = {
  ratio: number
  locked: boolean
}

export type CompletionRatioMetaMap = Record<string, CompletionRatioMeta>

export const parseCompletionRatioMeta = (
  raw: string | undefined
): CompletionRatioMetaMap =>
  safeJsonParse<CompletionRatioMetaMap>(raw ?? '', {
    fallback: {},
    context: 'completion ratio meta',
  })

export type ModelPricingSnapshot = {
  name: string
  price?: string
  ratio?: string
  cacheRatio?: string
  createCacheRatio?: string
  completionRatio?: string
  imageRatio?: string
  audioRatio?: string
  audioCompletionRatio?: string
  billingMode?: string
  billingExpr?: string
  billingContextTier?: string
  billingContextTierPerCall?: string
  requestRuleExpr?: string
  hasConflict: boolean
}

export type ModelRow = ModelPricingSnapshot & {
  saved?: ModelPricingSnapshot
  draft?: ModelPricingSnapshot
  isDraftChanged: boolean
  isDraftDeleted: boolean
  isDraftNew: boolean
  // True while the model has no stored pricing of its own.
  isPriceUnset: boolean
}

export const hasPricingValue = (value?: string) =>
  value !== undefined && value !== ''

/**
 * Resolves the completion ratio billing actually charges for a model.
 * Mirrors the backend precedence: a locked hardcoded default wins over the
 * stored map, otherwise the stored ratio wins, and the backend default is the
 * fallback.
 */
export const resolveEffectiveCompletionRatio = (
  modelName: string,
  meta: CompletionRatioMetaMap,
  dbCompletionRatio?: string
): number | null => {
  const entry = meta[modelName]
  const fromDb = toNumberOrNull(dbCompletionRatio)

  if (modelName.includes('/') && fromDb !== null) return fromDb
  if (entry?.locked) return entry.ratio
  if (fromDb !== null) return fromDb
  return entry?.ratio ?? null
}

export type TierPriceRow = {
  key: string
  /** Interval label: `≤100K`, `>100K`, or `*` for a flat price. */
  boundLabel: string
  inputPrice?: number
  outputPrice?: number
  cachePrice?: number
  cacheWritePrice?: number
  price?: number
}

/**
 * Flattens stored tiers into display rows (ascending bound, catch-all last)
 * with a human-readable interval label. Returns [] for missing/invalid data.
 */
export const parseTierRows = (raw?: string): TierPriceRow[] => {
  const text = (raw ?? '').trim()
  if (!text) return []
  let list: unknown
  try {
    const parsed: unknown = JSON.parse(text)
    if (Array.isArray(parsed)) {
      list = parsed
    } else if (
      typeof parsed === 'object' &&
      parsed !== null &&
      'tiers' in parsed &&
      Array.isArray(parsed.tiers)
    ) {
      list = parsed.tiers
    }
  } catch {
    return []
  }
  if (!Array.isArray(list) || list.length === 0) return []
  const tiers = list
    .filter(
      (item): item is Record<string, unknown> =>
        typeof item === 'object' && item !== null
    )
    .map((item) => ({
      maxContext: typeof item.max_context === 'number' ? item.max_context : 0,
      inputPrice: item.input_price,
      outputPrice: item.output_price,
      cachePrice: item.cache_price,
      cacheWritePrice: item.cache_write_price,
      price: item.price,
    }))
    .sort((a, b) => {
      const av = a.maxContext === 0 ? Number.POSITIVE_INFINITY : a.maxContext
      const bv = b.maxContext === 0 ? Number.POSITIVE_INFINITY : b.maxContext
      return av - bv
    })
  const num = (value: unknown) =>
    typeof value === 'number' && Number.isFinite(value) ? value : undefined
  let prevBound: number | null = null
  return tiers.map((tier, index) => {
    let boundLabel: string
    if (tier.maxContext > 0) {
      boundLabel = `≤${formatTokenCountHint(tier.maxContext)}`
      prevBound = tier.maxContext
    } else {
      boundLabel =
        prevBound !== null ? `>${formatTokenCountHint(prevBound)}` : '*'
    }
    return {
      key: `tier-${index}`,
      boundLabel,
      inputPrice: num(tier.inputPrice),
      outputPrice: num(tier.outputPrice),
      cachePrice: num(tier.cachePrice),
      cacheWritePrice: num(tier.cacheWritePrice),
      price: num(tier.price),
    }
  })
}

export const isBasePricingUnset = (snapshot?: ModelPricingSnapshot) => {
  if (!snapshot) return true
  if (snapshot.billingMode === 'tiered_expr') return false
  if (snapshot.billingMode === 'context_tier') {
    return !hasTierData(snapshot.billingContextTier)
  }
  if (snapshot.billingMode === 'context_tier_per_call') {
    return !hasTierData(snapshot.billingContextTierPerCall)
  }
  return !hasPricingValue(snapshot.price) && !hasPricingValue(snapshot.ratio)
}

const toNumberOrNull = (value?: string) => {
  if (!hasPricingValue(value)) return null
  const num = Number(value)
  return Number.isFinite(num) ? num : null
}

const ratioToPrice = (ratio?: string, denominator?: string) => {
  const ratioNumber = toNumberOrNull(ratio)
  const denominatorNumber = denominator ? toNumberOrNull(denominator) : 2
  if (ratioNumber === null || denominatorNumber === null) return ''
  return formatPricingNumber(ratioNumber * denominatorNumber)
}

/** Lenient tier summary for list/card display; invalid JSON yields null. */
export type TierSummary = {
  count: number
  inputMin: number | null
  inputMax: number | null
  outputMin: number | null
  outputMax: number | null
  priceMin: number | null
  priceMax: number | null
  maxBound: number | null
}

const rangeOf = (values: number[]): [number | null, number | null] =>
  values.length === 0
    ? [null, null]
    : [Math.min(...values), Math.max(...values)]

export const parseTierSummary = (raw?: string): TierSummary | null => {
  const text = (raw ?? '').trim()
  if (!text) return null
  let list: unknown
  try {
    const parsed: unknown = JSON.parse(text)
    if (Array.isArray(parsed)) {
      list = parsed
    } else if (
      typeof parsed === 'object' &&
      parsed !== null &&
      'tiers' in parsed &&
      Array.isArray(parsed.tiers)
    ) {
      list = parsed.tiers
    }
  } catch {
    return null
  }
  if (!Array.isArray(list) || list.length === 0) return null
  const rows = list.filter(
    (item): item is Record<string, unknown> =>
      typeof item === 'object' && item !== null
  )
  if (rows.length === 0) return null
  const numbers = (key: string) =>
    rows
      .map((row) => row[key])
      .filter((value): value is number => typeof value === 'number')
  const [inputMin, inputMax] = rangeOf(numbers('input_price'))
  const [outputMin, outputMax] = rangeOf(numbers('output_price'))
  const [priceMin, priceMax] = rangeOf(numbers('price'))
  const bounds = numbers('max_context').filter((bound) => bound > 0)
  return {
    count: rows.length,
    inputMin,
    inputMax,
    outputMin,
    outputMax,
    priceMin,
    priceMax,
    maxBound: bounds.length > 0 ? Math.max(...bounds) : null,
  }
}

/** `$1` for a single value, `$1–$2` for a range, `—` when unset. */
export const formatTierPriceRange = (
  min: number | null,
  max: number | null
) => {
  if (min === null) return '—'
  if (max === null || min === max) return `$${min}`
  return `$${min}–$${max}`
}

const hasTierData = (raw?: string) => parseTierSummary(raw) !== null

export const getModeLabel = (mode?: string) => {
  if (mode === 'per-request') return 'Per-request'
  if (mode === 'tiered_expr') return 'Expression'
  if (mode === 'context_tier') return 'Per-token context tiers'
  if (mode === 'context_tier_per_call') return 'Per-call context tiers'
  return 'Per-token'
}

export const getModeVariant = (
  mode?: string
): 'warning' | 'info' | 'success' => {
  if (mode === 'per-request') return 'warning'
  if (mode === 'context_tier_per_call') return 'warning'
  if (mode === 'tiered_expr') return 'info'
  if (mode === 'context_tier') return 'info'
  return 'success'
}

const getExpressionSummary = (
  row: ModelPricingSnapshot,
  t: (key: string) => string
) => {
  const tierCount = (row.billingExpr?.match(/tier\(/g) || []).length
  if (tierCount > 0) {
    return `${t('Tiered pricing')} · ${tierCount} ${t('tiers')}`
  }
  return t('Expression pricing')
}

export const getPriceSummary = (
  row: ModelPricingSnapshot,
  t: (key: string) => string
) => {
  if (row.billingMode === 'tiered_expr') {
    return getExpressionSummary(row, t)
  }
  if (row.billingMode === 'context_tier') {
    const tiers = parseTierSummary(row.billingContextTier)
    if (!tiers) return t('Unset price')
    return `${t('Input')} ${formatTierPriceRange(tiers.inputMin, tiers.inputMax)} · ${t('Output')} ${formatTierPriceRange(tiers.outputMin, tiers.outputMax)}`
  }
  if (row.billingMode === 'context_tier_per_call') {
    const tiers = parseTierSummary(row.billingContextTierPerCall)
    if (!tiers) return t('Unset price')
    return `${formatTierPriceRange(tiers.priceMin, tiers.priceMax)} / ${t('request')}`
  }
  if (row.billingMode === 'per-request') {
    return row.price ? `$${row.price} / ${t('request')}` : t('Unset price')
  }

  const inputPrice = ratioToPrice(row.ratio)
  if (!inputPrice) return t('Unset price')

  const extraCount = [
    row.completionRatio,
    row.cacheRatio,
    row.createCacheRatio,
    row.imageRatio,
    row.audioRatio,
    row.audioCompletionRatio,
  ].filter(hasPricingValue).length

  return extraCount > 0
    ? `${t('Input')} $${inputPrice} · ${extraCount} ${t('extras')}`
    : `${t('Input')} $${inputPrice}`
}

export const getPriceDetail = (
  row: ModelPricingSnapshot,
  t: (key: string) => string
) => {
  if (row.billingMode === 'tiered_expr') {
    return row.requestRuleExpr
      ? t('Includes request rules')
      : t('Expression based')
  }
  if (
    row.billingMode === 'context_tier' ||
    row.billingMode === 'context_tier_per_call'
  ) {
    const tiers = parseTierSummary(
      row.billingMode === 'context_tier'
        ? row.billingContextTier
        : row.billingContextTierPerCall
    )
    if (!tiers) return t('No base input price')
    const bound =
      tiers.maxBound !== null
        ? ` · ≤${formatTokenCountHint(tiers.maxBound)}`
        : ''
    return `${tiers.count} ${t('tiers')}${bound}`
  }
  if (row.billingMode === 'per-request') {
    return t('Fixed request price')
  }

  const inputPrice = ratioToPrice(row.ratio)
  if (!inputPrice) return t('No base input price')

  const details = [
    row.completionRatio &&
      `${t('Output')} $${ratioToPrice(row.completionRatio, inputPrice)}`,
    row.cacheRatio &&
      `${t('Cache')} $${ratioToPrice(row.cacheRatio, inputPrice)}`,
    row.createCacheRatio &&
      `${t('Cache write')} $${ratioToPrice(row.createCacheRatio, inputPrice)}`,
  ]
    .filter(Boolean)
    .slice(0, 2)

  return details.length > 0 ? details.join(' · ') : t('Base input price only')
}

export const buildModelSnapshots = ({
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
  billingContextTier,
  billingContextTierPerCall,
}: ModelPricingSnapshotInput): ModelPricingSnapshot[] => {
  const priceMap = safeJsonParse<Record<string, number>>(modelPrice, {
    fallback: {},
    context: 'model prices',
  })
  const ratioMap = safeJsonParse<Record<string, number>>(modelRatio, {
    fallback: {},
    context: 'model ratios',
  })
  const cacheMap = safeJsonParse<Record<string, number>>(cacheRatio, {
    fallback: {},
    context: 'cache ratios',
  })
  const createCacheMap = safeJsonParse<Record<string, number>>(
    createCacheRatio,
    { fallback: {}, context: 'create cache ratios' }
  )
  const completionMap = safeJsonParse<Record<string, number>>(completionRatio, {
    fallback: {},
    context: 'completion ratios',
  })
  const imageMap = safeJsonParse<Record<string, number>>(imageRatio, {
    fallback: {},
    context: 'image ratios',
  })
  const audioMap = safeJsonParse<Record<string, number>>(audioRatio, {
    fallback: {},
    context: 'audio ratios',
  })
  const audioCompletionMap = safeJsonParse<Record<string, number>>(
    audioCompletionRatio,
    { fallback: {}, context: 'audio completion ratios' }
  )
  const billingModeMap = safeJsonParse<Record<string, string>>(billingMode, {
    fallback: {},
    context: 'billing mode',
  })
  const billingExprMap = safeJsonParse<Record<string, string>>(billingExpr, {
    fallback: {},
    context: 'billing expression',
  })
  const billingContextTierMap = safeJsonParse<Record<string, string>>(
    billingContextTier,
    {
      fallback: {},
      context: 'billing context tier',
    }
  )
  const billingContextTierPerCallMap = safeJsonParse<Record<string, string>>(
    billingContextTierPerCall,
    { fallback: {}, context: 'billing context tier (per call)' }
  )

  const modelNames = new Set([
    ...Object.keys(priceMap),
    ...Object.keys(ratioMap),
    ...Object.keys(cacheMap),
    ...Object.keys(createCacheMap),
    ...Object.keys(completionMap),
    ...Object.keys(imageMap),
    ...Object.keys(audioMap),
    ...Object.keys(audioCompletionMap),
    ...Object.keys(billingModeMap),
    ...Object.keys(billingExprMap),
    ...Object.keys(billingContextTierMap),
    ...Object.keys(billingContextTierPerCallMap),
  ])

  return Array.from(modelNames).map((name) => {
    const price = priceMap[name]?.toString() || ''
    const ratio = ratioMap[name]?.toString() || ''
    const cache = cacheMap[name]?.toString() || ''
    const createCache = createCacheMap[name]?.toString() || ''
    const completion = completionMap[name]?.toString() || ''
    const image = imageMap[name]?.toString() || ''
    const audio = audioMap[name]?.toString() || ''
    const audioCompletion = audioCompletionMap[name]?.toString() || ''

    const modeForModel = billingModeMap[name]
    // Every method's data persists independently of which one is active, so
    // each row carries ALL stored method payloads; otherwise opening a model
    // whose active method differs from a configured one and saving would
    // overwrite/delete the other methods' config. A persisted expression is
    // authoritative for display: even when the billing_mode map drifted (or
    // never carried the entry), an expression means this model is
    // expression-billed.
    const storedExpr = billingExprMap[name] || ''
    const { billingExpr: pureExpr, requestRuleExpr } =
      splitBillingExprAndRequestRules(storedExpr)
    const storedTiers = billingContextTierMap[name] || ''
    const storedCallTiers = billingContextTierPerCallMap[name] || ''
    const shared = {
      price,
      ratio,
      cacheRatio: cache,
      createCacheRatio: createCache,
      completionRatio: completion,
      imageRatio: image,
      audioRatio: audio,
      audioCompletionRatio: audioCompletion,
      billingExpr: pureExpr,
      requestRuleExpr,
      billingContextTier: storedTiers,
      billingContextTierPerCall: storedCallTiers,
      hasConflict: false,
    }
    if (modeForModel === 'tiered_expr' || storedExpr.trim() !== '') {
      return { name, billingMode: 'tiered_expr', ...shared }
    }
    if (modeForModel === 'context_tier') {
      return { name, billingMode: 'context_tier', ...shared }
    }
    if (modeForModel === 'context_tier_per_call') {
      return { name, billingMode: 'context_tier_per_call', ...shared }
    }

    return {
      name,
      ...shared,
      billingMode: price !== '' ? 'per-request' : 'per-token',
      hasConflict:
        price !== '' &&
        (ratio !== '' ||
          completion !== '' ||
          cache !== '' ||
          createCache !== '' ||
          image !== '' ||
          audio !== '' ||
          audioCompletion !== ''),
    }
  })
}

export const getSnapshotSignature = (snapshot?: ModelPricingSnapshot) => {
  if (!snapshot) return ''
  return JSON.stringify({
    price: snapshot.price || '',
    ratio: snapshot.ratio || '',
    cacheRatio: snapshot.cacheRatio || '',
    createCacheRatio: snapshot.createCacheRatio || '',
    completionRatio: snapshot.completionRatio || '',
    imageRatio: snapshot.imageRatio || '',
    audioRatio: snapshot.audioRatio || '',
    audioCompletionRatio: snapshot.audioCompletionRatio || '',
    billingMode: snapshot.billingMode || 'per-token',
    billingExpr: snapshot.billingExpr || '',
    billingContextTier: snapshot.billingContextTier || '',
    billingContextTierPerCall: snapshot.billingContextTierPerCall || '',
    requestRuleExpr: snapshot.requestRuleExpr || '',
  })
}

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
import { Plus, Trash2 } from 'lucide-react'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import {
  formatTierValidationError,
  validateContextCallTiersStructured,
  validateContextTiersStructured,
  type TierValidationError,
} from './model-pricing-core'

import { Button } from '@/components/ui/button'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'

import { PriceInput } from './model-pricing-inputs'

/**
 * One editable interval row. `maxContext === ''` marks the catch-all row
 * (stored as max_context 0). Prices are draft strings; blank means 0.
 */
type ContextTierRow = {
  maxContext: string
  inputPrice: string
  outputPrice: string
  cachePrice: string
  cacheWritePrice: string
  price: string
}

const emptyRow = (): ContextTierRow => ({
  maxContext: '',
  inputPrice: '',
  outputPrice: '',
  cachePrice: '',
  cacheWritePrice: '',
  price: '',
})

/**
 * Token-bound input grammar: plain integers ('100000'), k/M shorthand
 * ('10k', '1.5m', '200K'). Empty string means catch-all.
 */
export function parseTokenBound(input: string): number | null {
  const trimmed = input.trim().toLowerCase()
  if (trimmed === '') return 0
  const match = /^([\d.]+)\s*([km])?$/.exec(trimmed)
  if (!match) return null
  const base = Number(match[1])
  if (!Number.isFinite(base)) return null
  const scaled = base * (match[2] === 'k' ? 1_000 : match[2] === 'm' ? 1_000_000 : 1)
  const tokens = Math.trunc(scaled)
  return tokens >= 0 ? tokens : null
}

/** Compact display for a stored token bound ('10000' -> '10k'); 0 renders blank (catch-all). */
function formatBoundDisplay(tokens: number): string {
  if (tokens <= 0) return ''
  if (tokens % 1_000_000 === 0) return `${tokens / 1_000_000}m`
  if (tokens % 1_000 === 0) return `${tokens / 1_000}k`
  return String(tokens)
}

function parseTiersValue(raw: string): {
  rows: ContextTierRow[]
  parseError: string
} {
  const text = (raw ?? '').trim()
  if (!text) return { rows: [emptyRow()], parseError: '' }
  try {
    const parsed: unknown = JSON.parse(text)
    let list: unknown[] | null = null
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
    if (!list) return { rows: [emptyRow()], parseError: 'invalid-shape' }
    const rows = list.map((item) => {
      const t = (item ?? {}) as Record<string, unknown>
      const bound = t.max_context
      return {
        // 0 is the catch-all; display it as an empty bound field.
        maxContext:
          typeof bound === 'number' && bound > 0
            ? formatBoundDisplay(bound)
            : '',
        inputPrice:
          typeof t.input_price === 'number' ? String(t.input_price) : '',
        outputPrice:
          typeof t.output_price === 'number' ? String(t.output_price) : '',
        cachePrice:
          typeof t.cache_price === 'number' ? String(t.cache_price) : '',
        cacheWritePrice:
          typeof t.cache_write_price === 'number'
            ? String(t.cache_write_price)
            : '',
        price: typeof t.price === 'number' ? String(t.price) : '',
      }
    })
    return { rows: rows.length > 0 ? rows : [emptyRow()], parseError: '' }
  } catch {
    return { rows: [emptyRow()], parseError: 'invalid-json' }
  }
}

function serializeRows(rows: ContextTierRow[], kind: 'per_token' | 'per_call') {
  const tiers = rows
    .filter((row) => {
      const boundFilled = row.maxContext.trim() !== ''
      const priceFilled =
        kind === 'per_call'
          ? row.price.trim() !== ''
          : [
              row.inputPrice,
              row.outputPrice,
              row.cachePrice,
              row.cacheWritePrice,
            ].some((price) => price.trim() !== '')
      return boundFilled || priceFilled
    })
    .map((row) => {
      const maxContext = parseTokenBound(row.maxContext) ?? 0
      if (kind === 'per_call') {
        return { max_context: maxContext, price: Number(row.price) || 0 }
      }
      return {
        max_context: maxContext,
        input_price: Number(row.inputPrice) || 0,
        output_price: Number(row.outputPrice) || 0,
        ...(Number(row.cachePrice)
          ? { cache_price: Number(row.cachePrice) }
          : {}),
        ...(Number(row.cacheWritePrice)
          ? { cache_write_price: Number(row.cacheWritePrice) }
          : {}),
      }
    })
  if (tiers.length === 0) return ''
  return JSON.stringify({ tiers })
}

type ContextTierTableProps = {
  /** per_token: interval + input/output/cache prices; per_call: interval + one price */
  kind: 'per_token' | 'per_call'
  /** Stored tiers JSON; '' when unset */
  value: string
  onChange: (tiersJson: string) => void
}

/**
 * Spreadsheet-style editor for context interval pricing: one row per
 * interval, one input for the upper bound and one price input per priced
 * lane. The blank-bound row is the catch-all ("above the last bound") and
 * always sorts last. The grid uses minmax(0, ·) columns so narrow sheets
 * shrink inputs instead of overflowing horizontally.
 */
export function ContextTierTable({ kind, value, onChange }: ContextTierTableProps) {
  const { t } = useTranslation()
  const isPerCall = kind === 'per_call'
  // Rows are the editing source of truth; the parent holds the serialized
  // JSON. The prop is re-parsed only when it changes from outside (e.g. the
  // sheet loaded another model), otherwise fully-blank rows would collapse
  // ("add interval" on an empty table serializes to '' and back).
  const [rows, setRows] = useState<ContextTierRow[]>(() => parseTiersValue(value).rows)
  const [parseError, setParseError] = useState(
    () => parseTiersValue(value).parseError
  )
  const [validationError, setValidationError] =
    useState<TierValidationError | null>(null)
  const lastSerialized = useRef(value)
  useEffect(() => {
    if (value !== lastSerialized.current) {
      const parsed = parseTiersValue(value)
      setRows(parsed.rows)
      setParseError(parsed.parseError)
      setValidationError(null)
      lastSerialized.current = value
    }
  }, [value])

  const commit = (nextRows: ContextTierRow[]) => {
    const json = serializeRows(nextRows, kind)
    lastSerialized.current = json
    setRows(nextRows)
    setParseError('')
    // Live row-order check: bounds and every price lane must strictly
    // increase top-to-bottom so a mis-typed row is flagged while editing.
    setValidationError(
      isPerCall
        ? validateContextCallTiersStructured(json)
        : validateContextTiersStructured(json)
    )
    onChange(json)
  }

  const updateRow = (index: number, patch: Partial<ContextTierRow>) => {
    const next = rows.map((row, i) => (i === index ? { ...row, ...patch } : row))
    commit(next)
  }

  const removeRow = (index: number) => {
    const next = rows.filter((_, i) => i !== index)
    commit(next.length > 0 ? next : [emptyRow()])
  }

  const addRow = () => {
    // Insert before a trailing blank-bound row so the catch-all stays last;
    // otherwise append. Ordering is not enforced — the backend sorts tiers.
    const next = [...rows]
    const fresh = emptyRow()
    const last = next[next.length - 1]
    if (last && last.maxContext.trim() === '') next.splice(next.length - 1, 0, fresh)
    else next.push(fresh)
    commit(next)
  }

  // Desktop: one row per interval. Mobile: the bound takes the full width,
  // prices pair up two per line with inline labels (the header row is hidden),
  // so nothing overflows a phone-width sheet.
  const gridClass = isPerCall
    ? 'grid grid-cols-2 gap-2 sm:grid-cols-[minmax(0,1.5fr)_minmax(0,1.1fr)_auto] sm:items-center sm:gap-x-2'
    : 'grid grid-cols-2 gap-2 sm:grid-cols-[minmax(0,1.1fr)_repeat(4,minmax(0,1fr))_auto] sm:items-center sm:gap-x-2'

  const boundInput = (row: ContextTierRow, index: number) => (
    <InputGroup className='h-9 min-w-0'>
      <InputGroupInput
        inputMode='text'
        autoComplete='off'
        className='text-sm'
        placeholder={t('Above')}
        aria-label={t('Context upper bound')}
        value={row.maxContext}
        onChange={(event) => {
          const raw = event.target.value
          // Allow digits, one dot, optional k/M suffix and spaces around it.
          if (raw.trim() !== '' && !/^\d*\.?\d*\s*[kKmM]?$/.test(raw)) return
          updateRow(index, { maxContext: raw })
        }}
      />
      <InputGroupAddon
        align='inline-end'
        className='text-muted-foreground text-xs'
      >
        {t('tokens')}
      </InputGroupAddon>
    </InputGroup>
  )

  const labelledCell = (labelKey: string, control: ReactNode) => (
    <label className='flex min-w-0 flex-col gap-1 sm:gap-0'>
      <span className='text-muted-foreground text-xs sm:hidden'>
        {t(labelKey)}
      </span>
      {control}
    </label>
  )

  const boundCell = (row: ContextTierRow, index: number) => (
    <div className='col-span-2 flex min-w-0 flex-col gap-1 sm:col-span-1 sm:gap-0'>
      <span className='text-muted-foreground text-xs sm:hidden'>
        {t('Context up to (tokens)')}
      </span>
      {boundInput(row, index)}
    </div>
  )

  const deleteCell = (index: number) => (
    <div className='col-span-2 flex justify-end sm:col-span-1 sm:justify-center'>
      <Button
        type='button'
        variant='ghost'
        size='icon'
        className='size-8'
        aria-label={t('Remove interval')}
        onClick={() => removeRow(index)}
      >
        <Trash2 data-icon='inline-start' />
      </Button>
    </div>
  )

  return (
    <div className='flex w-full min-w-0 flex-col gap-2'>
      {parseError !== '' ? (
        <p className='text-destructive text-sm'>
          {t('Saved tier JSON is invalid; edit the rows below to replace it.')}
        </p>
      ) : null}
      {validationError ? (
        <p className='text-destructive text-sm'>
          {formatTierValidationError(validationError, t)}
        </p>
      ) : null}
      <div className='min-w-0'>
        <div
          className={`${gridClass} text-muted-foreground hidden items-center pb-1 text-xs sm:grid`}
        >
          <span>{t('Context up to (tokens)')}</span>
          {isPerCall ? (
            <span>{t('Price per request ($)')}</span>
          ) : (
            <>
              <span>{t('Input $ / 1M')}</span>
              <span>{t('Output $ / 1M')}</span>
              <span>{t('Cache read $ / 1M')}</span>
              <span>{t('Cache write $ / 1M')}</span>
            </>
          )}
          <span aria-hidden />
        </div>
        <div className='flex flex-col gap-2 sm:gap-1'>
          {rows.map((row, index) => (
            <div
              key={index}
              className={`${gridClass} rounded-md border p-2 sm:rounded-none sm:border-0 sm:p-0`}
            >
              {boundCell(row, index)}
              {isPerCall
                ? labelledCell(
                    'Price per request ($)',
                    <PriceInput
                      value={row.price}
                      suffix={null}
                      onChange={(price) => updateRow(index, { price })}
                    />
                  )
                : [
                    labelledCell(
                      'Input $ / 1M',
                      <PriceInput
                        value={row.inputPrice}
                        suffix={null}
                        onChange={(inputPrice) =>
                          updateRow(index, { inputPrice })
                        }
                      />
                    ),
                    labelledCell(
                      'Output $ / 1M',
                      <PriceInput
                        value={row.outputPrice}
                        suffix={null}
                        onChange={(outputPrice) =>
                          updateRow(index, { outputPrice })
                        }
                      />
                    ),
                    labelledCell(
                      'Cache read $ / 1M',
                      <PriceInput
                        value={row.cachePrice}
                        suffix={null}
                        onChange={(cachePrice) =>
                          updateRow(index, { cachePrice })
                        }
                      />
                    ),
                    labelledCell(
                      'Cache write $ / 1M',
                      <PriceInput
                        value={row.cacheWritePrice}
                        suffix={null}
                        onChange={(cacheWritePrice) =>
                          updateRow(index, { cacheWritePrice })
                        }
                      />
                    ),
                  ]}
              {deleteCell(index)}
            </div>
          ))}
        </div>
        <Button
          type='button'
          variant='outline'
          size='sm'
          className='mt-2'
          onClick={addRow}
        >
          <Plus data-icon='inline-start' />
          {t('Add interval')}
        </Button>
      </div>
    </div>
  )
}

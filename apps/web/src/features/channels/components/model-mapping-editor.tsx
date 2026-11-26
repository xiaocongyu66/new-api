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
import { Code, Plus, Table, Trash2 } from 'lucide-react'
import { type ReactNode, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { JsonCodeEditor } from '@/components/json-code-editor'
import { SearchableTextInput } from '@/components/searchable-text-input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

type ModelMappingEditorProps = {
  value: string
  onChange: (value: string) => void
  disabled?: boolean
  header?: ReactNode
  plazaModelOptions?: string[]
  upstreamModelOptions?: string[]
}

type MappingRow = {
  id: string
  from: string
  to: string
}

const DUPLICATE_MAPPING_SENTINEL = '{ "duplicate_source_models": '



function getDuplicateSources(rows: MappingRow[]): string[] {
  const seen = new Set<string>()
  const duplicates = new Set<string>()

  for (const row of rows) {
    const source = row.from.trim()
    if (!source) continue
    if (seen.has(source)) {
      duplicates.add(source)
    } else {
      seen.add(source)
    }
  }

  return Array.from(duplicates)
}



export function ModelMappingEditor(props: ModelMappingEditorProps) {
  const { t } = useTranslation()
  const [mode, setMode] = useState<'visual' | 'json'>('visual')
  const [rows, setRows] = useState<MappingRow[]>([])
  const [jsonValue, setJsonValue] = useState(props.value)
  const [jsonError, setJsonError] = useState<string | null>(null)
  const nextRowIdRef = useRef(0)
  const lastEmittedValueRef = useRef<string | null>(null)
  const duplicateSources = useMemo(() => getDuplicateSources(rows), [rows])
  const plazaOptions = useMemo(
    () =>
      Array.from(
        new Set([
          ...(props.plazaModelOptions ?? []),
          ...rows.map((row) => row.from.trim()).filter(Boolean),
        ])
      ),
    [props.plazaModelOptions, rows]
  )
  const upstreamOptions = useMemo(
    () =>
      Array.from(
        new Set([
          ...(props.upstreamModelOptions ?? []),
          ...rows.map((row) => row.to.trim()).filter(Boolean),
        ])
      ),
    [props.upstreamModelOptions, rows]
  )

  const createRowId = () => {
    nextRowIdRef.current += 1
    return `mapping-${nextRowIdRef.current}`
  }

  const parseJsonToRows = (json: string): boolean => {
    try {
      if (!json.trim()) {
        setRows([])
        setJsonError(null)
        return true
      }
      const parsed = JSON.parse(json)
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        setJsonError(t('Model mapping must be a valid JSON object'))
        return false
      }
      const entries = Object.entries(parsed)
      const invalidValue = entries.find(([, to]) => typeof to !== 'string')
      if (invalidValue) {
        setJsonError(t('Model mapping values must be strings'))
        return false
      }
      setRows((previousRows) => {
        const remainingRows = [...previousRows]
        return entries.map(([from, to], index) => {
          const toString = String(to)
          const existingIndex = remainingRows.findIndex(
            (row) =>
              row.from === from ||
              (row.from === from && row.to === toString) ||
              previousRows[index]?.id === row.id
          )
          if (existingIndex >= 0) {
            const [existing] = remainingRows.splice(existingIndex, 1)
            return {
              id: existing.id,
              from,
              to: toString,
            }
          }
          return {
            id: createRowId(),
            from,
            to: toString,
          }
        })
      })
      setJsonError(null)
      return true
    } catch (_error) {
      setJsonError(t('Model mapping must be valid JSON format'))
      return false
    }
  }

  // Parse JSON to rows when value changes externally
  useEffect(() => {
    const emittedValue = lastEmittedValueRef.current
    lastEmittedValueRef.current = null
    if (emittedValue === props.value) return
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setJsonValue(props.value)
    parseJsonToRows(props.value)
  }, [props.value])

  const convertRowsToJson = (updatedRows: MappingRow[]): string => {
    // Blank rows are UI state only; emitting "{}" would make the value effect wipe them.
    if (!updatedRows.some((row) => row.from.trim())) {
      return ''
    }
    const obj: Record<string, string> = {}
    updatedRows.forEach((row) => {
      if (row.from.trim()) {
        obj[row.from.trim()] = row.to.trim()
      }
    })
    return JSON.stringify(obj, null, 2)
  }

  const emitValue = (json: string) => {
    lastEmittedValueRef.current = json
    setJsonValue(json)
    props.onChange(json)
  }

  const syncRows = (updatedRows: MappingRow[]) => {
    setRows(updatedRows)
    const duplicates = getDuplicateSources(updatedRows)
    if (duplicates.length > 0) {
      setJsonError(t('Duplicate source model mappings are not allowed'))
      emitValue(DUPLICATE_MAPPING_SENTINEL)
      return
    }

    const json = convertRowsToJson(updatedRows)
    setJsonError(null)
    emitValue(json)
  }

  const handleAddRow = () => {
    const newRow: MappingRow = {
      id: createRowId(),
      from: '',
      to: '',
    }
    syncRows([...rows, newRow])
  }

  const handleDeleteRow = (id: string) => {
    syncRows(rows.filter((row) => row.id !== id))
  }

  const handleRowChange = (
    id: string,
    field: 'from' | 'to',
    newValue: string
  ) => {
    const updatedRows = rows.map((row) =>
      row.id === id ? { ...row, [field]: newValue } : row
    )
    syncRows(updatedRows)
  }


  const handleJsonChange = (newJson: string) => {
    emitValue(newJson)
    parseJsonToRows(newJson)
  }

  const handleFillTemplate = () => {
    const template = JSON.stringify(
      { 'gpt-3.5-turbo': 'gpt-3.5-turbo-0125' },
      null,
      2
    )
    emitValue(template)
    parseJsonToRows(template)
  }

  const handleModeChange = (nextMode: string) => {
    if (nextMode !== 'visual' && nextMode !== 'json') return
    if (nextMode === 'json') {
      const duplicates = getDuplicateSources(rows)
      if (duplicates.length === 0) {
        emitValue(convertRowsToJson(rows))
      }
      setMode('json')
      return
    }
    parseJsonToRows(jsonValue)
    setMode('visual')
  }

  return (
    <div className='space-y-2'>
      <Tabs value={mode} onValueChange={handleModeChange} className='space-y-2'>
        <div className='flex flex-wrap items-center gap-x-2 gap-y-1'>
          {props.header}
          <TabsList>
            <TabsTrigger value='visual' className='text-xs'>
              <Table className='h-3 w-3' aria-hidden='true' />
              {t('Visual')}
            </TabsTrigger>
            <TabsTrigger value='json' className='text-xs'>
              <Code className='h-3 w-3' aria-hidden='true' />
              {t('JSON')}
            </TabsTrigger>
          </TabsList>
          <Button
            type='button'
            variant='link'
            size='sm'
            className='ml-auto h-auto p-0'
            onClick={handleFillTemplate}
            disabled={props.disabled}
          >
            {t('Fill Template')}
          </Button>
        </div>

        {jsonError && (
          <Alert variant='destructive'>
            <AlertDescription>{jsonError}</AlertDescription>
          </Alert>
        )}

        {duplicateSources.length > 0 && (
          <Alert>
            <AlertDescription>
              {t('Duplicate source model(s): {{models}}', {
                models: duplicateSources.join(', '),
              })}
            </AlertDescription>
          </Alert>
        )}

        <TabsContent value='visual' className='space-y-2'>
          {rows.length > 0 ? (
            <div className='space-y-2'>
              {rows.map((row) => (
                <div
                  key={row.id}
                  className='flex items-center gap-2 transition-all duration-200'
                >
                  <SearchableTextInput
                    value={row.from}
                    onChange={(value) => handleRowChange(row.id, 'from', value)}
                    options={plazaOptions}
                    placeholder='gpt-3.5-turbo'
                    ariaLabel={t('Plaza Model')}
                    disabled={props.disabled}
                  />
                  <SearchableTextInput
                    value={row.to}
                    onChange={(value) => handleRowChange(row.id, 'to', value)}
                    options={upstreamOptions}
                    placeholder='gpt-3.5-turbo-0125'
                    ariaLabel={t('Upstream Model')}
                    disabled={props.disabled}
                  />
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    onClick={() => handleDeleteRow(row.id)}
                    disabled={props.disabled}
                    className='h-6 w-6 shrink-0 transition-colors duration-150'
                    aria-label={t('Delete mapping')}
                  >
                    <Trash2 className='h-3 w-3' aria-hidden='true' />
                  </Button>
                </div>
              ))}
            </div>
          ) : (
            <div className='text-muted-foreground flex h-24 items-center justify-center rounded-md border border-dashed text-sm'>
              {t(
                'No model mappings configured. Click "Add Mapping" to get started.'
              )}
            </div>
          )}
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={handleAddRow}
            disabled={props.disabled}
            className='w-full transition-colors duration-150'
          >
            <Plus className='mr-2 h-4 w-4' />
            {t('Add Mapping')}
          </Button>
        </TabsContent>
        <TabsContent value='json'>
          <JsonCodeEditor
            value={jsonValue}
            onChange={handleJsonChange}
            placeholder={t('{"original-model": "replacement-model"}')}
            disabled={props.disabled}
            className={jsonError ? 'border-destructive' : undefined}
            aria-invalid={Boolean(jsonError)}
            ariaLabel={t('Model Mapping')}
          />
        </TabsContent>
      </Tabs>

    </div>
  )
}

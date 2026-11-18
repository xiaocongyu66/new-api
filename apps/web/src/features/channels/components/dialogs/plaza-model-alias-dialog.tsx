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
import { Plus, Trash2, X } from 'lucide-react'
import { useMemo, useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

type PlazaModelEditorDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  models: string[]
  onSave: (models: string[]) => void
}

const INPUT_CLASSNAME =
  'border-input bg-transparent dark:bg-input/30 h-8 min-w-0 rounded-lg border px-2.5 py-1 text-xs outline-none transition-colors focus-visible:ring-2'

type RankedModel = { value: string; index: number; score: number }

function matchModelScore(model: string, query: string): number | null {
  if (!query) return 0
  const lower = model.toLowerCase()
  if (lower === query) return 0
  if (lower.startsWith(query)) return 1
  const direct = lower.indexOf(query)
  if (direct >= 0) return 2 + direct / lower.length

  let queryIndex = 0
  let gaps = 0
  let previous = -1
  let firstMatch = -1
  for (let i = 0; i < lower.length && queryIndex < query.length; i += 1) {
    if (lower[i] === query[queryIndex]) {
      if (firstMatch < 0) firstMatch = i
      if (previous >= 0) gaps += i - previous - 1
      previous = i
      queryIndex += 1
    }
  }
  if (queryIndex < query.length) return null
  return 10 + gaps / lower.length + (2 * firstMatch) / lower.length
}


export function PlazaModelEditorDialog({
  open,
  onOpenChange,
  models,
  onSave,
}: PlazaModelEditorDialogProps) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<string[]>(models)
  const [newModel, setNewModel] = useState('')

  useEffect(() => {
    if (open) {
      setDraft(models)
      setNewModel('')
    }
  }, [open, models])
  const visibleModels = useMemo<RankedModel[]>(() => {
    const query = newModel.trim().toLowerCase()
    const entries = draft.map((value, index) => ({ value, index, score: 0 }))
    if (!query) return entries
    return entries
      .map((entry) => ({
        ...entry,
        score: matchModelScore(entry.value, query) ?? Number.POSITIVE_INFINITY,
      }))
      .filter((entry) => Number.isFinite(entry.score))
      .sort((a, b) => a.score - b.score || a.index - b.index)
  }, [draft, newModel])

  if (!open) return null

  const handleAdd = () => {
    const model = newModel.trim()
    if (!model || draft.includes(model)) return
    setDraft((current) => [...current, model])
    setNewModel('')
  }

  const handleSave = () => {
    onSave(draft.map((model) => model.trim()).filter(Boolean))
    onOpenChange(false)
  }

  return createPortal(
    <div className='fixed inset-0 z-50 flex items-center justify-center p-4'>
      <button
        type='button'
        aria-label={t('Close')}
        className='absolute inset-0 cursor-default bg-black/50 transition-opacity duration-150'
        onClick={() => onOpenChange(false)}
      />
      <div
        role='dialog'
        aria-modal='true'
        className='bg-[var(--float-surface)] relative flex max-h-[85vh] w-full max-w-lg flex-col gap-3 rounded-2xl p-4 shadow-xl transition-all duration-150'
      >
        <div className='flex items-start justify-between gap-3'>
          <div className='min-w-0'>
            <h2 className='text-base font-semibold'>
              {t('Edit Plaza Models')}
            </h2>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t(
                'Rename or delete plaza models, then add new ones. Saved models appear in the plaza model dropdown.'
              )}
            </p>
          </div>
          <Button
            type='button'
            variant='ghost'
            size='icon-sm'
            onClick={() => onOpenChange(false)}
            aria-label={t('Close')}
          >
            <X className='h-4 w-4' />
          </Button>
        </div>
        <div className='min-h-0 flex-1 space-y-1 overflow-y-auto pr-1'>
          {visibleModels.map((entry) => (
            <div
              key={`${entry.value}-${entry.index}`}
              className='flex items-center gap-2'
            >
              <input
                value={entry.value}
                onChange={(event) =>
                  setDraft((current) =>
                    current.map((item, itemIndex) =>
                      itemIndex === entry.index ? event.target.value : item
                    )
                  )
                }
                aria-label={t('Plaza model name')}
                className={`${INPUT_CLASSNAME} min-w-0 flex-1`}
              />
              <Button
                type='button'
                variant='ghost'
                size='icon-sm'
                onClick={() =>
                  setDraft((current) =>
                    current.filter((_, itemIndex) => itemIndex !== entry.index)
                  )
                }
                aria-label={t('Delete plaza model')}
              >
                <Trash2 className='h-3.5 w-3.5 text-destructive' />
              </Button>
            </div>
          ))}
        </div>
        <div className='border-border/60 flex items-center gap-2 border-t pt-3'>
          <input
            value={newModel}
            onChange={(event) => setNewModel(event.target.value)}
            placeholder={t('Plaza model name')}
            aria-label={t('Plaza model name')}
            className={`${INPUT_CLASSNAME} min-w-0 flex-1`}
          />
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={handleAdd}
            disabled={!newModel.trim()}
            aria-label={t('Add plaza model')}
          >
            <Plus className='h-3.5 w-3.5' />
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='icon-sm'
            onClick={() => setNewModel('')}
            aria-label={t('Clear filter')}
          >
            <X className='h-3.5 w-3.5' />
          </Button>
        </div>
        <div className='flex justify-end gap-2 pt-1'>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleSave}>{t('Save')}</Button>
        </div>
      </div>
    </div>,
    document.body
  )
}

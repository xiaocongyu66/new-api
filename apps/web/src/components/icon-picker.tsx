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
import { Search, X } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { getLobeIcon, LOBE_ICON_KEYS } from '@/lib/lobe-icon'

type IconPickerProps = {
  value?: string
  onChange: (value: string) => void
}

export function IconPicker({ value, onChange }: IconPickerProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')

  const keys = useMemo(
    () =>
      LOBE_ICON_KEYS.filter((key) =>
        key.toLowerCase().includes(query.trim().toLowerCase())
      ),
    [query]
  )

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) setQuery('')
      }}
    >
      <PopoverTrigger
        render={
          <Button
            type='button'
            variant='outline'
            role='combobox'
            className='w-full justify-start font-normal'
          />
        }
      >
        {value ? (
          <>
            <span className='flex size-5 shrink-0 items-center justify-center'>
              {getLobeIcon(value, 18)}
            </span>
            <span className='truncate'>{value}</span>
          </>
        ) : (
          <span className='text-muted-foreground'>{t('Select an icon')}</span>
        )}
      </PopoverTrigger>
      <PopoverContent align='start' className='w-72 space-y-2 p-2'>
        <div className='relative'>
          <Search className='text-muted-foreground absolute top-1/2 left-2 size-4 -translate-y-1/2' />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t('Search icons')}
            className='h-8 pl-8'
          />
        </div>
        <div className='grid max-h-56 grid-cols-6 gap-1 overflow-y-auto'>
          {keys.length === 0 ? (
            <div className='text-muted-foreground col-span-6 flex h-20 items-center justify-center text-sm'>
              {t('No results found')}
            </div>
          ) : (
            keys.map((key) => (
              <button
                key={key}
                type='button'
                title={key}
                aria-label={key}
                onClick={() => {
                  onChange(key)
                  setOpen(false)
                }}
                className={`flex size-9 items-center justify-center rounded-md border ${
                  key === value
                    ? 'border-primary bg-primary/10'
                    : 'hover:bg-accent border-transparent'
                }`}
              >
                {getLobeIcon(key, 22)}
              </button>
            ))
          )}
        </div>
        {value && (
          <Button
            type='button'
            variant='ghost'
            size='sm'
            className='w-full'
            onClick={() => {
              onChange('')
              setOpen(false)
            }}
          >
            <X className='size-4' />
            {t('Clear icon')}
          </Button>
        )}
      </PopoverContent>
    </Popover>
  )
}

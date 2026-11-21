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
import { ArrowUp, ChevronDown } from 'lucide-react'
import { useState } from 'react'
import { createPortal } from 'react-dom'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

export type FloatingSectionItem = {
  id: string
  title: string
}

type FloatingSectionNavProps = {
  items: FloatingSectionItem[]
  activeItemId?: string
  onNavigate: (targetId: string) => void
  navigationLabel: string
}

/**
 * Compact section switcher pinned to the bottom-right of a scrolling drawer.
 * Long drawers get a capsule showing the section currently in view; tapping it
 * opens the full section list.
 */
export function FloatingSectionNav({
  items,
  activeItemId,
  onNavigate,
  navigationLabel,
}: FloatingSectionNavProps) {
  const [open, setOpen] = useState(false)
  const activeItem = items.find((item) => item.id === activeItemId)

  return createPortal(
    <div className='fixed right-14 bottom-4 z-[60] lg:hidden'>
      <div className='relative'>
        <button
          type='button'
          className='bg-background/95 border-border/60 flex items-center gap-1 rounded-full border px-2 py-1 text-left shadow-sm backdrop-blur'
          aria-expanded={open}
          aria-label={navigationLabel}
          onClick={() => setOpen((value) => !value)}
        >
          <span className='max-w-[7rem] truncate text-[11px] font-medium'>
            {activeItem?.title ?? navigationLabel}
          </span>
          <ChevronDown
            className={cn(
              'text-muted-foreground size-3 shrink-0 transition-transform',
              open && 'rotate-180'
            )}
            aria-hidden='true'
          />
        </button>
        {open && (
          <>
            <button
              type='button'
              className='fixed inset-0 z-0 cursor-default'
              aria-label={navigationLabel}
              onClick={() => setOpen(false)}
            />
            <nav
              className='border-border/60 bg-background absolute bottom-full right-0 z-10 mb-1 max-h-72 w-56 overflow-y-auto rounded-lg border p-1 shadow-lg'
              aria-label={navigationLabel}
            >
              {items.map((item) => {
                const isActive = activeItemId === item.id
                return (
                  <button
                    key={item.id}
                    type='button'
                    className={cn(
                      'hover:bg-muted/60 flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-xs',
                      isActive && 'bg-primary/10 font-medium text-primary'
                    )}
                    aria-current={isActive ? 'true' : undefined}
                    onClick={() => {
                      onNavigate(item.id)
                      setOpen(false)
                    }}
                  >
                    <span className='min-w-0 flex-1 truncate'>
                      {item.title}
                    </span>
                  </button>
                )
              })}
            </nav>
          </>
        )}
      </div>
    </div>,
    document.body
  )
}

type FloatingBackToTopProps = {
  visible: boolean
  onClick: () => void
  label: string
}

/** Round back-to-top button shared by long drawers. */
export function FloatingBackToTop({
  visible,
  onClick,
  label,
}: FloatingBackToTopProps) {
  if (!visible) return null
  return createPortal(
    <Button
      type='button'
      size='icon'
      className='fixed right-4 bottom-4 z-[60] rounded-full shadow-lg lg:bottom-6'
      onClick={onClick}
      aria-label={label}
    >
      <ArrowUp className='h-4 w-4' aria-hidden='true' />
    </Button>,
    document.body
  )
}
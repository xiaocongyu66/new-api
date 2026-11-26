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
import {
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
} from 'react'
import { createPortal } from 'react-dom'

import { Input } from '@/components/ui/input'

export type SearchableTextInputProps = {
  value: string
  onChange: (value: string) => void
  options: string[]
  placeholder: string
  ariaLabel: string
  disabled?: boolean
}

const ITEM_HEIGHT = 28
const VIEWPORT_HEIGHT = 252
const OVERSCAN = 6
const VISIBLE_COUNT = Math.ceil(VIEWPORT_HEIGHT / ITEM_HEIGHT)

export function SearchableTextInput(props: SearchableTextInputProps) {
  const [open, setOpen] = useState(false)
  const [inputValue, setInputValue] = useState(props.value)
  const [highlightIndex, setHighlightIndex] = useState(-1)
  const [scrollTop, setScrollTop] = useState(0)
  const [popupWidth, setPopupWidth] = useState<number | null>(null)
  const [position, setPosition] = useState({ top: 0, left: 0 })
  const containerRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const optionsRef = useRef(props.options)
  const totalRef = useRef(0)
  const popupWidthRef = useRef<number | null>(null)

  optionsRef.current = props.options

  useLayoutEffect(() => setInputValue(props.value), [props.value])

  useLayoutEffect(() => {
    setScrollTop(0)
    setHighlightIndex(-1)
    if (listRef.current) listRef.current.scrollTop = 0
  }, [open, inputValue])

  const filteredOptions = useMemo(() => {
    const query = inputValue.trim().toLowerCase()
    if (!query) return props.options
    return props.options.filter((option) =>
      option.toLowerCase().includes(query)
    )
  }, [props.options, inputValue])

  const total = filteredOptions.length
  totalRef.current = total
  const startIndex = Math.min(
    Math.max(0, Math.floor(scrollTop / ITEM_HEIGHT) - OVERSCAN),
    total
  )
  const endIndex = Math.min(
    total,
    startIndex + VISIBLE_COUNT + OVERSCAN * 2
  )
  const visibleOptions = filteredOptions.slice(startIndex, endIndex)

  const updatePosition = useCallback(() => {
    const input = inputRef.current
    if (!input) return
    const rect = input.getBoundingClientRect()
    const height = Math.min(totalRef.current * ITEM_HEIGHT, VIEWPORT_HEIGHT) + 2
    const width = popupWidthRef.current ?? rect.width
    const showAbove =
      window.innerHeight - rect.bottom < height + 8 && rect.top > height
    setPosition({
      top: showAbove ? rect.top - height - 4 : rect.bottom + 4,
      left: Math.min(rect.left, window.innerWidth - width - 8),
    })
  }, [])

  useLayoutEffect(() => {
    if (!open) return
    const input = inputRef.current
    const context = document.createElement('canvas').getContext('2d')
    if (input && context) {
      context.font = `14px ${getComputedStyle(input).fontFamily}`
      let maxWidth = 0
      for (const option of optionsRef.current) {
        maxWidth = Math.max(maxWidth, context.measureText(option).width)
      }
      const width = Math.round(
        Math.min(Math.max(maxWidth + 64, input.getBoundingClientRect().width), 640)
      )
      popupWidthRef.current = width
      setPopupWidth(width)
    }
    updatePosition()
    const handleScroll = (event: Event) => {
      if (event.target === listRef.current) return
      updatePosition()
    }
    window.addEventListener('scroll', handleScroll, true)
    window.addEventListener('resize', updatePosition)
    return () => {
      window.removeEventListener('scroll', handleScroll, true)
      window.removeEventListener('resize', updatePosition)
    }
  }, [open, updatePosition])

  useLayoutEffect(() => {
    if (!open) return
    const handleOutsideClick = (event: MouseEvent) => {
      const target = event.target as Node
      if (
        !containerRef.current?.contains(target) &&
        !listRef.current?.contains(target)
      ) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handleOutsideClick)
    return () => document.removeEventListener('mousedown', handleOutsideClick)
  }, [open])

  const selectOption = (value: string) => {
    setInputValue(value)
    props.onChange(value)
    setOpen(false)
    inputRef.current?.focus()
  }

  const moveHighlight = (delta: number) => {
    if (total === 0) return
    let next = highlightIndex
    if (next < 0) next = delta > 0 ? 0 : total - 1
    else next = Math.min(total - 1, Math.max(0, next + delta))
    setHighlightIndex(next)
    const list = listRef.current
    if (!list) return
    const top = next * ITEM_HEIGHT
    if (top < list.scrollTop) list.scrollTop = top
    else if (top + ITEM_HEIGHT > list.scrollTop + VIEWPORT_HEIGHT) {
      list.scrollTop = top + ITEM_HEIGHT - VIEWPORT_HEIGHT
    }
  }

  const handleKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (props.disabled) return
    if (!open && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
      event.preventDefault()
      setOpen(true)
      return
    }
    if (!open) return

    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault()
        moveHighlight(1)
        break
      case 'ArrowUp':
        event.preventDefault()
        moveHighlight(-1)
        break
      case 'Enter':
        event.preventDefault()
        if (highlightIndex >= 0 && filteredOptions[highlightIndex]) {
          selectOption(filteredOptions[highlightIndex])
        } else {
          setOpen(false)
        }
        break
      case 'Escape':
        event.preventDefault()
        setOpen(false)
        break
    }
  }

  return (
    <div ref={containerRef} className='min-w-0 flex-1'>
      <Input
        ref={inputRef}
        role='combobox'
        aria-expanded={open}
        aria-haspopup='listbox'
        aria-autocomplete='list'
        autoComplete='off'
        value={inputValue}
        placeholder={props.placeholder}
        aria-label={props.ariaLabel}
        disabled={props.disabled}
        onChange={(event) => {
          setInputValue(event.target.value)
          props.onChange(event.target.value)
          setOpen(true)
        }}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        onPointerDown={() => {
          if (document.activeElement === inputRef.current && !open) {
            setOpen(true)
          }
        }}
        onKeyDown={handleKeyDown}
        className='w-full min-w-0 text-xs'
      />
      {open &&
        total > 0 &&
        createPortal(
          <div
            ref={listRef}
            role='listbox'
            aria-label={props.ariaLabel}
            onScroll={(event) => setScrollTop(event.currentTarget.scrollTop)}
            style={{
              top: position.top,
              left: position.left,
              width: popupWidth ?? undefined,
              maxHeight: VIEWPORT_HEIGHT,
            }}
            className='bg-[var(--float-surface)] text-popover-foreground fixed z-50 overflow-y-auto overscroll-contain rounded-lg border shadow-md'
          >
            <div
              style={{ height: startIndex * ITEM_HEIGHT }}
              aria-hidden='true'
            />
            {visibleOptions.map((option, offset) => {
              const index = startIndex + offset
              return (
                <div
                  key={option}
                  role='option'
                  aria-selected={props.value === option}
                  data-highlighted={index === highlightIndex}
                  onMouseEnter={() => setHighlightIndex(index)}
                  onMouseDown={(event) => {
                    event.preventDefault()
                    selectOption(option)
                  }}
                  className='flex h-7 cursor-pointer items-center gap-2 px-2 text-sm data-[highlighted=true]:bg-accent data-[highlighted=true]:text-accent-foreground'
                >
                  <span className='block truncate'>{option}</span>
                </div>
              )
            })}
            <div
              style={{ height: (total - endIndex) * ITEM_HEIGHT }}
              aria-hidden='true'
            />
          </div>,
          document.body
        )}
    </div>
  )
}

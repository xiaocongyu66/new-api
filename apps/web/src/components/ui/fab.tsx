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
import { mergeProps } from '@base-ui/react/merge-props'
import { cva, type VariantProps } from 'class-variance-authority'
import * as React from 'react'

import { useRipple } from '@/hooks/use-ripple'
import { cn } from '@/lib/utils'

import { RippleContainer } from './ripple'

const fabVariants = cva(
  "group/fab relative inline-flex shrink-0 items-center justify-center overflow-hidden rounded-2xl font-medium whitespace-nowrap transition-all outline-none select-none focus-visible:ring-3 focus-visible:ring-ring/50 active:translate-y-px disabled:pointer-events-none disabled:opacity-50 after:pointer-events-none after:absolute after:inset-0 after:rounded-[inherit] after:bg-current after:opacity-0 after:content-[''] after:transition-opacity hover:after:opacity-[0.08] focus-visible:after:opacity-[0.1] active:after:opacity-[0.12] [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-6",
  {
    variants: {
      variant: {
        primary: 'bg-accent text-primary',
        secondary: 'bg-secondary text-secondary-foreground',
        surface: 'bg-[var(--float-surface)] text-primary shadow-sm ring-foreground/10 ring-1',
      },
      size: {
        sm: 'size-10 rounded-xl',
        default: 'size-14',
        lg: 'size-24 rounded-3xl [&_svg:not([class*="size-"])]:size-9',
        extended: 'h-14 w-auto gap-3 rounded-2xl px-5 text-sm [&_svg:not([class*="size-"])]:size-5',
      },
    },
    defaultVariants: {
      variant: 'primary',
      size: 'default',
    },
  }
)

type FabProps = React.ComponentProps<'button'> &
  VariantProps<typeof fabVariants>

function Fab({
  className,
  variant = 'primary',
  size = 'default',
  children,
  ...props
}: FabProps) {
  const { onPointerDown, removeRipple, ripples } = useRipple()

  return (
    <button
      type='button'
      data-slot='fab'
      data-variant={variant}
      data-size={size}
      className={cn(fabVariants({ variant, size }), className)}
      {...mergeProps({ onPointerDown }, props)}
    >
      <RippleContainer ripples={ripples} onRemove={removeRipple} />
      {children}
    </button>
  )
}

export { Fab, fabVariants }
export type { FabProps }

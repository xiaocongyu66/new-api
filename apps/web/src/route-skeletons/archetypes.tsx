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
import type { ReactNode } from 'react'

import { SkeletonBlock } from '@/components/loading-state'
import { cn } from '@/lib/utils'

/**
 * Layout shells and content archetypes for route-level pending skeletons.
 *
 * The router's root `beforeLoad` re-runs on every navigation (session
 * bootstrap/refresh), so a pending window opens on many switches. These
 * archetypes mirror the real layout chrome (header / sidebar rail /
 * secondary panel) plus the dominant content shape of each page group, so
 * the pending state reads as "this page is loading" instead of a generic
 * gray block. Blocks are pure placeholders: no data hooks, no copy.
 */

export function PublicShellSkeleton({ children }: { children: ReactNode }) {
  return (
    <div
      role='status'
      aria-live='polite'
      className='bg-background text-foreground flex min-h-svh flex-col'
    >
      <header className='flex h-16 shrink-0 items-center gap-3 border-b px-4 sm:px-6'>
        <SkeletonBlock className='size-9 rounded-lg' />
        <SkeletonBlock className='h-5 w-24' />
        <div className='ml-auto flex items-center gap-2'>
          <SkeletonBlock className='hidden h-5 w-14 md:block' />
          <SkeletonBlock className='hidden h-5 w-14 md:block' />
          <SkeletonBlock className='hidden h-5 w-14 md:block' />
          <SkeletonBlock className='ml-2 h-9 w-20 rounded-lg' />
          <SkeletonBlock className='size-9 rounded-full' />
        </div>
      </header>
      {children}
    </div>
  )
}

export function AuthedShellSkeleton({ children }: { children: ReactNode }) {
  return (
    <div role='status' aria-live='polite' className='flex min-h-svh flex-col'>
      <header className='flex h-14 shrink-0 items-center gap-3 border-b px-4'>
        <SkeletonBlock className='size-8 rounded-md' />
        <SkeletonBlock className='hidden h-5 w-28 sm:block' />
        <div className='ml-auto flex items-center gap-2'>
          <SkeletonBlock className='hidden h-8 w-40 rounded-lg md:block' />
          <SkeletonBlock className='size-8 rounded-md' />
          <SkeletonBlock className='size-8 rounded-md' />
          <SkeletonBlock className='size-8 rounded-full' />
        </div>
      </header>
      <div className='flex min-h-0 flex-1'>
        <div className='hidden w-16 shrink-0 flex-col items-center gap-2 border-r p-2 sm:flex'>
          {[1, 2, 3, 4, 5].map((i) => (
            <SkeletonBlock key={i} className='size-8 rounded-md' />
          ))}
        </div>
        <div className='hidden w-[17.5rem] shrink-0 flex-col gap-1 border-r p-3 lg:flex'>
          <SkeletonBlock className='mb-1 h-3 w-16' />
          {[1, 2, 3, 4, 5, 6].map((i) => (
            <SkeletonBlock key={i} className='h-8 rounded-md' />
          ))}
        </div>
        <main className='bg-page-surface flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden'>
          {children}
        </main>
      </div>
    </div>
  )
}

export function CenteredShellSkeleton({ children }: { children: ReactNode }) {
  return (
    <div
      role='status'
      aria-live='polite'
      className='flex min-h-svh items-center justify-center p-4'
    >
      {children}
    </div>
  )
}

export function PageHeaderSkeleton({ actions = true }: { actions?: boolean }) {
  return (
    <div className='flex items-center justify-between gap-4'>
      <div className='min-w-0'>
        <SkeletonBlock className='h-8 w-52 max-w-full' />
        <SkeletonBlock className='mt-2 h-4 w-72 max-w-full' />
      </div>
      {actions && (
        <div className='flex shrink-0 items-center gap-2'>
          <SkeletonBlock className='h-9 w-24 rounded-lg' />
          <SkeletonBlock className='h-9 w-24 rounded-lg' />
        </div>
      )}
    </div>
  )
}

export function ToolbarSkeleton({ filters = 3 }: { filters?: number }) {
  const keyPrefix = 'toolbar-filter'
  return (
    <div className='flex flex-wrap items-center gap-2'>
      <SkeletonBlock className='h-9 w-44 rounded-lg' />
      {Array.from({ length: filters }, (_, i) => (
        <SkeletonBlock
          key={`${keyPrefix}-${i}`}
          className='h-9 w-28 rounded-lg'
        />
      ))}
      <div className='ml-auto flex items-center gap-2'>
        <SkeletonBlock className='h-9 w-20 rounded-lg' />
      </div>
    </div>
  )
}

export function TableContentSkeleton({ rows = 8 }: { rows?: number }) {
  const keyPrefix = 'table-row'
  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='flex items-center gap-4 border-b px-4 py-3'>
        {[1, 2, 3, 4].map((i) => (
          <SkeletonBlock key={i} className='h-4 flex-1 rounded-sm' />
        ))}
      </div>
      <div className='divide-y'>
        {Array.from({ length: rows }, (_, i) => (
          <div
            key={`${keyPrefix}-${i}`}
            className='flex items-center gap-4 px-4 py-3.5'
          >
            <SkeletonBlock className='h-4 flex-[2] rounded-sm' />
            <SkeletonBlock className='h-4 flex-1 rounded-sm' />
            <SkeletonBlock className='h-4 flex-1 rounded-sm' />
            <SkeletonBlock className='h-4 flex-1 rounded-sm' />
          </div>
        ))}
      </div>
    </div>
  )
}

export function FormCardSkeleton({ fields = 6 }: { fields?: number }) {
  const keyPrefix = 'form-field'
  return (
    <div className='space-y-4 rounded-lg border p-4 sm:p-5'>
      {Array.from({ length: fields }, (_, i) => (
        <div key={`${keyPrefix}-${i}`} className='space-y-1.5'>
          <SkeletonBlock className='h-3.5 w-24' />
          <SkeletonBlock className='h-10 w-full max-w-md rounded-lg' />
        </div>
      ))}
      <SkeletonBlock className='h-9 w-24 rounded-lg' />
    </div>
  )
}

export function StatCardsSkeleton({ count = 4 }: { count?: number }) {
  const keyPrefix = 'stat-card'
  return (
    <div className='grid grid-cols-2 gap-3 lg:grid-cols-4'>
      {Array.from({ length: count }, (_, i) => (
        <div key={`${keyPrefix}-${i}`} className='rounded-lg border p-4'>
          <div className='flex items-center gap-2'>
            <SkeletonBlock className='size-4 rounded-sm' />
            <SkeletonBlock className='h-3.5 w-16' />
          </div>
          <SkeletonBlock className='mt-2 h-7 w-20' />
          <SkeletonBlock className='mt-1 h-3 w-24' />
        </div>
      ))}
    </div>
  )
}

export function ChartPanelSkeleton({ className }: { className?: string }) {
  return (
    <div className={cn('overflow-hidden rounded-lg border', className)}>
      <div className='flex items-center justify-between border-b px-4 py-3'>
        <SkeletonBlock className='h-5 w-32' />
        <SkeletonBlock className='h-8 w-40 rounded-lg' />
      </div>
      <div className='p-2'>
        <SkeletonBlock className='h-64 w-full sm:h-72' />
      </div>
    </div>
  )
}

export function CardsGridSkeleton({
  count = 6,
  className,
}: {
  count?: number
  className?: string
}) {
  const keyPrefix = 'card-grid'
  return (
    <div
      className={cn(
        'grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3',
        className
      )}
    >
      {Array.from({ length: count }, (_, i) => (
        <div
          key={`${keyPrefix}-${i}`}
          className='space-y-3 rounded-lg border p-4'
        >
          <div className='flex items-center justify-between'>
            <SkeletonBlock className='size-9 rounded-md' />
            <SkeletonBlock className='h-5 w-14 rounded-full' />
          </div>
          <SkeletonBlock className='h-5 w-2/3' />
          <SkeletonBlock className='h-3.5 w-full' />
          <SkeletonBlock className='h-3.5 w-5/6' />
        </div>
      ))}
    </div>
  )
}

export function ChatPageSkeleton() {
  return (
    <div className='flex min-h-0 flex-1 flex-col p-4'>
      <div className='flex-1 space-y-3 overflow-hidden'>
        <SkeletonBlock className='h-12 w-3/4 rounded-xl' />
        <SkeletonBlock className='h-12 w-1/2 rounded-xl' />
        <SkeletonBlock className='ml-auto h-12 w-2/3 rounded-xl' />
        <SkeletonBlock className='h-12 w-1/3 rounded-xl' />
      </div>
      <SkeletonBlock className='h-24 w-full rounded-xl' />
    </div>
  )
}

export function WizardSkeleton() {
  return (
    <div className='w-full max-w-lg space-y-5 rounded-lg border p-6 sm:p-8'>
      <div className='mx-auto flex items-center gap-2'>
        <SkeletonBlock className='size-10 rounded-full' />
        <SkeletonBlock className='h-6 w-32' />
      </div>
      <div className='flex gap-2'>
        {[1, 2, 3, 4].map((i) => (
          <SkeletonBlock key={i} className='h-1.5 flex-1 rounded-full' />
        ))}
      </div>
      <SkeletonBlock className='h-8 w-56' />
      <div className='space-y-3'>
        <SkeletonBlock className='h-3.5 w-24' />
        <SkeletonBlock className='h-10 w-full rounded-lg' />
        <SkeletonBlock className='h-3.5 w-24' />
        <SkeletonBlock className='h-10 w-full rounded-lg' />
      </div>
      <div className='flex justify-end gap-2 pt-2'>
        <SkeletonBlock className='h-9 w-20 rounded-lg' />
        <SkeletonBlock className='h-9 w-24 rounded-lg' />
      </div>
    </div>
  )
}

export function AuthCardSkeleton() {
  return (
    <div className='w-full max-w-sm space-y-4 rounded-lg border p-6 sm:p-8'>
      <div className='mx-auto flex flex-col items-center gap-3'>
        <SkeletonBlock className='size-10 rounded-md' />
        <SkeletonBlock className='h-6 w-36' />
      </div>
      <div className='space-y-3'>
        {[1, 2, 3].map((i) => (
          <div key={i} className='space-y-1.5'>
            <SkeletonBlock className='h-3.5 w-20' />
            <SkeletonBlock className='h-10 w-full rounded-lg' />
          </div>
        ))}
      </div>
      <SkeletonBlock className='h-10 w-full rounded-lg' />
      <SkeletonBlock className='mx-auto h-3.5 w-40' />
    </div>
  )
}

export function DetailPageSkeleton() {
  return (
    <div className='space-y-5'>
      <SkeletonBlock className='h-4 w-20' />
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div className='min-w-0 space-y-2'>
          <SkeletonBlock className='h-9 w-64 max-w-full' />
          <SkeletonBlock className='h-4 w-96 max-w-full' />
        </div>
        <SkeletonBlock className='h-10 w-28 shrink-0 rounded-lg' />
      </div>
      <div className='flex gap-2 border-b pb-px'>
        {[1, 2, 3].map((i) => (
          <SkeletonBlock key={i} className='h-9 w-24 rounded-md' />
        ))}
      </div>
      <div className='grid gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]'>
        <SkeletonBlock className='h-80 w-full rounded-xl' />
        <SkeletonBlock className='h-80 w-full rounded-xl' />
      </div>
    </div>
  )
}

export function HeroSkeleton() {
  return (
    <div className='flex flex-col items-center gap-3 py-8 text-center'>
      <SkeletonBlock className='h-5 w-36 rounded-full' />
      <SkeletonBlock className='h-10 w-[26rem] max-w-full sm:h-12' />
      <SkeletonBlock className='h-4 w-[34rem] max-w-full' />
      <div className='mt-2 flex gap-2'>
        <SkeletonBlock className='h-10 w-28 rounded-lg' />
        <SkeletonBlock className='h-10 w-28 rounded-lg' />
      </div>
    </div>
  )
}

export function ContentLinesSkeleton({ lines = 6 }: { lines?: number }) {
  const keyPrefix = 'content-line'
  return (
    <div className='space-y-3'>
      {Array.from({ length: lines }, (_, i) => {
        let width = 'w-full'
        if (i === lines - 1) {
          width = 'w-1/2'
        } else if (i % 3 === 2) {
          width = 'w-3/4'
        }
        return (
          <SkeletonBlock
            key={`${keyPrefix}-${i}`}
            className={cn('h-4', width)}
          />
        )
      })}
    </div>
  )
}

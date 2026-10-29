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
import { SkeletonBlock, SectionFallback } from '@/components/loading-state'
// Direct file import (not the components barrel): the barrel is edited by
// concurrent work on the model square; the LoadingSkeleton export and its
// viewMode prop are the stable contract, and reusing it here keeps the
// pending state and the post-mount loading state in sync with the card
// redesign without touching features/pricing.
import { LoadingSkeleton } from '@/features/pricing/components/loading-skeleton'

import {
  AuthCardSkeleton,
  AuthedShellSkeleton,
  CardsGridSkeleton,
  CenteredShellSkeleton,
  ChartPanelSkeleton,
  ChatPageSkeleton,
  ContentLinesSkeleton,
  DetailPageSkeleton,
  FormCardSkeleton,
  HeroSkeleton,
  PageHeaderSkeleton,
  PublicShellSkeleton,
  StatCardsSkeleton,
  TableContentSkeleton,
  ToolbarSkeleton,
  WizardSkeleton,
} from './archetypes'

export function HomePageSkeleton() {
  return (
    <PublicShellSkeleton>
      <div className='mx-auto w-full max-w-6xl space-y-10 px-4 py-10 sm:px-6'>
        <HeroSkeleton />
        <SkeletonBlock className='h-24 w-full rounded-xl' />
        <CardsGridSkeleton count={3} />
        <div className='flex flex-col items-center gap-3 rounded-xl border p-8 py-10'>
          <SkeletonBlock className='h-8 w-72 max-w-full' />
          <SkeletonBlock className='h-4 w-96 max-w-full' />
          <SkeletonBlock className='mt-2 h-10 w-32 rounded-lg' />
        </div>
      </div>
    </PublicShellSkeleton>
  )
}

export function PricingPageSkeleton() {
  return (
    <PublicShellSkeleton>
      <div className='mx-auto w-full max-w-[1600px] px-4 pt-20 pb-10 sm:px-6 sm:pt-24 lg:px-8'>
        <LoadingSkeleton viewMode='card' />
      </div>
    </PublicShellSkeleton>
  )
}

export function PricingModelPageSkeleton() {
  return (
    <PublicShellSkeleton>
      <div className='mx-auto w-full max-w-[1600px] px-4 pt-20 pb-10 sm:px-6 sm:pt-24 lg:px-8'>
        <DetailPageSkeleton />
      </div>
    </PublicShellSkeleton>
  )
}

export function RankingsPageSkeleton() {
  return (
    <PublicShellSkeleton>
      <div className='mx-auto w-full max-w-6xl space-y-6 px-4 py-8 sm:px-6'>
        <HeroSkeleton />
        <div className='grid gap-4 lg:grid-cols-2'>
          <ChartPanelSkeleton />
          <ChartPanelSkeleton />
        </div>
        <ChartPanelSkeleton />
      </div>
    </PublicShellSkeleton>
  )
}

export function AboutPageSkeleton() {
  return (
    <PublicShellSkeleton>
      <div className='mx-auto w-full max-w-4xl space-y-4 px-4 py-12 sm:px-6'>
        <SkeletonBlock className='h-8 w-[45%]' />
        <ContentLinesSkeleton lines={6} />
      </div>
    </PublicShellSkeleton>
  )
}

export function DashboardPageSkeleton() {
  return (
    <AuthedShellSkeleton>
      <div className='space-y-4 p-4 sm:p-6'>
        <PageHeaderSkeleton />
        <SectionFallback variant='stat-cards' />
        <div className='grid gap-4 xl:grid-cols-[minmax(0,1.45fr)_minmax(20rem,1fr)]'>
          <SectionFallback variant='chart' />
          <div className='space-y-3 rounded-lg border p-4'>
            <SkeletonBlock className='h-5 w-32' />
            <SkeletonBlock className='h-4 w-full' />
            <SkeletonBlock className='h-4 w-full' />
            <SkeletonBlock className='h-4 w-3/4' />
            <div className='flex gap-2 pt-2'>
              <SkeletonBlock className='h-6 w-24 rounded-full' />
              <SkeletonBlock className='h-6 w-24 rounded-full' />
            </div>
          </div>
        </div>
      </div>
    </AuthedShellSkeleton>
  )
}

export function AuthedTablePageSkeleton() {
  return (
    <AuthedShellSkeleton>
      <div className='space-y-4 p-4 sm:p-6'>
        <PageHeaderSkeleton />
        <ToolbarSkeleton />
        <TableContentSkeleton rows={8} />
      </div>
    </AuthedShellSkeleton>
  )
}

export function InsightsPageSkeleton() {
  return (
    <AuthedShellSkeleton>
      <div className='space-y-4 p-4 sm:p-6'>
        <PageHeaderSkeleton />
        <div className='flex gap-2'>
          {[0, 1, 2].map((i) => (
            <SkeletonBlock key={i} className='h-8 w-24 rounded-lg' />
          ))}
        </div>
        <StatCardsSkeleton count={6} />
        <TableContentSkeleton rows={6} />
      </div>
    </AuthedShellSkeleton>
  )
}

export function SettingsRouteSkeleton() {
  return (
    <AuthedShellSkeleton>
      <div className='space-y-4 p-4 sm:p-6'>
        <PageHeaderSkeleton />
        <FormCardSkeleton fields={6} />
      </div>
    </AuthedShellSkeleton>
  )
}

export function ProfilePageSkeleton() {
  return (
    <AuthedShellSkeleton>
      <div className='space-y-4 p-4 sm:p-6'>
        <div className='flex items-center gap-4 rounded-lg border p-4'>
          <SkeletonBlock className='size-16 rounded-full' />
          <div className='space-y-2'>
            <SkeletonBlock className='h-6 w-40' />
            <SkeletonBlock className='h-4 w-64' />
          </div>
        </div>
        <CardsGridSkeleton count={4} />
      </div>
    </AuthedShellSkeleton>
  )
}

export function WalletPageSkeleton() {
  return (
    <AuthedShellSkeleton>
      <div className='space-y-4 p-4 sm:p-6'>
        <PageHeaderSkeleton />
        <StatCardsSkeleton count={4} />
        <div className='grid gap-4 lg:grid-cols-2'>
          <FormCardSkeleton fields={3} />
          <CardsGridSkeleton count={3} />
        </div>
        <TableContentSkeleton rows={4} />
      </div>
    </AuthedShellSkeleton>
  )
}

export function SystemInfoPageSkeleton() {
  return (
    <AuthedShellSkeleton>
      <div className='space-y-4 p-4 sm:p-6'>
        <PageHeaderSkeleton actions={false} />
        <div className='grid gap-4 lg:grid-cols-2'>
          <TableContentSkeleton rows={4} />
          <TableContentSkeleton rows={4} />
        </div>
      </div>
    </AuthedShellSkeleton>
  )
}

export function ChatRouteSkeleton() {
  return (
    <AuthedShellSkeleton>
      <ChatPageSkeleton />
    </AuthedShellSkeleton>
  )
}

export function SetupPageSkeleton() {
  return (
    <CenteredShellSkeleton>
      <WizardSkeleton />
    </CenteredShellSkeleton>
  )
}

export function AuthPageSkeleton() {
  return (
    <CenteredShellSkeleton>
      <AuthCardSkeleton />
    </CenteredShellSkeleton>
  )
}

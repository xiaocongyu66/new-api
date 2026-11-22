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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Main } from '@/components/layout'
import { PageFooterProvider } from '@/components/layout/components/page-footer'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SettingsPageProvider } from '@/features/system-settings/components/settings-page-context'

import { ManualUpstreamPrices } from './manual-upstream-prices'
import { UpstreamRatioSync } from './upstream-ratio-sync'

export type UpstreamPriceTab = 'auto' | 'manual'

/**
 * Upstream price page. Two tabs share the same subject (what an upstream
 * charges us): `auto` fetches /api/pricing and compares it with local pricing,
 * `manual` holds operator-entered prices for upstreams that expose no pricing
 * endpoint at all.
 */
export function UpstreamPrices() {
  const { t } = useTranslation()
  const [tab, setTab] = useState<UpstreamPriceTab>('auto')
  // Action buttons of the active tab portal into the title row, matching the
  // channels page so the sync controls stay visible without scrolling.
  const [actionsContainer, setActionsContainer] =
    useState<HTMLDivElement | null>(null)

  return (
    <SettingsPageProvider actionsContainer={actionsContainer} suppressSectionHeader>
      <PageFooterProvider container={null}>
        <Main>
          <Tabs
            value={tab}
            onValueChange={(value) => setTab(value as UpstreamPriceTab)}
            className='flex h-full min-h-0 flex-col'
          >
            <header className='shrink-0 space-y-2 px-3 pt-3 pb-3 sm:px-4 sm:pt-5'>
              <div className='flex flex-wrap items-center gap-3'>
                <h1 className='shrink-0 text-xl font-semibold tracking-tight sm:text-2xl'>
                  {t('Upstream prices')}
                </h1>
                <div
                  ref={setActionsContainer}
                  className='ml-auto flex shrink-0 flex-wrap items-center justify-end gap-2'
                />
              </div>
              <div className='no-scrollbar w-full max-w-full overflow-x-auto overflow-y-hidden sm:overflow-visible'>
                <TabsList className='flex w-max flex-nowrap sm:grid sm:w-fit sm:grid-cols-2'>
                  <TabsTrigger
                    value='auto'
                    className='flex-none px-4 text-sm max-sm:px-3'
                  >
                    {t('Profit comparison')}
                  </TabsTrigger>
                  <TabsTrigger
                    value='manual'
                    className='flex-none px-4 text-sm max-sm:px-3'
                  >
                    {t('Manual prices')}
                  </TabsTrigger>
                </TabsList>
              </div>
            </header>

            <div className='flex min-h-0 flex-1 flex-col overflow-hidden px-3 pt-1 pb-3 sm:px-4 sm:pt-1.5 sm:pb-4'>
              <TabsContent
                value='auto'
                keepMounted
                className='m-0 flex min-h-0 flex-1 flex-col overflow-y-auto overflow-x-hidden data-hidden:hidden'
              >
                <UpstreamRatioSync />
              </TabsContent>
              <TabsContent
                value='manual'
                keepMounted
                className='m-0 flex min-h-0 flex-1 flex-col overflow-hidden data-hidden:hidden'
              >
                <ManualUpstreamPrices />
              </TabsContent>
            </div>
          </Tabs>
        </Main>
      </PageFooterProvider>
    </SettingsPageProvider>
  )
}

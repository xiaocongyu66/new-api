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
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Main } from '@/components/layout'
import { PageFooterProvider } from '@/components/layout/components/page-footer'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ModelPricingSection } from '@/features/system-settings/models/model-pricing-section'
import type { RatioTabId } from '@/features/system-settings/models/ratio-settings-card'
import {
  hasPermission,
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
} from '@/lib/admin-permissions'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import { ChannelCreateTab } from './components/channel-create-tab'
import { ChannelListTab } from './components/channel-list-tab'
import { ChannelsDialogs } from './components/channels-dialogs'
import { ChannelsPrimaryButtons } from './components/channels-primary-buttons'
import {
  ChannelsProvider,
  useChannels,
  type ChannelsPageTab,
} from './components/channels-provider'

// 定价的三个视图在页签行里各占一格，cardTab 决定 RatioSettingsCard 只渲染
// 哪一块内容（单页签时它自带的分页器会自动消失）。
const PRICING_TABS: {
  id: ChannelsPageTab
  labelKey: string
  cardTab: RatioTabId
}[] = [
  { id: 'pricing-models', labelKey: 'Pricing', cardTab: 'models' },
  { id: 'pricing-tools', labelKey: 'Tool prices', cardTab: 'tool-prices' },
  {
    id: 'pricing-upstream',
    labelKey: 'Upstream sync',
    cardTab: 'upstream-sync',
  },
]

function ChannelsContent() {
  const { t } = useTranslation()
  const { pageTab, setPageTab, setCurrentRow } = useChannels()
  const currentUser = useAuthStore((state) => state.auth.user)
  const canCreateChannel = hasPermission(
    currentUser,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.SENSITIVE_WRITE
  )
  const canEditModelPricing = hasPermission(
    currentUser,
    ADMIN_PERMISSION_RESOURCES.SYSTEM,
    ADMIN_PERMISSION_ACTIONS.SETTINGS
  )
  const [footerContainer, setFooterContainer] = useState<HTMLDivElement | null>(
    null
  )
  const [createActionsContainer, setCreateActionsContainer] =
    useState<HTMLDivElement | null>(null)
  const createScrollContainerRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    if (pageTab !== 'create') return

    const frameId = requestAnimationFrame(() => {
      createScrollContainerRef.current?.focus({ preventScroll: true })
    })

    return () => cancelAnimationFrame(frameId)
  }, [pageTab])

  return (
    <>
      <PageFooterProvider container={footerContainer}>
        <Main>
          <Tabs
            value={pageTab}
            onValueChange={(value) => {
              const next = value as ChannelsPageTab
              if (next === 'create') setCurrentRow(null)
              setPageTab(next)
            }}
            className='flex h-full min-h-0 flex-col'
          >
            <header className='shrink-0 space-y-3 px-3 pt-3 pb-3 sm:px-4 sm:pt-5'>
              {/* 第一行：标题 + 页签。页签跟着标题走，窄屏整行 wrap */}
              <div className='flex flex-wrap items-center gap-x-3 gap-y-2 sm:gap-x-4'>
                <h1 className='min-w-0 truncate text-xl font-semibold tracking-tight sm:text-2xl'>
                  {t('Channel Management')}
                </h1>
                {/*
                  窄屏放不下五个页签，横向滚动而不是换行：TabsList 的行高被
                  group-data-horizontal/tabs:h-8 锁死，换行后第二行会溢出叠字。
                  滚动条放在外层，避免裁掉触发器的 after 伪元素。
                */}
                <div className='w-full max-w-full overflow-x-auto overflow-y-hidden sm:w-fit sm:overflow-visible'>
                  <TabsList
                    className={cn(
                      'flex w-max flex-nowrap',
                      canEditModelPricing
                        ? 'sm:grid sm:w-fit sm:grid-cols-5'
                        : 'sm:grid sm:w-fit sm:grid-cols-2'
                    )}
                  >
                    <TabsTrigger
                      value='create'
                      disabled={!canCreateChannel}
                      className='flex-none px-4 text-sm'
                    >
                      <span className='sm:hidden'>{t('Create')}</span>
                      <span className='hidden sm:inline'>
                        {t('Create Channel')}
                      </span>
                    </TabsTrigger>
                    <TabsTrigger
                      value='channels'
                      className='flex-none px-4 text-sm'
                    >
                      {t('Channels')}
                    </TabsTrigger>
                    {canEditModelPricing
                      ? PRICING_TABS.map((tab) => (
                          <TabsTrigger
                            key={tab.id}
                            value={tab.id}
                            className='flex-none px-4 text-sm'
                          >
                            {t(tab.labelKey)}
                          </TabsTrigger>
                        ))
                      : null}
                  </TabsList>
                </div>
              </div>
              {/* 第二行：列表工具栏与创建表单的操作按钮 */}
              <div className='flex flex-wrap items-center justify-between gap-x-3 gap-y-2'>
                <div
                  className={
                    pageTab === 'channels'
                      ? 'flex shrink-0 flex-wrap items-center justify-end gap-2 sm:gap-x-4'
                      : 'hidden'
                  }
                >
                  <ChannelsPrimaryButtons />
                </div>
                <div
                  ref={setCreateActionsContainer}
                  className={
                    pageTab === 'create'
                      ? 'flex w-full flex-wrap items-center justify-center gap-2 sm:w-auto sm:justify-end'
                      : 'hidden'
                  }
                />
              </div>
            </header>

            <div
              ref={createScrollContainerRef}
              className={
                pageTab === 'create'
                  ? 'flex min-h-0 flex-1 flex-col overflow-y-auto overflow-x-hidden px-3 pt-1 pb-4 sm:px-4 sm:pt-1.5 md:pb-4'
                  : 'flex min-h-0 flex-1 flex-col overflow-hidden px-3 pt-1 pb-3 sm:px-4 sm:pt-1.5 sm:pb-4'
              }
              tabIndex={pageTab === 'create' ? -1 : undefined}
            >
              <TabsContent
                value='create'
                keepMounted
                className='m-0 flex min-h-full flex-col data-hidden:hidden'
              >
                <ChannelCreateTab
                  actionsContainer={createActionsContainer}
                  active={pageTab === 'create'}
                />
              </TabsContent>

              <TabsContent
                value='channels'
                keepMounted
                className='m-0 flex h-full min-h-0 flex-1 flex-col overflow-hidden data-hidden:hidden'
              >
                <ChannelListTab />
              </TabsContent>

              {canEditModelPricing
                ? PRICING_TABS.map((tab) => (
                    <TabsContent
                      key={tab.id}
                      value={tab.id}
                      className='m-0 flex min-h-0 flex-1 flex-col overflow-hidden data-hidden:hidden'
                    >
                      <div className='flex min-h-0 flex-1 flex-col overflow-y-auto overflow-x-hidden px-1 pt-1 pb-4 sm:px-1.5 sm:pt-1.5'>
                        <ModelPricingSection tabs={[tab.cardTab]} />
                      </div>
                    </TabsContent>
                  ))
                : null}
            </div>

            <div
              ref={setFooterContainer}
              className='bg-header-surface shrink-0 border-t px-3 py-2.5 empty:hidden sm:px-4 md:py-3'
              hidden={pageTab !== 'channels'}
            />
          </Tabs>
        </Main>
      </PageFooterProvider>

      <ChannelsDialogs />
    </>
  )
}

export function Channels() {
  return (
    <ChannelsProvider>
      <ChannelsContent />
    </ChannelsProvider>
  )
}

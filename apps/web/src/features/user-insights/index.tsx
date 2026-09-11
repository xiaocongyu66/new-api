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

import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useMediaQuery } from '@/hooks'

import { SettingsPageProvider } from '../system-settings/components/settings-page-context'
import {
  getOptionValue,
  useSystemOptions,
} from '../system-settings/hooks/use-system-options'
import {
  DEFAULT_INSIGHT_VALUES,
  parseBlockedClients,
} from '../system-settings/operations/user-insight-defaults'
import { UserInsightSection } from '../system-settings/operations/user-insight-section'
import { InsightSummaryCards } from './components/insight-summary-cards'
import { InsightsTable } from './components/insights-table'
import { SampleDetailSheet } from './components/sample-detail-sheet'
import { SamplesPanel } from './components/samples-panel'
import { UserEvidenceSheet } from './components/user-evidence-sheet'
import { useUserInsightSummary } from './hooks/use-user-insights'
import type { UserInsight } from './types'

export function UserInsights() {
  const { t } = useTranslation()
  // 证据抽屉的目标用户：从画像行的证据按钮打开。
  const [evidenceUser, setEvidenceUser] = useState<UserInsight | null>(null)
  // 请求体原文抽屉叠在证据抽屉之上，只在管理员明确点开时才拉原文。
  const [rawBodySampleId, setRawBodySampleId] = useState<number | null>(null)
  // 移动端放弃"固定视口 + 表格内部滚动"：统计卡片 + 工具栏会挤掉卡片列表，
  // 改成本 Tab 自身滚动（与证据样本/画像配置 Tab 一致），桌面端保持固定表头。
  const isMobile = useMediaQuery('(max-width: 640px)')

  const summaryQuery = useUserInsightSummary()

  // 保存按钮的 portal 容器：画像配置 Tab 的表单按钮通过 SettingsPageProvider
  // portal 到这里，落在 Tab 行最右（与 TabsList 同行）。
  const [actionsContainer, setActionsContainer] =
    useState<HTMLDivElement | null>(null)

  // 表单初始值用服务端实际配置，查询返回前回落到内置默认值。
  const systemOptionsQuery = useSystemOptions()
  const insightDefaults = useMemo(
    () => getOptionValue(systemOptionsQuery.data?.data, DEFAULT_INSIGHT_VALUES),
    [systemOptionsQuery.data]
  )
  // 全站封禁的客户端列表，用于在证据抽屉里标记/操作每个客户端。
  const blockedClients = useMemo(
    () => parseBlockedClients(insightDefaults['user_insight_setting.blocked_clients']),
    [insightDefaults]
  )

  return (
    <SettingsPageProvider
      actionsContainer={actionsContainer}
      suppressSectionHeader={false}
    >
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>{t('User Insights')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          {/* 画像聚合与逐请求证据分开两页：前者看长期倾向，后者做人工复核。
              桌面端用 fixedContent + DataTablePage 的固定表头（与 users 页一致）；
              移动端（isMobile）画像 Tab 改为整体滚动，避免统计卡片挤占列表高度。 */}
          <Tabs
            defaultValue='profiles'
            className='flex h-full min-h-0 flex-col'
          >
            <div className='flex shrink-0 items-center justify-between gap-3'>
              <TabsList>
                <TabsTrigger value='profiles'>{t('Profiles')}</TabsTrigger>
                <TabsTrigger value='samples'>
                  {t('Evidence samples')}
                </TabsTrigger>
                <TabsTrigger value='settings'>
                  {t('Profiling settings')}
                </TabsTrigger>
              </TabsList>
              {/* 空容器：仅当画像配置 Tab 激活时，表单的保存按钮才 portal 进来 */}
              <div
                ref={setActionsContainer}
                className='flex flex-wrap items-center justify-end gap-2'
              />
            </div>

            <TabsContent
              value='profiles'
              className={
                isMobile
                  ? 'min-h-0 flex-1 space-y-3 overflow-y-auto pt-3'
                  : 'flex min-h-0 flex-1 flex-col gap-3 pt-3'
              }
            >
              <div className='shrink-0'>
                <InsightSummaryCards
                  summary={summaryQuery.data?.data}
                  isLoading={summaryQuery.isLoading}
                />
              </div>
              <div className='min-h-0 flex-1'>
                <InsightsTable
                  onViewEvidence={setEvidenceUser}
                  blockedClients={blockedClients}
                />
              </div>
            </TabsContent>

            <TabsContent
              value='samples'
              className='min-h-0 flex-1 overflow-auto pt-3'
            >
              <SamplesPanel />
            </TabsContent>
            <TabsContent
              value='settings'
              className='min-h-0 flex-1 overflow-auto pt-3'
            >
              <UserInsightSection defaultValues={insightDefaults} />
            </TabsContent>
          </Tabs>

          <UserEvidenceSheet
            userId={evidenceUser?.user_id ?? null}
            username={evidenceUser?.display_name || evidenceUser?.username}
            item={evidenceUser}
            blockedClients={blockedClients}
            onClose={() => setEvidenceUser(null)}
            onOpenRawBody={setRawBodySampleId}
          />
          <SampleDetailSheet
            sampleId={rawBodySampleId}
            onClose={() => setRawBodySampleId(null)}
          />
        </SectionPageLayout.Content>
      </SectionPageLayout>
    </SettingsPageProvider>
  )
}

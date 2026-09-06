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

import { SectionPageLayout } from '@/components/layout'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

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

  const summaryQuery = useUserInsightSummary()

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t('User Insights')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        {/* 画像聚合与逐请求证据分开两页：前者看长期倾向，后者做人工复核。
            列表页用 fixedContent + DataTablePage 的固定表头，与 users 页一致。 */}
        <Tabs defaultValue='profiles' className='flex h-full min-h-0 flex-col'>
          <TabsList className='shrink-0'>
            <TabsTrigger value='profiles'>{t('Profiles')}</TabsTrigger>
            <TabsTrigger value='samples'>{t('Evidence samples')}</TabsTrigger>
          </TabsList>

          <TabsContent
            value='profiles'
            className='flex min-h-0 flex-1 flex-col gap-3 pt-3'
          >
            <div className='shrink-0'>
              <InsightSummaryCards
                summary={summaryQuery.data?.data}
                isLoading={summaryQuery.isLoading}
              />
            </div>
            <div className='min-h-0 flex-1'>
              <InsightsTable onViewEvidence={setEvidenceUser} />
            </div>
          </TabsContent>

          <TabsContent value='samples' className='min-h-0 flex-1 overflow-auto pt-3'>
            <SamplesPanel />
          </TabsContent>
        </Tabs>

        <UserEvidenceSheet
          userId={evidenceUser?.user_id ?? null}
          username={evidenceUser?.display_name || evidenceUser?.username}
          onClose={() => setEvidenceUser(null)}
          onOpenRawBody={setRawBodySampleId}
        />
        <SampleDetailSheet
          sampleId={rawBodySampleId}
          onClose={() => setRawBodySampleId(null)}
        />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
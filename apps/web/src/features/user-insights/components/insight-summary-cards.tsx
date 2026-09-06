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

import { useTranslation } from 'react-i18next'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import type { UserInsightSummary } from '../types'

type InsightSummaryCardsProps = {
  summary?: UserInsightSummary
  isLoading: boolean
}

/** 看板顶部的统计卡片：用途分布、技术方向、性别倾向、风险与中转站来源。 */
export function InsightSummaryCards({
  summary,
  isLoading,
}: InsightSummaryCardsProps) {
  const { t } = useTranslation()

  if (isLoading) {
    return (
      <div className='grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-6'>
        {Array.from({ length: 6 }).map((_, index) => (
          <Skeleton key={index} className='h-24 w-full rounded-xl' />
        ))}
      </div>
    )
  }

  if (!summary) return null

  const cards = [
    { label: t('Profiled users'), value: summary.total_users },
    { label: t('Coders'), value: summary.coders },
    { label: t('Roleplayers'), value: summary.roleplayers },
    {
      label: t('Frontend / Backend'),
      value: `${summary.frontend_leaning} / ${summary.backend_leaning}`,
    },
    {
      label: t('Male / Female leaning'),
      value: `${summary.male_leaning} / ${summary.female_leaning}`,
    },
    {
      label: t('Relay sources'),
      value: summary.relay_users,
    },
  ]

  return (
    <div className='grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-6'>
      {cards.map((card) => (
        <Card key={card.label}>
          <CardContent className='p-4'>
            <p className='text-muted-foreground text-xs'>{card.label}</p>
            <p className='mt-1 text-2xl font-semibold tabular-nums'>
              {card.value}
            </p>
          </CardContent>
        </Card>
      ))}
      {summary.risky_users > 0 && (
        <Card className='border-destructive/40 col-span-2 md:col-span-3 lg:col-span-6'>
          <CardContent className='flex items-center justify-between p-4'>
            <div>
              <p className='text-muted-foreground text-xs'>
                {t('Users with jailbreak signals')}
              </p>
              <p className='text-destructive mt-1 text-2xl font-semibold tabular-nums'>
                {summary.risky_users}
              </p>
            </div>
            <p className='text-muted-foreground max-w-md text-xs'>
              {t(
                'Detection is heuristic. Review the matched techniques before taking action.'
              )}
            </p>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
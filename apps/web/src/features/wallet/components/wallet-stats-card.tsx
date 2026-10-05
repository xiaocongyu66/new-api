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
import { Activity, BarChart3, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Card, CardContent } from '@/components/ui/card'
import { IconBadge, type IconBadgeTone } from '@/components/ui/icon-badge'
import { Skeleton } from '@/components/ui/skeleton'
import { formatQuota } from '@/lib/format'

import type { UserWalletData } from '../types'

interface WalletStatsCardProps {
  user: UserWalletData | null
  loading?: boolean
}
const STATS_GRID_CLASS =
  'grid min-w-0 grid-cols-2 gap-3 sm:grid-cols-3 sm:gap-4'

export function WalletStatsCard(props: WalletStatsCardProps) {
  const { t } = useTranslation()
  if (props.loading) {
    return (
      <div className={STATS_GRID_CLASS}>
        {['balance', 'usage', 'requests'].map((key) => (
          <Card key={key} className='min-w-0 gap-0 py-0'>
            <CardContent className='p-3 sm:p-5'>
              <Skeleton className='h-3.5 w-full' />
              <Skeleton className='mt-2 h-6 w-full sm:h-7' />
            </CardContent>
          </Card>
        ))}
      </div>
    )
  }

  const stats: {
    label: string
    value: string
    description: string
    icon: typeof WalletCards
    tone: IconBadgeTone
  }[] = [
    {
      label: t('Current Balance'),
      value: formatQuota(props.user?.quota_display ?? 0),
      description: t('Remaining quota'),
      icon: WalletCards,
      tone: 'success',
    },
    {
      label: t('Total Usage'),
      value: formatQuota(props.user?.used_quota_display ?? 0),
      description: t('Total consumed quota'),
      icon: BarChart3,
      tone: 'info',
    },
    {
      label: t('API Requests'),
      value: (props.user?.request_count ?? 0).toLocaleString(),
      description: t('Total requests made'),
      icon: Activity,
      tone: 'chart-4',
    },
  ]

  return (
    <div className={STATS_GRID_CLASS}>
      {stats.map((item) => (
        <Card key={item.label} className='min-w-0 gap-0 py-0'>
          <CardContent className='p-3 sm:p-5'>
            <div className='flex items-center gap-2'>
              <IconBadge tone={item.tone} size='stat'>
                <item.icon />
              </IconBadge>
              <div className='text-muted-foreground min-w-0 text-xs font-medium'>
                {item.label}
              </div>
            </div>

            <div className='text-foreground mt-3 font-mono text-lg font-semibold tracking-tight break-all tabular-nums sm:text-2xl'>
              {item.value}
            </div>
            <div className='text-muted-foreground mt-1 hidden text-xs md:block'>
              {item.description}
            </div>
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

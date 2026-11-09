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

import { Progress } from '@/components/ui/progress'
import { cn } from '@/lib/utils'

import type { SubscriptionRequestCounter } from '../types'

interface RequestCountBarsProps {
  counters?: SubscriptionRequestCounter[]
  className?: string
}

/**
 * Renders one two-row entry per request counter: a coloured progress bar on the
 * first row and the used/total numbers on the second. Shared by the wallet card
 * and the admin user-subscription dialog.
 */
export function RequestCountBars({
  counters,
  className,
}: RequestCountBarsProps) {
  const { t } = useTranslation()
  const list = counters || []
  if (list.length === 0) {
    return (
      <p className={cn('text-muted-foreground text-xs', className)}>
        {t('No request counters')}
      </p>
    )
  }
  return (
    <div className={cn('space-y-2', className)}>
      {list.map((counter) => {
        const total = Number(counter.total || 0)
        const used = Number(counter.used || 0)
        const percent =
          total > 0 ? Math.min(100, Math.round((used / total) * 100)) : 0
        const exhausted = total > 0 && used >= total
        return (
          <div key={counter.id} className='space-y-1'>
            <Progress value={percent} className='h-1.5' />
            <div className='flex items-center justify-between gap-2 text-xs'>
              <span className='text-muted-foreground truncate'>
                {counter.group_name} · {counter.model_name}
              </span>
              <span
                className={cn(
                  'shrink-0 tabular-nums',
                  exhausted ? 'text-red-500' : 'text-muted-foreground'
                )}
              >
                {used}/{total}
              </span>
            </div>
          </div>
        )
      })}
    </div>
  )
}

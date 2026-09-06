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
import { formatRatio } from '../lib/labels'

type UsageMixBarProps = {
  code: number
  roleplay: number
  qa: number
  other: number
}

/**
 * 用途占比条：把写代码 / 角色扮演 / 问答 / 其他四类的比例画成一条堆叠条。
 * 相比数字表格，堆叠条能让管理员一眼看出用户的主要用途。
 */
export function UsageMixBar({ code, roleplay, qa, other }: UsageMixBarProps) {
  const { t } = useTranslation()

  const segments = [
    {
      key: 'code',
      ratio: code,
      className: 'bg-sky-500',
      label: t('Coding'),
    },
    {
      key: 'roleplay',
      ratio: roleplay,
      className: 'bg-pink-500',
      label: t('Roleplay'),
    },
    {
      key: 'qa',
      ratio: qa,
      className: 'bg-emerald-500',
      label: t('Q&A'),
    },
    {
      key: 'other',
      ratio: other,
      className: 'bg-muted-foreground/40',
      label: t('Other'),
    },
  ].filter((segment) => segment.ratio > 0)

  if (segments.length === 0) {
    return <span className='text-muted-foreground text-xs'>{t('No data')}</span>
  }

  const description = segments
    .map((segment) => `${segment.label} ${formatRatio(segment.ratio)}`)
    .join(', ')

  return (
    <div className='min-w-[140px] space-y-1'>
      <div
        className='bg-muted flex h-2 w-full overflow-hidden rounded-full'
        role='img'
        aria-label={description}
      >
        {segments.map((segment) => (
          <div
            key={segment.key}
            className={segment.className}
            style={{ width: `${Math.max(segment.ratio * 100, 2)}%` }}
          />
        ))}
      </div>
      <p className='text-muted-foreground text-[11px] leading-tight'>
        {description}
      </p>
    </div>
  )
}
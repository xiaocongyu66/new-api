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
import type { ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'

import { GroupBadge } from '@/components/group-badge'
import { LongText } from '@/components/long-text'
import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import {
  categoryLabel,
  clientLabel,
  formatRatio,
  formatTimestamp,
  genderBasisLabel,
  genderLabel,
  jailbreakTagLabel,
  riskLabel,
  stackLabel,
} from '../lib/labels'
import type { InsightRisk, UserInsight } from '../types'
import { InsightRowActions } from './insight-row-actions'
import { UsageMixBar } from './usage-mix-bar'

/** new-api 中 status=2 表示已封禁。 */
const USER_STATUS_DISABLED = 2

/**
 * 风险等级到 StatusBadge 配色的映射。
 *
 * 与旧实现的 riskBadgeVariant 不同：那个返回的是 shadcn Badge 的
 * variant，而全站表格统一用 StatusBadge，两者的取值域不一样。
 */
function riskTone(
  risk: InsightRisk
): 'danger' | 'warning' | 'neutral' | 'success' {
  switch (risk) {
    case 'confirmed':
      return 'danger'
    case 'likely':
      return 'warning'
    case 'suspect':
      return 'neutral'
    default:
      return 'success'
  }
}

type UseInsightColumnsOptions = {
  onViewEvidence: (item: UserInsight) => void
}

export function useInsightColumns(
  options: UseInsightColumnsOptions
): ColumnDef<UserInsight>[] {
  const { t } = useTranslation()

  return [
    {
      accessorKey: 'user_id',
      header: t('ID'),
      cell: ({ row }) => (
        <TableId value={row.original.user_id} className='w-[60px] text-sm' />
      ),
      size: 80,
      enableSorting: false,
      meta: { mobileOrder: 10 },
    },
    {
      accessorKey: 'username',
      header: t('User'),
      cell: ({ row }) => {
        const item = row.original
        const name = item.display_name || item.username
        return (
          <div className='flex min-w-[140px] flex-col gap-1'>
            <LongText className='max-w-[160px] font-medium'>{name}</LongText>
            {item.group && (
              <div className='w-fit'>
                <GroupBadge group={item.group} />
              </div>
            )}
          </div>
        )
      },
      enableHiding: false,
      enableSorting: false,
      size: 200,
      meta: { mobileTitle: true },
    },
    {
      id: 'status',
      accessorKey: 'status',
      header: t('Status'),
      cell: ({ row }) => {
        const banned = row.original.status === USER_STATUS_DISABLED
        return (
          <StatusBadge
            label={banned ? t('Banned') : t('Enabled')}
            variant={banned ? 'danger' : 'success'}
            copyable={false}
          />
        )
      },
      enableSorting: false,
      size: 110,
      meta: { mobileBadge: true },
    },
    {
      id: 'usage_mix',
      header: t('Usage mix'),
      cell: ({ row }) => {
        const ratios = row.original.ratios
        return (
          <UsageMixBar
            code={ratios.code_ratio}
            roleplay={ratios.roleplay_ratio}
            qa={ratios.qa_ratio}
            other={ratios.other_ratio + ratios.translate_ratio}
          />
        )
      },
      enableSorting: false,
      size: 200,
      meta: { mobileOrder: 20 },
    },
    {
      accessorKey: 'primary_category',
      header: t('Primary use'),
      cell: ({ row }) => {
        const item = row.original
        return (
          <div className='flex flex-col gap-1'>
            <StatusBadge
              label={categoryLabel(item.primary_category, t)}
              variant='info'
              copyable={false}
            />
            <span className='text-muted-foreground text-xs tabular-nums'>
              {t('{{count}} requests', { count: item.total_requests })}
            </span>
          </div>
        )
      },
      // 用途类别用 faceted filter 单选，值与后端常量一致。
      filterFn: (row, id, value) => value.includes(String(row.getValue(id))),
      enableSorting: false,
      size: 150,
      meta: { mobileOrder: 30 },
    },
    {
      accessorKey: 'primary_stack',
      header: t('Stack'),
      cell: ({ row }) => {
        const item = row.original
        if (item.code_requests <= 0) {
          return <span className='text-muted-foreground text-xs'>—</span>
        }
        const languages = Object.keys(item.languages ?? {})
        return (
          <div className='flex flex-col gap-1'>
            <span className='text-sm'>{stackLabel(item.primary_stack, t)}</span>
            <span className='text-muted-foreground text-xs'>
              {t('FE')} {formatRatio(item.ratios.frontend_ratio)} ·{' '}
              {t('BE')} {formatRatio(item.ratios.backend_ratio)}
            </span>
            {languages.length > 0 && (
              <LongText className='text-muted-foreground max-w-[160px] text-[11px]'>
                {languages.slice(0, 4).join(', ')}
              </LongText>
            )}
          </div>
        )
      },
      enableSorting: false,
      size: 160,
      meta: { mobileOrder: 40 },
    },
    {
      accessorKey: 'guessed_gender',
      header: t('Gender leaning'),
      cell: ({ row }) => {
        const item = row.original
        if (item.guessed_gender === 'unknown') {
          return <span className='text-muted-foreground text-xs'>—</span>
        }
        return (
          <div className='flex flex-col gap-0.5'>
            <span className='text-sm'>
              {genderLabel(item.guessed_gender, t)}
            </span>
            <span className='text-muted-foreground text-xs'>
              {t('confidence')} {formatRatio(item.gender_confidence)}
            </span>
            {item.gender_basis && (
              // 依据强度必须展示：inverse 的置信度上限只有 0.45，
              // 与自述不是同一个量级的证据。
              <span className='text-muted-foreground text-[11px]'>
                {genderBasisLabel(item.gender_basis, t)}
              </span>
            )}
          </div>
        )
      },
      enableSorting: false,
      size: 150,
      meta: { mobileHidden: true },
    },
    {
      id: 'clients',
      header: t('Clients'),
      cell: ({ row }) => {
        const item = row.original
        const clients = item.clients ?? []
        if (clients.length === 0 && item.relay_requests === 0) {
          return <span className='text-muted-foreground text-xs'>—</span>
        }
        return (
          <div className='flex max-w-[200px] flex-wrap items-center gap-1'>
            {clients.slice(0, 3).map((client) => (
              <StatusBadge
                key={client.client}
                label={`${clientLabel(client.client)}${client.version ? ` ${client.version}` : ''}`}
                variant='neutral'
                copyable={false}
              />
            ))}
            {item.relay_requests > 0 && (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <StatusBadge
                      label={t('via relay')}
                      variant='warning'
                      copyable={false}
                      className='cursor-help'
                    />
                  }
                />
                <TooltipContent>
                  <p className='text-xs'>
                    {Object.keys(item.relay_vendors ?? {}).join(', ') ||
                      t('Relay traffic only')}
                  </p>
                </TooltipContent>
              </Tooltip>
            )}
          </div>
        )
      },
      enableSorting: false,
      size: 220,
      meta: { mobileHidden: true },
    },
    {
      accessorKey: 'risk_level',
      header: t('Risk'),
      cell: ({ row }) => {
        const item = row.original
        const tags = Object.keys(item.jailbreak_tags ?? {})
        return (
          <div className='flex flex-col gap-1'>
            <StatusBadge
              label={`${riskLabel(item.risk_level, t)}${item.jailbreak_max_score > 0 ? ` (${item.jailbreak_max_score})` : ''}`}
              variant={riskTone(item.risk_level)}
              copyable={false}
            />
            {tags.length > 0 && (
              <LongText className='text-muted-foreground max-w-[180px] text-[11px]'>
                {tags
                  .slice(0, 2)
                  .map((tag) => jailbreakTagLabel(tag, t))
                  .join(', ')}
              </LongText>
            )}
          </div>
        )
      },
      filterFn: (row, id, value) => value.includes(String(row.getValue(id))),
      enableSorting: false,
      size: 170,
      meta: { mobileOrder: 50 },
    },
    {
      accessorKey: 'last_seen_at',
      header: t('Last active'),
      cell: ({ row }) => (
        <span className='text-muted-foreground text-sm whitespace-nowrap'>
          {formatTimestamp(row.original.last_seen_at, t)}
        </span>
      ),
      enableSorting: false,
      size: 170,
      meta: { mobileOrder: 60 },
    },
    {
      id: 'actions',
      header: () => t('Actions'),
      cell: ({ row }) => (
        <InsightRowActions
          row={row}
          onViewEvidence={options.onViewEvidence}
        />
      ),
      meta: { pinned: 'right' as const },
    },
  ]
}
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
import type { Row } from '@tanstack/react-table'
import { Eraser, FileSearch, Power, PowerOff, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableRowActionMenu } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import {
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
} from '@/components/ui/dropdown-menu'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import {
  useDeleteUserSamples,
  usePurgeUserInsight,
  useToggleUserBan,
} from '../hooks/use-user-insights'
import { categoryLabel, jailbreakTagLabel, riskLabel } from '../lib/labels'
import type { UserInsight } from '../types'

/** new-api 中 status=2 表示已封禁。 */
const USER_STATUS_DISABLED = 2

type InsightRowActionsProps = {
  row: Row<UserInsight>
  /**
   * 打开该用户的证据抽屉。
   *
   * 证据入口做成独立按钮而不是塞进菜单：管理员的动线是
   * "看到可疑用户 → 查证据 → 决定处置"，查证据是最高频的一步。
   */
  onViewEvidence: (item: UserInsight) => void
}

export function InsightRowActions({
  row,
  onViewEvidence,
}: InsightRowActionsProps) {
  const { t } = useTranslation()
  const item = row.original
  const banned = item.status === USER_STATUS_DISABLED

  const toggleBan = useToggleUserBan()
  const purgeInsight = usePurgeUserInsight()
  const purgeSamples = useDeleteUserSamples()

  const [banOpen, setBanOpen] = useState(false)
  const [purgeOpen, setPurgeOpen] = useState(false)
  const [purgeSamplesOpen, setPurgeSamplesOpen] = useState(false)

  return (
    <div className='-ml-1.5 flex items-center gap-1'>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant='ghost'
              size='icon-sm'
              onClick={() => onViewEvidence(item)}
              aria-label={t('View evidence')}
            />
          }
        >
          <FileSearch />
        </TooltipTrigger>
        <TooltipContent>{t('View evidence')}</TooltipContent>
      </Tooltip>

      <DataTableRowActionMenu
        ariaLabel={t('Open menu')}
        contentClassName='w-52'
      >
        {banned ? (
          // 解封是低风险且可逆的，直接执行；封禁走二次确认。
          <DropdownMenuItem
            onClick={() => toggleBan.mutate({ userId: item.user_id, banned: false })}
          >
            {t('Unban')}
            <DropdownMenuShortcut>
              <Power size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>
        ) : (
          <DropdownMenuItem
            onSelect={(event) => {
              event.preventDefault()
              setBanOpen(true)
            }}
          >
            {t('Ban')}
            <DropdownMenuShortcut>
              <PowerOff size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>
        )}

        <DropdownMenuSeparator />

        <DropdownMenuItem
          onSelect={(event) => {
            event.preventDefault()
            setPurgeSamplesOpen(true)
          }}
        >
          {t('Purge evidence samples')}
          <DropdownMenuShortcut>
            <Eraser size={16} />
          </DropdownMenuShortcut>
        </DropdownMenuItem>

        <DropdownMenuItem
          onSelect={(event) => {
            event.preventDefault()
            setPurgeOpen(true)
          }}
          className='text-destructive focus:text-destructive'
        >
          {t('Clear profile')}
          <DropdownMenuShortcut>
            <Trash2 size={16} />
          </DropdownMenuShortcut>
        </DropdownMenuItem>
      </DataTableRowActionMenu>

      <ConfirmDialog
        open={banOpen}
        onOpenChange={setBanOpen}
        destructive
        title={t('Ban this user?')}
        desc={
          <div className='space-y-2'>
            <p>
              {t(
                'The user will immediately lose API access. Profiling is heuristic and can be wrong, so please confirm the matched signals first. This action is reversible.'
              )}
            </p>
            <div className='bg-muted/50 space-y-1 rounded-md p-3 text-sm'>
              <p>
                <span className='text-muted-foreground'>{t('User')}: </span>
                {item.display_name || item.username} (#{item.user_id})
              </p>
              <p>
                <span className='text-muted-foreground'>
                  {t('Primary use')}:{' '}
                </span>
                {categoryLabel(item.primary_category, t)}
              </p>
              <p>
                <span className='text-muted-foreground'>{t('Risk')}: </span>
                {riskLabel(item.risk_level, t)} ({item.jailbreak_max_score})
              </p>
              {item.jailbreak_tags && (
                <p>
                  <span className='text-muted-foreground'>
                    {t('Techniques')}:{' '}
                  </span>
                  {Object.keys(item.jailbreak_tags)
                    .map((tag) => jailbreakTagLabel(tag, t))
                    .join(', ')}
                </p>
              )}
            </div>
          </div>
        }
        confirmText={t('Ban')}
        isLoading={toggleBan.isPending}
        handleConfirm={() => {
          toggleBan.mutate({ userId: item.user_id, banned: true })
          setBanOpen(false)
        }}
      />

      <ConfirmDialog
        open={purgeSamplesOpen}
        onOpenChange={setPurgeSamplesOpen}
        destructive
        title={t('Purge all samples of this user?')}
        desc={t(
          'The stored prompt snippets and raw bodies for this user will be deleted permanently. This cannot be undone, but new samples may be captured again on later requests.'
        )}
        confirmText={t('Purge')}
        isLoading={purgeSamples.isPending}
        handleConfirm={() => {
          purgeSamples.mutate(item.user_id)
          setPurgeSamplesOpen(false)
        }}
      />

      <ConfirmDialog
        open={purgeOpen}
        onOpenChange={setPurgeOpen}
        destructive
        title={t('Clear this user profile?')}
        desc={t(
          'Aggregated counters, evidence samples, pending in-memory deltas and the auto-ban mark will all be removed. This cannot be undone; the profile will be rebuilt from future requests.'
        )}
        confirmText={t('Clear profile')}
        isLoading={purgeInsight.isPending}
        handleConfirm={() => {
          purgeInsight.mutate(item.user_id)
          setPurgeOpen(false)
        }}
      />
    </div>
  )
}
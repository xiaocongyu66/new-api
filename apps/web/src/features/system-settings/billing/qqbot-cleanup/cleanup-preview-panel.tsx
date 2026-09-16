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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { api } from '@/lib/api'

import {
  getCleanupPreview,
  MEMBER_STATUS,
  type CleanupPreview,
  type GroupMember,
} from './api'

const QK = ['qq-bot', 'cleanup-preview'] as const

function formatTimestamp(ts: number): string {
  if (!ts || ts <= 0) return '-'
  // 后端给的是 unix 秒
  return new Date(ts * 1000).toLocaleString()
}

function daysSince(ts: number, now: number): number {
  if (!ts || ts <= 0) return -1
  return Math.floor((now - ts) / 86400)
}

export function CleanupPreviewPanel() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [groupOpenId, setGroupOpenId] = useState('')
  const [activeGroup, setActiveGroup] = useState('')

  const previewQuery = useQuery({
    queryKey: [...QK, activeGroup],
    queryFn: () => getCleanupPreview(activeGroup),
    enabled: !!activeGroup,
  })

  const runMutation = useMutation({
    mutationFn: async (groupId: string) => {
      const res = await api.post('/api/user/qq/cleanup/run', {
        group_open_id: groupId,
      })
      return res.data as CleanupPreview
    },
    onSuccess: () => {
      toast.success(t('Cleanup task started'))
      // 异步任务，稍后刷新预览看状态推进
      setTimeout(() => {
        queryClient.invalidateQueries({ queryKey: QK })
      }, 5000)
    },
    onError: (err: unknown) => {
      // 后端返回 {success,message}，直接读 message 给管理员看
      const msg =
        (err as { response?: { data?: { message?: string } } })?.response?.data
          ?.message ?? t('Failed to start cleanup task')
      toast.error(msg)
    },
  })

  const onLoad = () => {
    const trimmed = groupOpenId.trim()
    if (!trimmed) {
      toast.error(t('Please enter a group_openid'))
      return
    }
    setActiveGroup(trimmed)
  }

  const now = Math.floor(Date.now() / 1000)
  const members: GroupMember[] = previewQuery.data?.data?.members ?? []
  const stats = previewQuery.data?.data?.stats ?? {}
  const isDryRun = previewQuery.data?.data?.dry_run ?? false

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-end gap-2'>
        <div className='flex-1 min-w-[240px] space-y-1.5'>
          <label className='text-sm font-medium'>
            {t('Group openid')}
          </label>
          <Input
            placeholder={t('Paste the group_openid to scan')}
            value={groupOpenId}
            onChange={(e) => setGroupOpenId(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') onLoad()
            }}
          />
        </div>
        <Button variant='outline' onClick={onLoad} disabled={previewQuery.isFetching}>
          {previewQuery.isFetching ? t('Loading...') : t('Load candidates')}
        </Button>
        <Button
          onClick={() => runMutation.mutate(activeGroup)}
          disabled={!activeGroup || runMutation.isPending}
          variant='destructive'
        >
          {runMutation.isPending ? t('Starting...') : t('Run cleanup')}
        </Button>
      </div>

      {isDryRun && (
        <p className='text-sm text-muted-foreground'>
          {t(
            'Dry run is on: candidates are identified and warned only, no one is removed'
          )}
        </p>
      )}

      {activeGroup && Object.keys(stats).length > 0 && (
        <div className='flex flex-wrap gap-x-6 gap-y-1 text-sm text-muted-foreground'>
          <span>
            {t('Inactive candidates scanned')}: {stats.inactive_candidate ?? 0}
          </span>
          <span>
            {t('Warned')}: {stats.warned ?? 0}
          </span>
          <span>
            {t('Removed')}: {stats.removed ?? 0}
          </span>
          <span>
            {t('Exempt')}: {stats.exempt ?? 0}
          </span>
        </div>
      )}

      {activeGroup && (
        <div className='rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Member openid')}</TableHead>
                <TableHead>{t('Username')}</TableHead>
                <TableHead>{t('Last active')}</TableHead>
                <TableHead>{t('Days silent')}</TableHead>
                <TableHead>{t('Warned at')}</TableHead>
                <TableHead>{t('QQ number')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
                <TableHead>{t('Exempt')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {renderTableBody({
                isError: !!previewQuery.isError,
                isFetching: !!previewQuery.isFetching,
                members,
                now,
                t,
              })}
            </TableBody>
          </Table>
        </div>
      )}

      <p className='text-xs text-muted-foreground'>
        {t(
          'The official bot only knows member_openid; the real QQ number is filled in when NapCat receives the same warning message (the @ bridge). Members without a bridged number can still be warned but not removed'
        )}
      </p>
    </div>
  )
}

type TFunc = ReturnType<typeof useTranslation>['t']

function statusLabel(status: string, t: TFunc): string {
  switch (status) {
    case MEMBER_STATUS.warned:
      return t('Warned')
    case MEMBER_STATUS.removed:
      return t('Removed')
    case MEMBER_STATUS.exempt:
      return t('Exempt')
    default:
      return t('Active')
  }
}

/** 表格体三种形态：报错 / 空名单 / 成员行。拆成函数避免 JSX 里嵌套三元。 */
function renderTableBody({
  isError,
  isFetching,
  members,
  now,
  t,
}: {
  isError: boolean
  isFetching: boolean
  members: GroupMember[]
  now: number
  t: TFunc
}) {
  const emptyCell = (text: string) => (
    <TableRow>
      <TableCell colSpan={8} className='text-center text-muted-foreground'>
        {text}
      </TableCell>
    </TableRow>
  )

  if (isError) {
    return emptyCell(
      t(
        'Failed to load candidates. Check the group openid and whether cleanup is enabled for it.'
      )
    )
  }
  if (members.length === 0 && !isFetching) {
    return emptyCell(t('No inactive members found in this group'))
  }

  return members.map((m) => (
    <TableRow key={m.id}>
      <TableCell className='font-mono text-xs max-w-[180px] truncate'>
        {m.member_open_id}
      </TableCell>
      <TableCell>{m.username || '-'}</TableCell>
      <TableCell className='whitespace-nowrap'>
        {formatTimestamp(m.last_active_at)}
      </TableCell>
      <TableCell>
        {daysSince(m.last_active_at, now) >= 0
          ? daysSince(m.last_active_at, now)
          : '-'}
      </TableCell>
      <TableCell className='whitespace-nowrap'>
        {formatTimestamp(m.warned_at)}
      </TableCell>
      <TableCell>{m.qq_number > 0 ? m.qq_number : t('Not bridged')}</TableCell>
      <TableCell>{statusLabel(m.status, t)}</TableCell>
      <TableCell className='text-muted-foreground text-xs'>
        {m.exempt ? m.exempt_reason || t('Exempt') : '-'}
      </TableCell>
    </TableRow>
  ))
}

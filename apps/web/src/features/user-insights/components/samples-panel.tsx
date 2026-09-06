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
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  useDeleteUserSamples,
  useInsightSampleGroups,
} from '../hooks/use-user-insights'
import {
  formatBytes,
  formatTimestamp,
  riskBadgeVariant,
  riskLabel,
} from '../lib/labels'
import type { InsightSampleFilters, InsightSampleGroup } from '../types'
import { SampleDetailSheet } from './sample-detail-sheet'
import { UserEvidenceSheet } from './user-evidence-sheet'

const PAGE_SIZE = 20

/**
 * 证据样本面板，按用户归类。
 *
 * 之前这里是逐请求平铺，结果线上 547 条样本里 499 条来自同一个用户的
 * 同一份 agent 注入提示词，整页都在重复同一条证据。现在改成两层结构：
 * 一个用户一行，点开抽屉才看该用户的具体命中模式（写库时已按指纹去重）。
 *
 * 同时显示缓存库的容量水位——这个库是有硬上限的滚动缓存，
 * 满了会按"优先级低 + 时间早"淘汰，不是审计留档。
 */
export function SamplesPanel() {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [filters, setFilters] = useState<Omit<InsightSampleFilters, 'userId'>>({
    risk: '',
    relayOnly: false,
  })
  const [openUser, setOpenUser] = useState<InsightSampleGroup | null>(null)
  const [rawBodySampleId, setRawBodySampleId] = useState<number | null>(null)
  const [pendingPurgeUserId, setPendingPurgeUserId] = useState<number | null>(
    null
  )

  const groupsQuery = useInsightSampleGroups(page, PAGE_SIZE, filters)
  const purgeSamples = useDeleteUserSamples()

  const items = groupsQuery.data?.data.items ?? []
  const total = groupsQuery.data?.data.total ?? 0
  const quota = groupsQuery.data?.data.quota
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const usedRatio =
    quota && quota.limit_bytes > 0
      ? Math.min(100, (quota.used_bytes / quota.limit_bytes) * 100)
      : 0

  return (
    <div className='space-y-4'>
      {quota && (
        <div className='space-y-2 rounded-md border p-4'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <div className='flex items-center gap-2'>
              <h3 className='text-sm font-medium'>
                {t('Evidence cache usage')}
              </h3>
              {!quota.enabled && (
                <Badge variant='outline'>{t('Collection off')}</Badge>
              )}
              {quota.keep_body && (
                <Badge variant='secondary'>{t('Full body retained')}</Badge>
              )}
              <Badge variant='outline'>
                {t('Sampling {{rate}}%', { rate: quota.sample_rate })}
              </Badge>
            </div>
            <span className='text-muted-foreground text-xs tabular-nums'>
              {formatBytes(quota.used_bytes)} / {formatBytes(quota.limit_bytes)}
            </span>
          </div>
          <Progress value={usedRatio} />
          <p className='text-muted-foreground text-xs'>
            {t(
              'Rolling cache with a hard cap. When full, ordinary samples are evicted first, then relay ones; jailbreak evidence is dropped last.'
            )}
          </p>
        </div>
      )}

      <div className='flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center'>
        <Select
          value={filters.risk || 'all'}
          onValueChange={(value) => {
            setPage(1)
            setFilters((prev) => ({
              ...prev,
              risk:
                value === 'all' ? '' : (value as InsightSampleFilters['risk']),
            }))
          }}
        >
          <SelectTrigger className='w-full sm:w-[180px]'>
            <SelectValue placeholder={t('Risk')} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='all'>{t('All risk levels')}</SelectItem>
            <SelectItem value='confirmed'>
              {t('Confirmed jailbreak')}
            </SelectItem>
            <SelectItem value='likely'>{t('Likely jailbreak')}</SelectItem>
            <SelectItem value='suspect'>{t('Suspected jailbreak')}</SelectItem>
            <SelectItem value='none'>{t('Clean')}</SelectItem>
          </SelectContent>
        </Select>

        <Button
          variant={filters.relayOnly ? 'default' : 'outline'}
          size='sm'
          className='w-full sm:w-auto'
          onClick={() => {
            setPage(1)
            setFilters((prev) => ({ ...prev, relayOnly: !prev.relayOnly }))
          }}
        >
          {t('Relay traffic only')}
        </Button>
      </div>

      {groupsQuery.isLoading ? (
        <div className='space-y-2'>
          {Array.from({ length: 6 }).map((_, index) => (
            <Skeleton key={index} className='h-12 w-full' />
          ))}
        </div>
      ) : items.length === 0 ? (
        <p className='text-muted-foreground py-12 text-center text-sm'>
          {t('No samples captured yet')}
        </p>
      ) : (
        <>
          {/* 桌面端：宽表格。移动端隐藏，改用下方卡片列表。 */}
          <div className='hidden overflow-x-auto md:block'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('User')}</TableHead>
                  <TableHead>{t('Risk')}</TableHead>
                  <TableHead>{t('Hit patterns')}</TableHead>
                  <TableHead>{t('Matched requests')}</TableHead>
                  <TableHead>{t('Last active')}</TableHead>
                  <TableHead>{t('Size')}</TableHead>
                  <TableHead className='text-right'>{t('Actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((group) => (
                  <TableRow key={group.user_id}>
                    <TableCell>
                      <div className='flex flex-col'>
                        <span className='text-sm'>{group.username}</span>
                        <span className='text-muted-foreground text-xs'>
                          #{group.user_id}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge variant={riskBadgeVariant(group.max_risk)}>
                        {riskLabel(group.max_risk, t)}
                        {group.max_score > 0 ? ` (${group.max_score})` : ''}
                      </Badge>
                    </TableCell>
                    <TableCell className='text-xs tabular-nums'>
                      {group.samples}
                    </TableCell>
                    <TableCell className='text-xs tabular-nums'>
                      {group.hit_total}
                    </TableCell>
                    <TableCell className='text-muted-foreground text-xs whitespace-nowrap'>
                      {formatTimestamp(group.last_seen_at, t)}
                    </TableCell>
                    <TableCell className='text-muted-foreground text-xs tabular-nums'>
                      {formatBytes(group.byte_size)}
                    </TableCell>
                    <TableCell className='text-right'>
                      <div className='flex items-center justify-end gap-2'>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() => setOpenUser(group)}
                        >
                          {t('View evidence')}
                        </Button>
                        <Button
                          size='sm'
                          variant='destructive'
                          disabled={purgeSamples.isPending}
                          onClick={() => setPendingPurgeUserId(group.user_id)}
                        >
                          {t('Purge')}
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>

          {/* 移动端：卡片列表。 */}
          <div className='space-y-3 md:hidden'>
            {items.map((group) => (
              <div
                key={group.user_id}
                className='space-y-3 rounded-lg border p-3'
              >
                <div className='flex items-start justify-between gap-2'>
                  <div className='flex min-w-0 flex-col'>
                    <span className='truncate text-sm'>{group.username}</span>
                    <span className='text-muted-foreground text-xs'>
                      #{group.user_id}
                    </span>
                  </div>
                  <Badge variant={riskBadgeVariant(group.max_risk)}>
                    {riskLabel(group.max_risk, t)}
                    {group.max_score > 0 ? ` (${group.max_score})` : ''}
                  </Badge>
                </div>

                <div className='grid grid-cols-2 gap-x-3 gap-y-2 text-sm'>
                  <div className='flex flex-col'>
                    <span className='text-muted-foreground text-xs'>
                      {t('Hit patterns')}
                    </span>
                    <span className='tabular-nums'>{group.samples}</span>
                  </div>
                  <div className='flex flex-col'>
                    <span className='text-muted-foreground text-xs'>
                      {t('Matched requests')}
                    </span>
                    <span className='tabular-nums'>{group.hit_total}</span>
                  </div>
                  <div className='flex flex-col'>
                    <span className='text-muted-foreground text-xs'>
                      {t('Last active')}
                    </span>
                    <span className='text-xs'>
                      {formatTimestamp(group.last_seen_at, t)}
                    </span>
                  </div>
                  <div className='flex flex-col'>
                    <span className='text-muted-foreground text-xs'>
                      {t('Size')}
                    </span>
                    <span className='text-xs tabular-nums'>
                      {formatBytes(group.byte_size)}
                    </span>
                  </div>
                </div>

                <div className='flex items-center gap-2'>
                  <Button
                    size='sm'
                    variant='outline'
                    className='flex-1'
                    onClick={() => setOpenUser(group)}
                  >
                    {t('View evidence')}
                  </Button>
                  <Button
                    size='sm'
                    variant='destructive'
                    className='flex-1'
                    disabled={purgeSamples.isPending}
                    onClick={() => setPendingPurgeUserId(group.user_id)}
                  >
                    {t('Purge')}
                  </Button>
                </div>
              </div>
            ))}
          </div>
        </>
      )}

      <div className='flex items-center justify-between'>
        <p className='text-muted-foreground text-xs'>
          {t('{{count}} users with samples', { count: total })}
        </p>
        <div className='flex items-center gap-2'>
          <Button
            variant='outline'
            size='sm'
            disabled={page <= 1}
            onClick={() => setPage((prev) => Math.max(1, prev - 1))}
          >
            {t('Previous')}
          </Button>
          <span className='text-muted-foreground text-xs tabular-nums'>
            {page} / {totalPages}
          </span>
          <Button
            variant='outline'
            size='sm'
            disabled={page >= totalPages}
            onClick={() => setPage((prev) => prev + 1)}
          >
            {t('Next')}
          </Button>
        </div>
      </div>

      <UserEvidenceSheet
        userId={openUser?.user_id ?? null}
        username={openUser?.username}
        onClose={() => setOpenUser(null)}
        onOpenRawBody={setRawBodySampleId}
      />
      <SampleDetailSheet
        sampleId={rawBodySampleId}
        onClose={() => setRawBodySampleId(null)}
      />

      <AlertDialog
        open={pendingPurgeUserId !== null}
        onOpenChange={(open) => !open && setPendingPurgeUserId(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Purge all samples of this user?')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'The stored prompt snippets and raw bodies for this user will be deleted permanently. This cannot be undone, but new samples may be captured again on later requests.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (pendingPurgeUserId !== null) {
                  purgeSamples.mutate(pendingPurgeUserId)
                }
                setPendingPurgeUserId(null)
              }}
            >
              {t('Purge')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
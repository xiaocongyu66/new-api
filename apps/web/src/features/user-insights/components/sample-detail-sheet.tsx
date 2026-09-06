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

import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Skeleton } from '@/components/ui/skeleton'
import { useInsightSampleDetail } from '../hooks/use-user-insights'
import {
  clientLabel,
  formatBytes,
  formatTimestamp,
  riskBadgeVariant,
  riskLabel,
} from '../lib/labels'
import { EvidenceList } from './evidence-list'

type SampleDetailSheetProps = {
  sampleId: number | null
  onClose: () => void
}

/**
 * 单条样本的复核抽屉。
 *
 * 上半部分是命中证据（关键词 + 原句），下半部分是请求体原文。
 * 原文只有在系统设置里开启"完整留存"时才有值，因为它体积远大于片段；
 * 未开启时提示管理员去哪里打开，而不是显示空白。
 */
export function SampleDetailSheet({
  sampleId,
  onClose,
}: SampleDetailSheetProps) {
  const { t } = useTranslation()
  const detailQuery = useInsightSampleDetail(sampleId)
  const sample = detailQuery.data?.data

  return (
    <Sheet
      open={sampleId !== null}
      onOpenChange={(open) => !open && onClose()}
    >
      <SheetContent className='flex w-full flex-col gap-0 sm:max-w-2xl'>
        <SheetHeader>
          <SheetTitle>{t('Request evidence')}</SheetTitle>
          <SheetDescription>
            {t(
              'Keyword hits with their original sentences. Use this to verify a score before taking action.'
            )}
          </SheetDescription>
        </SheetHeader>

        <ScrollArea className='flex-1 px-4 pb-6'>
          {detailQuery.isLoading && (
            <div className='space-y-2'>
              {Array.from({ length: 5 }).map((_, index) => (
                <Skeleton key={index} className='h-16 w-full' />
              ))}
            </div>
          )}

          {!detailQuery.isLoading && !sample && (
            <p className='text-muted-foreground py-12 text-center text-sm'>
              {t('Sample not found. It may have been evicted by the quota.')}
            </p>
          )}

          {sample && (
            <div className='space-y-5'>
              <div className='grid grid-cols-2 gap-x-4 gap-y-2 text-xs'>
                <MetaRow label={t('User')}>
                  {sample.username} (#{sample.user_id})
                </MetaRow>
                <MetaRow label={t('Time')}>
                  {formatTimestamp(sample.created_at, t)}
                </MetaRow>
                <MetaRow label={t('Model')}>{sample.model_name || '—'}</MetaRow>
                <MetaRow label={t('Endpoint')}>{sample.path || '—'}</MetaRow>
                <MetaRow label={t('Client')}>
                  {sample.client ? clientLabel(sample.client) : '—'}
                  {sample.client_version ? ` ${sample.client_version}` : ''}
                </MetaRow>
                <MetaRow label={t('Request ID')}>
                  <span className='font-mono'>{sample.request_id || '—'}</span>
                </MetaRow>
              </div>

              <div className='flex flex-wrap items-center gap-2'>
                <Badge variant={riskBadgeVariant(sample.risk_level ?? 'none')}>
                  {riskLabel(sample.risk_level ?? 'none', t)}
                  {sample.jailbreak_score > 0
                    ? ` (${sample.jailbreak_score})`
                    : ''}
                </Badge>
                {sample.is_relay && (
                  <Badge variant='secondary'>
                    {t('via relay')}
                    {sample.relay_vendor ? `: ${sample.relay_vendor}` : ''}
                  </Badge>
                )}
                <Badge variant='outline'>
                  {t('{{count}} hits', { count: sample.evidence_count })}
                </Badge>
              </div>

              <EvidenceList items={sample.evidence ?? []} />

              <div className='space-y-2'>
                <div className='flex items-center justify-between'>
                  <h4 className='text-sm font-medium'>
                    {t('Raw request body')}
                  </h4>
                  <span className='text-muted-foreground text-xs'>
                    {formatBytes(sample.body_size)}
                  </span>
                </div>
                {sample.body ? (
                  <pre className='bg-muted/40 max-h-96 overflow-auto rounded-md p-3 text-[11px] leading-relaxed break-words whitespace-pre-wrap'>
                    {sample.body}
                  </pre>
                ) : (
                  <p className='text-muted-foreground text-xs'>
                    {sample.body_size > 0
                      ? t(
                          'Full body retention is off, so only the evidence snippets were kept. Enable it in system settings if you need the raw request.'
                        )
                      : t('No request body was captured.')}
                  </p>
                )}
              </div>
            </div>
          )}
        </ScrollArea>
      </SheetContent>
    </Sheet>
  )
}

function MetaRow({
  label,
  children,
}: {
  label: string
  children: ReactNode
}) {
  return (
    <div className='flex flex-col'>
      <span className='text-muted-foreground'>{label}</span>
      <span className='break-all'>{children}</span>
    </div>
  )
}
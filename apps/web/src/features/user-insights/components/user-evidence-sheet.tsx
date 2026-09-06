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
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { useUserInsightSamples } from '../hooks/use-user-insights'
import {
  clientLabel,
  formatBytes,
  formatTimestamp,
  riskBadgeVariant,
  riskLabel,
} from '../lib/labels'
import { EvidenceList } from './evidence-list'

type UserEvidenceSheetProps = {
  userId: number | null
  username?: string
  onClose: () => void
  /** 打开完整请求体抽屉。不传则不显示该入口。 */
  onOpenRawBody?: (sampleId: number) => void
}

/**
 * 某个用户的证据抽屉。
 *
 * 这是管理员实际工作流的落点："看到可疑用户 → 查证据 → 决定封禁"，
 * 所以入口就放在画像表格的封禁按钮旁边，而不是另一个页签。
 *
 * 样本已按"命中模式指纹"在写库时去重，一行代表一种模式，
 * hit_count 表示这种模式出现过多少次请求。
 */
export function UserEvidenceSheet({
  userId,
  username,
  onClose,
  onOpenRawBody,
}: UserEvidenceSheetProps) {
  const { t } = useTranslation()
  const samplesQuery = useUserInsightSamples(userId, {})
  const samples = samplesQuery.data?.data.items ?? []

  return (
    <Sheet open={userId !== null} onOpenChange={(open) => !open && onClose()}>
      <SheetContent className='flex w-full flex-col gap-0 sm:max-w-2xl'>
        <SheetHeader>
          <SheetTitle>
            {username
              ? t('Evidence for {{name}}', { name: username })
              : t('Request evidence')}
          </SheetTitle>
          <SheetDescription>
            {t(
              'Each row is one distinct hit pattern, deduplicated across requests. Verify these before banning.'
            )}
          </SheetDescription>
        </SheetHeader>

        <ScrollArea className='flex-1 px-4 pb-6'>
          {samplesQuery.isLoading && (
            <div className='space-y-2'>
              {Array.from({ length: 4 }).map((_, index) => (
                <Skeleton key={index} className='h-16 w-full' />
              ))}
            </div>
          )}

          {!samplesQuery.isLoading && samples.length === 0 && (
            <p className='text-muted-foreground py-12 text-center text-sm'>
              {t(
                'No evidence samples for this user. Sampling sees only a fraction of ordinary requests.'
              )}
            </p>
          )}

          <div className='space-y-3'>
            {samples.map((sample) => (
              <Collapsible
                key={sample.id}
                className='rounded-md border'
                defaultOpen={samples.length === 1}
              >
                <CollapsibleTrigger className='hover:bg-muted/40 flex w-full flex-col gap-1.5 rounded-md p-3 text-left'>
                  <div className='flex flex-wrap items-center gap-1.5'>
                    <Badge
                      variant={riskBadgeVariant(sample.risk_level ?? 'none')}
                    >
                      {riskLabel(sample.risk_level ?? 'none', t)}
                      {sample.jailbreak_score > 0
                        ? ` (${sample.jailbreak_score})`
                        : ''}
                    </Badge>
                    {sample.client && (
                      <Badge variant='outline' className='text-[11px]'>
                        {clientLabel(sample.client)}
                        {sample.client_version
                          ? ` ${sample.client_version}`
                          : ''}
                      </Badge>
                    )}
                    {sample.is_relay && (
                      <Badge variant='secondary' className='text-[11px]'>
                        {t('via relay')}
                      </Badge>
                    )}
                    <Badge variant='outline' className='text-[11px]'>
                      {t('{{count}} hits', { count: sample.evidence_count })}
                    </Badge>
                    {sample.hit_count > 1 && (
                      <Badge variant='secondary' className='text-[11px]'>
                        {t('seen {{count}} times', { count: sample.hit_count })}
                      </Badge>
                    )}
                  </div>
                  <div className='text-muted-foreground flex flex-wrap gap-x-3 text-xs'>
                    <span>{sample.model_name || '—'}</span>
                    <span>
                      {formatTimestamp(
                        sample.last_seen_at || sample.created_at,
                        t
                      )}
                    </span>
                    <span className='tabular-nums'>
                      {formatBytes(sample.byte_size)}
                    </span>
                  </div>
                </CollapsibleTrigger>
                <CollapsibleContent className='space-y-3 border-t p-3'>
                  <EvidenceList items={sample.evidence ?? []} />
                  {onOpenRawBody && (
                    <Button
                      size='sm'
                      variant='outline'
                      onClick={() => onOpenRawBody(sample.id)}
                    >
                      {t('Raw request body')}
                    </Button>
                  )}
                </CollapsibleContent>
              </Collapsible>
            ))}
          </div>
        </ScrollArea>
      </SheetContent>
    </Sheet>
  )
}
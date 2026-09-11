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
import { useState } from 'react'

import { ConfirmDialog } from '@/components/confirm-dialog'
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
import {
  useToggleClientBan,
  useToggleUserClientBan,
  useUserInsightSamples,
} from '../hooks/use-user-insights'
import {
  clientLabel,
  formatBytes,
  formatTimestamp,
  riskBadgeVariant,
  riskLabel,
} from '../lib/labels'
import type { InsightClientUsage, UserInsight } from '../types'
import { EvidenceList } from './evidence-list'

type UserEvidenceSheetProps = {
  userId: number | null
  username?: string
  /** 该用户的完整画像：驱动"客户端访问控制"区块（客户端列表 + 封禁/禁用状态）。 */
  item?: UserInsight | null
  /** 全站封禁的客户端 ID 列表（user_insight_setting.blocked_clients）。 */
  blockedClients?: string[]
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
  item,
  blockedClients = [],
  onClose,
  onOpenRawBody,
}: UserEvidenceSheetProps) {
  const { t } = useTranslation()
  const samplesQuery = useUserInsightSamples(userId, {})
  const samples = samplesQuery.data?.data.items ?? []
  const clients = item?.clients ?? []

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

        {clients.length > 0 && (
          <div className='border-b px-4 py-3'>
            <div className='text-muted-foreground mb-2 text-xs font-medium tracking-wide'>
              {t('Client access control')}
            </div>
            <div className='space-y-2'>
              {clients.map((client) => (
                <ClientAccessRow
                  key={client.client}
                  item={item as UserInsight}
                  client={client}
                  blockedClients={blockedClients}
                />
              ))}
            </div>
          </div>
        )}

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

/**
 * 单个客户端的访问控制行：
 *  - 全站封禁 / 解封（user_insight_setting.blocked_clients，影响所有用户）；
 *  - 仅对该用户禁用 / 恢复（user_insight_client_bans，不影响该用户的其它客户端）。
 * 全站封禁是面向全站的不可即时回退动作（撤销要再操作一次），故封禁走二次确认；
 * 单用户禁用影响面小且可逆，直接执行。
 */
function ClientAccessRow({
  item,
  client,
  blockedClients,
}: {
  item: UserInsight
  client: InsightClientUsage
  blockedClients: string[]
}) {
  const { t } = useTranslation()
  const toggleClientBan = useToggleClientBan()
  const toggleUserClientBan = useToggleUserClientBan()
  const [banConfirmOpen, setBanConfirmOpen] = useState(false)

  const globalBanned = blockedClients.includes(client.client)
  const userDisabled = (item.disabled_clients ?? []).includes(client.client)

  return (
    <div className='flex flex-wrap items-center gap-2 rounded-md border p-2'>
      <div className='flex min-w-0 flex-1 items-center gap-1.5'>
        <Badge
          variant={globalBanned ? 'destructive' : 'outline'}
          className='text-[11px]'
        >
          {clientLabel(client.client)}
          {client.version ? ` ${client.version}` : ''}
        </Badge>
        <span className='text-muted-foreground text-[11px] tabular-nums'>
          {t('{{count}} requests', { count: client.count })}
        </span>
        {globalBanned && (
          <Badge variant='destructive' className='text-[10px]'>
            {t('Banned site-wide')}
          </Badge>
        )}
        {userDisabled && (
          <Badge variant='secondary' className='text-[10px]'>
            {t('Disabled for this user')}
          </Badge>
        )}
      </div>

      <div className='flex shrink-0 items-center gap-1.5'>
        <Button
          size='sm'
          variant={globalBanned ? 'outline' : 'destructive'}
          disabled={toggleClientBan.isPending}
          onClick={() => {
            if (globalBanned) {
              toggleClientBan.mutate({ client: client.client, banned: false })
            } else {
              setBanConfirmOpen(true)
            }
          }}
        >
          {globalBanned ? t('Unban site-wide') : t('Ban site-wide')}
        </Button>
        <Button
          size='sm'
          variant='outline'
          disabled={toggleUserClientBan.isPending}
          onClick={() =>
            toggleUserClientBan.mutate({
              userId: item.user_id,
              client: client.client,
              banned: !userDisabled,
            })
          }
        >
          {userDisabled
            ? t('Re-enable for this user')
            : t('Disable for this user')}
        </Button>
      </div>

      <ConfirmDialog
        open={banConfirmOpen}
        onOpenChange={setBanConfirmOpen}
        destructive
        title={t('Ban this client site-wide?')}
        desc={t(
          'Every user whose requests are identified as this client by request headers will be rejected with a 403. This is reversible.'
        )}
        confirmText={t('Ban site-wide')}
        isLoading={toggleClientBan.isPending}
        handleConfirm={() => {
          toggleClientBan.mutate({ client: client.client, banned: true })
          setBanConfirmOpen(false)
        }}
      />
    </div>
  )
}
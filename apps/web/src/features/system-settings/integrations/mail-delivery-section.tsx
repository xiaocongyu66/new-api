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
import { Link } from '@tanstack/react-router'
import { Mail } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import dayjs from '@/lib/dayjs'
import { cn } from '@/lib/utils'

import { SettingsSection } from '../components/settings-section'
import { useSMTPPoolStatus } from '../hooks/use-smtp-pool-status'
import type { SMTPSenderStatus, SMTPPoolStatus } from '../types'

// Go serializes an unset time.Time as year 1, so anything that old is "never".
const GO_ZERO_TIME_YEAR = 1
const QUOTA_WARNING_RATIO = 0.8

function formatSenderTime(value: string): string | null {
  const parsed = dayjs(value)
  if (!parsed.isValid() || parsed.year() <= GO_ZERO_TIME_YEAR) return null
  return parsed.format('YYYY-MM-DD HH:mm:ss')
}

function groupSendersByPriority(senders: SMTPSenderStatus[]) {
  const tiers = new Map<number, SMTPSenderStatus[]>()
  for (const sender of senders) {
    const accounts = tiers.get(sender.priority) ?? []
    accounts.push(sender)
    tiers.set(sender.priority, accounts)
  }
  return [...tiers.entries()]
    .sort(([left], [right]) => left - right)
    .map(([priority, accounts]) => ({ priority, accounts }))
}

function getQuotaBarClass(usageRatio: number) {
  if (usageRatio >= 1) {
    return '[&_[data-slot=progress-indicator]]:bg-destructive'
  }
  if (usageRatio >= QUOTA_WARNING_RATIO) {
    return '[&_[data-slot=progress-indicator]]:bg-warning'
  }
  return '[&_[data-slot=progress-indicator]]:bg-primary'
}

function SMTPAccountCard({ sender }: { sender: SMTPSenderStatus }) {
  const { t } = useTranslation()
  const unlimited = sender.dailyLimit <= 0
  const usageRatio = unlimited
    ? 0
    : Math.min(1, sender.usedToday / sender.dailyLimit)
  const lastSentAt = formatSenderTime(sender.lastSentAt)
  const lastErrorAt = formatSenderTime(sender.lastErrorAt)

  return (
    <Card>
      <CardHeader>
        <div className='flex flex-wrap items-center gap-2'>
          <CardTitle className='min-w-0 truncate'>{sender.account}</CardTitle>
          <Badge variant='outline'>
            {sender.server}:{sender.port}
          </Badge>
          <Badge variant='secondary'>
            {t('Priority {{priority}}', { priority: sender.priority })}
          </Badge>
        </div>
        <CardDescription>
          {unlimited
            ? t('Unlimited')
            : `${sender.usedToday} / ${sender.dailyLimit}`}
        </CardDescription>
      </CardHeader>
      <CardContent className='flex flex-col gap-3'>
        {unlimited ? (
          <div className='text-muted-foreground text-sm'>
            {t('Unlimited')} · {t('Remaining')} {sender.remainingToday}
          </div>
        ) : (
          <div className='flex flex-col gap-1.5'>
            <Progress
              value={usageRatio * 100}
              aria-label={t('Daily quota')}
              className={cn('w-full', getQuotaBarClass(usageRatio))}
            />
            <div className='text-muted-foreground flex items-center justify-between text-xs'>
              <span className='tabular-nums'>
                {sender.usedToday} / {sender.dailyLimit}
              </span>
              <span className='tabular-nums'>
                {t('Remaining')} {sender.remainingToday}
              </span>
            </div>
          </div>
        )}

        <dl className='grid gap-x-6 gap-y-1.5 text-sm md:grid-cols-[6rem_1fr]'>
          <dt className='text-muted-foreground'>{t('Last sent')}</dt>
          <dd className='tabular-nums'>
            {lastSentAt
              ? `${lastSentAt} (${dayjs(sender.lastSentAt).fromNow()})`
              : t('Never')}
          </dd>
          <dt className='text-muted-foreground'>{t('Last error')}</dt>
          <dd className='min-w-0'>
            {sender.lastError ? (
              <div className='flex flex-col gap-0.5'>
                <span className='text-destructive break-all'>
                  {sender.lastError}
                </span>
                {lastErrorAt && (
                  <span className='text-muted-foreground text-xs tabular-nums'>
                    {lastErrorAt} ({dayjs(sender.lastErrorAt).fromNow()})
                  </span>
                )}
              </div>
            ) : (
              t('Never')
            )}
          </dd>
        </dl>
      </CardContent>
    </Card>
  )
}

function SMTPPoolSummary({ status }: { status: SMTPPoolStatus }) {
  const { t } = useTranslation()

  return (
    <div className='grid gap-4 sm:grid-cols-2'>
      <Card>
        <CardHeader>
          <CardTitle>{t('Sent today')}</CardTitle>
          <CardDescription>
            {t('Across accounts with a daily limit')}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className='text-2xl font-semibold tabular-nums'>
            {status.sentToday}
          </div>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>{t('Remaining today')}</CardTitle>
          <CardDescription>
            {t('Across accounts with a daily limit')}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className='text-2xl font-semibold tabular-nums'>
            {status.remainingToday}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

export function MailDeliverySection() {
  const { t } = useTranslation()
  const { data, isLoading, isError, error, refetch } = useSMTPPoolStatus()
  const status = data?.data
  const senders = status?.senders ?? []

  if (isLoading) {
    return (
      <SettingsSection title={t('Mail Delivery')}>
        <Skeleton className='h-32 w-full' />
      </SettingsSection>
    )
  }

  if (isError) {
    return (
      <SettingsSection title={t('Mail Delivery')}>
        <Alert variant='destructive'>
          <AlertTitle>{t('Failed to load mail delivery status')}</AlertTitle>
          <AlertDescription className='flex flex-col items-start gap-2'>
            {error instanceof Error && error.message ? (
              <span className='break-all'>{error.message}</span>
            ) : null}
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => refetch()}
            >
              {t('Retry')}
            </Button>
          </AlertDescription>
        </Alert>
      </SettingsSection>
    )
  }

  if (senders.length === 0) {
    return (
      <SettingsSection title={t('Mail Delivery')}>
        <Empty className='min-h-64'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <Mail />
            </EmptyMedia>
            <EmptyTitle>{t('No SMTP accounts configured')}</EmptyTitle>
            <EmptyDescription>
              {t(
                "Configure an SMTP account pool to see each account's delivery status here."
              )}
            </EmptyDescription>
          </EmptyHeader>
          <Button
            variant='outline'
            render={
              <Link
                to='/admin/system-settings/operations/$section'
                params={{ section: 'email' }}
              />
            }
          >
            {t('Go to SMTP Email settings')}
          </Button>
        </Empty>
      </SettingsSection>
    )
  }

  const tiers = groupSendersByPriority(senders)

  return (
    <SettingsSection title={t('Mail Delivery')}>
      {status && <SMTPPoolSummary status={status} />}
      <div className='flex flex-col gap-5'>
        {tiers.map((tier) => (
          <div key={tier.priority} className='flex flex-col gap-3'>
            <div className='flex items-center gap-2'>
              <span className='text-muted-foreground text-xs font-medium'>
                {t('Priority {{priority}}', { priority: tier.priority })}
              </span>
              <Badge variant='secondary'>
                {t('{{count}} accounts', { count: tier.accounts.length })}
              </Badge>
            </div>
            <div className='grid gap-4 xl:grid-cols-2'>
              {tier.accounts.map((sender) => (
                <SMTPAccountCard
                  key={`${sender.server}:${sender.port}:${sender.account}`}
                  sender={sender}
                />
              ))}
            </div>
          </div>
        ))}
      </div>
    </SettingsSection>
  )
}

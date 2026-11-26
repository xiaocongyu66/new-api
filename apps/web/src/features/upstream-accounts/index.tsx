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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Loader2, Pencil, PlugZap, Plus, Trash2 } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { SettingsPageActionsPortal } from '@/features/system-settings/components/settings-page-context'

import {
  deleteUpstreamAccount,
  getUpstreamAccounts,
  testUpstreamAccount,
} from './api'
import type { UpstreamAccount } from './types'
import { UpstreamAccountDialog } from './upstream-account-dialog'

type UpstreamAccountsProps = {
  /**
   * Tabs keep their content mounted, so an inactive accounts tab must not
   * portal its actions into the page header owned by the active tab.
   */
  active?: boolean
}

type UpstreamAccountCardProps = {
  account: UpstreamAccount
  isTesting: boolean
  testDisabled: boolean
  onTest: () => void
  onEdit: () => void
  onDelete: () => void
}

function UpstreamAccountCard({
  account,
  isTesting,
  testDisabled,
  onTest,
  onEdit,
  onDelete,
}: UpstreamAccountCardProps) {
  const { t } = useTranslation()
  const tokenExpired =
    account.has_token &&
    account.token_expires_at > 0 &&
    account.token_expires_at * 1000 < Date.now()
  let tokenLabel = t('Configured')
  let tokenVariant: StatusVariant = 'success'
  if (!account.has_token) {
    tokenLabel = t('Not configured')
    tokenVariant = 'neutral'
  } else if (tokenExpired) {
    tokenLabel = t('Expired')
    tokenVariant = 'danger'
  } else if (account.token_expires_at > 0) {
    tokenLabel = t('Expires {{date}}', {
      date: new Date(account.token_expires_at * 1000).toLocaleString(),
    })
  }

  return (
    <Card size='sm' className='gap-0 py-0'>
      <CardHeader className='grid-cols-[minmax(0,1fr)_auto] items-start gap-3 border-b px-4 py-3'>
        <div className='min-w-0 space-y-1'>
          <div className='flex min-w-0 flex-wrap items-center gap-2'>
            <CardTitle className='truncate'>{account.host}</CardTitle>
            <Badge variant='outline'>{account.kind}</Badge>
            <Badge variant={account.enabled ? 'outline' : 'secondary'}>
              {t(account.enabled ? 'Enabled' : 'Disabled')}
            </Badge>
          </div>
          <p
            className='text-muted-foreground truncate text-xs'
            title={account.base_url}
          >
            {account.base_url}
          </p>
        </div>
        <div className='flex shrink-0 items-center gap-1'>
          <Button
            size='icon-sm'
            variant='ghost'
            aria-label={t('Edit')}
            onClick={onEdit}
          >
            <Pencil />
          </Button>
          <Button
            size='icon-sm'
            variant='ghost'
            aria-label={t('Delete')}
            onClick={onDelete}
          >
            <Trash2 />
          </Button>
        </div>
      </CardHeader>

      <CardContent className='px-4 py-3'>
        <dl className='grid grid-cols-2 gap-x-6 gap-y-3 text-sm sm:grid-cols-3 lg:grid-cols-5'>
          <div className='min-w-0'>
            <dt className='text-muted-foreground text-xs'>{t('Username')}</dt>
            <dd className='break-all'>{account.username || '—'}</dd>
          </div>
          <div className='min-w-0'>
            <dt className='text-muted-foreground text-xs'>{t('Password')}</dt>
            <dd>
              <StatusBadge
                label={
                  account.has_password ? t('Configured') : t('Not configured')
                }
                variant={account.has_password ? 'success' : 'neutral'}
                copyable={false}
              />
            </dd>
          </div>
          <div className='min-w-0'>
            <dt className='text-muted-foreground text-xs'>
              {t('JK Group ID')}
            </dt>
            <dd className='truncate'>{account.jk_group_id || '—'}</dd>
          </div>
          <div className='min-w-0'>
            <dt className='text-muted-foreground text-xs'>
              {t('Token Status')}
            </dt>
            <dd>
              <StatusBadge
                label={tokenLabel}
                variant={tokenVariant}
                copyable={false}
              />
            </dd>
          </div>
          <div className='col-span-2 min-w-0 sm:col-span-1'>
            <dt className='text-muted-foreground text-xs'>{t('Last test')}</dt>
            <dd>
              {account.last_test_at > 0 ? (
                <div className='min-w-0 space-y-1'>
                  <StatusBadge
                    label={t(
                      account.last_test_ok ? 'Test passed' : 'Test failed'
                    )}
                    variant={account.last_test_ok ? 'success' : 'danger'}
                    copyable={false}
                  />
                  <p
                    className='text-muted-foreground truncate text-xs'
                    title={account.last_test_message}
                  >
                    {new Date(account.last_test_at * 1000).toLocaleString()}
                    {account.last_test_message
                      ? ` · ${account.last_test_message}`
                      : ''}
                  </p>
                </div>
              ) : (
                <span className='text-muted-foreground text-xs'>
                  {t('Never tested')}
                </span>
              )}
            </dd>
          </div>
        </dl>
      </CardContent>

      <CardFooter className='justify-end gap-2 border-t px-4 py-2'>
        <Button
          size='sm'
          variant='outline'
          disabled={testDisabled}
          onClick={onTest}
        >
          {isTesting ? <Loader2 className='animate-spin' /> : <PlugZap />}
          {t('Test connection')}
        </Button>
      </CardFooter>
    </Card>
  )
}

export function UpstreamAccounts({
  active = true,
}: UpstreamAccountsProps = {}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingAccount, setEditingAccount] = useState<
    UpstreamAccount | undefined
  >(undefined)
  const [pendingDelete, setPendingDelete] = useState<
    UpstreamAccount | undefined
  >(undefined)

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['upstream-accounts'],
    queryFn: getUpstreamAccounts,
  })
  const accounts = data?.data ?? []
  const loadFailed = isError || (data !== undefined && !data.success)

  const testMutation = useMutation({
    mutationFn: testUpstreamAccount,
    onSuccess: (result) => {
      if (!result.success) return
      const detail = result.data
      if (detail?.ok) {
        const groups =
          detail.group_ids.length > 0
            ? detail.group_ids.join(', ')
            : t('None')
        toast.success(t('Test passed'), {
          description: t('Models: {{count}}, groups: {{groups}}', {
            count: detail.model_count,
            groups,
          }),
        })
      } else {
        toast.error(detail?.message || t('Test failed'))
      }
      queryClient.invalidateQueries({ queryKey: ['upstream-accounts'] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: deleteUpstreamAccount,
    onSuccess: (result) => {
      // A 204 response has no body; only an explicit failure blocks the
      // success path.
      if (result && result.success === false) return
      toast.success(t('Upstream account deleted'))
      setPendingDelete(undefined)
      queryClient.invalidateQueries({ queryKey: ['upstream-accounts'] })
    },
  })

  const openCreate = () => {
    setEditingAccount(undefined)
    setDialogOpen(true)
  }

  const openEdit = (account: UpstreamAccount) => {
    setEditingAccount(account)
    setDialogOpen(true)
  }

  let content: ReactNode
  if (isLoading) {
    content = <LoadingState className='min-h-[240px]' />
  } else if (loadFailed) {
    content = (
      <ErrorState
        title={t('Failed to load upstream accounts')}
        onRetry={() => {
          void refetch()
        }}
      />
    )
  } else if (accounts.length === 0) {
    content = (
      <EmptyState
        icon={KeyRound}
        title={t('No upstream accounts yet')}
        action={
          <Button size='sm' onClick={openCreate}>
            <Plus />
            {t('Add upstream account')}
          </Button>
        }
      />
    )
  } else {
    content = (
      <div className='flex flex-col gap-3'>
        {accounts.map((account) => (
          <UpstreamAccountCard
            key={account.id}
            account={account}
            isTesting={
              testMutation.isPending && testMutation.variables === account.id
            }
            testDisabled={testMutation.isPending}
            onTest={() => testMutation.mutate(account.id)}
            onEdit={() => openEdit(account)}
            onDelete={() => setPendingDelete(account)}
          />
        ))}
      </div>
    )
  }

  return (
    <div className='flex h-full min-h-0 flex-col gap-3'>
      {active && (
        <SettingsPageActionsPortal>
          <Button size='sm' onClick={openCreate}>
            <Plus />
            {t('Add upstream account')}
          </Button>
        </SettingsPageActionsPortal>
      )}

      <p className='text-muted-foreground shrink-0 text-sm'>
        {t(
          'Accounts are keyed by upstream host. Channels pointing to the same host reuse these credentials for probes and usage sync.'
        )}
      </p>

      <div className='min-h-0 flex-1 overflow-y-auto'>{content}</div>

      <UpstreamAccountDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        account={editingAccount}
        onSaved={() => {
          queryClient.invalidateQueries({ queryKey: ['upstream-accounts'] })
        }}
      />

      <ConfirmDialog
        open={pendingDelete !== undefined}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(undefined)
        }}
        title={t('Delete upstream account?')}
        desc={t(
          'This removes the stored credentials for {{host}}. Channels pointing to this host fall back to their channel settings.',
          { host: pendingDelete?.host ?? '' }
        )}
        destructive
        isLoading={deleteMutation.isPending}
        handleConfirm={() => {
          if (pendingDelete) deleteMutation.mutate(pendingDelete.id)
        }}
      />
    </div>
  )
}

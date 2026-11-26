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
import { Loader2, Pencil, PlugZap, Plus, Trash2 } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { SectionPageLayout } from '@/components/layout'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import {
  deleteUpstreamAccount,
  getUpstreamAccounts,
  testUpstreamAccount,
} from './api'
import type { UpstreamAccount } from './types'
import { UpstreamAccountDialog } from './upstream-account-dialog'

export function UpstreamAccounts() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingAccount, setEditingAccount] = useState<
    UpstreamAccount | undefined
  >(undefined)
  const [pendingDelete, setPendingDelete] = useState<
    UpstreamAccount | undefined
  >(undefined)

  const { data, isLoading, isError } = useQuery({
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

  let tableBody: ReactNode
  if (isLoading) {
    tableBody = (
      <TableRow>
        <TableCell colSpan={8} className='text-muted-foreground py-8 text-center'>
          {t('Loading...')}
        </TableCell>
      </TableRow>
    )
  } else if (loadFailed) {
    tableBody = (
      <TableRow>
        <TableCell colSpan={8} className='text-destructive py-8 text-center'>
          {t('Failed to load upstream accounts')}
        </TableCell>
      </TableRow>
    )
  } else if (accounts.length === 0) {
    tableBody = (
      <TableRow>
        <TableCell colSpan={8} className='text-muted-foreground py-8 text-center'>
          {t('No upstream accounts yet')}
        </TableCell>
      </TableRow>
    )
  } else {
    tableBody = accounts.map((account) => {
      const isTesting =
        testMutation.isPending && testMutation.variables === account.id
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
        <TableRow key={account.id}>
          <TableCell>
            <div className='flex flex-wrap items-center gap-1.5 font-medium'>
              <span>{account.host}</span>
              {!account.enabled && (
                <Badge variant='secondary'>{t('Disabled')}</Badge>
              )}
            </div>
            <div
              className='text-muted-foreground max-w-64 truncate text-xs'
              title={account.base_url}
            >
              {account.base_url}
            </div>
          </TableCell>
          <TableCell>
            <Badge variant='outline'>{account.kind}</Badge>
          </TableCell>
          <TableCell className='text-muted-foreground hidden md:table-cell'>
            {account.username || '—'}
          </TableCell>
          <TableCell>
            <StatusBadge
              label={account.has_password ? t('Configured') : t('Not configured')}
              variant={account.has_password ? 'success' : 'neutral'}
              copyable={false}
            />
          </TableCell>
          <TableCell className='text-muted-foreground hidden lg:table-cell'>
            {account.jk_group_id || '—'}
          </TableCell>
          <TableCell className='hidden lg:table-cell'>
            <StatusBadge
              label={tokenLabel}
              variant={tokenVariant}
              copyable={false}
            />
          </TableCell>
          <TableCell className='hidden md:table-cell'>
            {account.last_test_at > 0 ? (
              <>
                <StatusBadge
                  label={t(account.last_test_ok ? 'Test passed' : 'Test failed')}
                  variant={account.last_test_ok ? 'success' : 'danger'}
                  copyable={false}
                />
                <div
                  className='text-muted-foreground mt-1 max-w-52 truncate text-xs'
                  title={account.last_test_message}
                >
                  {new Date(account.last_test_at * 1000).toLocaleString()}
                  {account.last_test_message
                    ? ` · ${account.last_test_message}`
                    : ''}
                </div>
              </>
            ) : (
              <span className='text-muted-foreground text-xs'>
                {t('Never tested')}
              </span>
            )}
          </TableCell>
          <TableCell>
            <div className='flex items-center justify-end gap-1'>
              <Button
                size='sm'
                variant='outline'
                disabled={testMutation.isPending}
                onClick={() => testMutation.mutate(account.id)}
              >
                {isTesting ? (
                  <Loader2 className='animate-spin' />
                ) : (
                  <PlugZap />
                )}
                <span className='hidden sm:inline'>{t('Test connection')}</span>
                <span className='sr-only sm:hidden'>{t('Test connection')}</span>
              </Button>
              <Button
                size='icon-sm'
                variant='ghost'
                aria-label={t('Edit')}
                onClick={() => openEdit(account)}
              >
                <Pencil />
              </Button>
              <Button
                size='icon-sm'
                variant='ghost'
                aria-label={t('Delete')}
                onClick={() => setPendingDelete(account)}
              >
                <Trash2 />
              </Button>
            </div>
          </TableCell>
        </TableRow>
      )
    })
  }

  return (
    <>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>
          {t('Upstream accounts')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button onClick={openCreate}>
            <Plus />
            {t('Add upstream account')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <Card className='h-full min-h-0 gap-0 py-0'>
            <CardContent className='flex min-h-0 flex-1 flex-col p-3 sm:p-4'>
              <p className='text-muted-foreground mb-3 text-sm'>
                {t(
                  'Accounts are keyed by upstream host. Channels pointing to the same host reuse these credentials for probes and usage sync.'
                )}
              </p>
              <div className='min-h-0 flex-1 overflow-auto rounded-md border'>
                <Table>
                  <TableHeader className='bg-muted/50'>
                    <TableRow>
                      <TableHead>{t('Host')}</TableHead>
                      <TableHead>{t('Type')}</TableHead>
                      <TableHead className='hidden md:table-cell'>
                        {t('Username')}
                      </TableHead>
                      <TableHead>{t('Password')}</TableHead>
                      <TableHead className='hidden lg:table-cell'>
                        {t('JK Group ID')}
                      </TableHead>
                      <TableHead className='hidden lg:table-cell'>
                        {t('Token Status')}
                      </TableHead>
                      <TableHead className='hidden md:table-cell'>
                        {t('Last test')}
                      </TableHead>
                      <TableHead className='text-right'>
                        {t('Actions')}
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>{tableBody}</TableBody>
                </Table>
              </div>
            </CardContent>
          </Card>
        </SectionPageLayout.Content>
      </SectionPageLayout>

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
    </>
  )
}

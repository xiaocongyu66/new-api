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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'

import { createUpstreamAccount, updateUpstreamAccount } from './api'
import type {
  UpstreamAccount,
  UpstreamAccountKind,
  UpstreamAccountPayload,
} from './types'

type UpstreamAccountFormValues = {
  base_url: string
  kind: UpstreamAccountKind
  username: string
  password: string
  access_token: string
  refresh_token: string
  jk_group_id: string
  enabled: boolean
}

const DEFAULT_VALUES: UpstreamAccountFormValues = {
  base_url: '',
  kind: 'sub2api',
  username: '',
  password: '',
  access_token: '',
  refresh_token: '',
  jk_group_id: '',
  enabled: true,
}

function buildFormSchema(t: TFunction) {
  return z.object({
    base_url: z
      .string()
      .trim()
      .min(1, t('Base URL is required'))
      .refine((value) => {
        try {
          const url = new URL(value)
          return url.protocol === 'https:' || url.protocol === 'http:'
        } catch {
          return false
        }
      }, t('Enter a valid HTTP(S) URL')),
    kind: z.enum(['sub2api', 'newapi']),
    username: z.string(),
    password: z.string(),
    access_token: z.string(),
    refresh_token: z.string(),
    jk_group_id: z.string(),
    enabled: z.boolean(),
  })
}

function deriveHost(baseUrl: string): string {
  try {
    return new URL(baseUrl.trim()).hostname.toLowerCase()
  } catch {
    return ''
  }
}

function accountToFormValues(account: UpstreamAccount): UpstreamAccountFormValues {
  return {
    base_url: account.base_url,
    kind: account.kind,
    username: account.username,
    // Secrets are never returned by the API. Leaving them blank keeps the
    // stored value; typing a new value replaces it.
    password: '',
    access_token: '',
    refresh_token: '',
    jk_group_id: account.jk_group_id,
    enabled: account.enabled,
  }
}

type UpstreamAccountDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  account?: UpstreamAccount
  onSaved: () => void
}

export function UpstreamAccountDialog({
  open,
  onOpenChange,
  account,
  onSaved,
}: UpstreamAccountDialogProps) {
  const { t } = useTranslation()
  const isUpdate = !!account

  const form = useForm<UpstreamAccountFormValues>({
    resolver: zodResolver(buildFormSchema(t)),
    defaultValues: DEFAULT_VALUES,
  })

  useEffect(() => {
    if (!open) return
    form.reset(account ? accountToFormValues(account) : DEFAULT_VALUES)
  }, [open, account, form])

  const createMutation = useMutation({
    mutationFn: createUpstreamAccount,
    onSuccess: (result) => {
      if (!result.success) return
      toast.success(t('Upstream account created'))
      onOpenChange(false)
      onSaved()
    },
  })

  const updateMutation = useMutation({
    mutationFn: updateUpstreamAccount,
    onSuccess: (result) => {
      if (!result.success) return
      toast.success(t('Upstream account updated'))
      onOpenChange(false)
      onSaved()
    },
  })

  const isPending = createMutation.isPending || updateMutation.isPending

  const onSubmit = (values: UpstreamAccountFormValues) => {
    const payload: UpstreamAccountPayload = {
      base_url: values.base_url.trim(),
      kind: values.kind,
      username: values.username.trim(),
      password: values.password,
      jk_group_id: values.jk_group_id.trim(),
      access_token: values.access_token,
      refresh_token: values.refresh_token,
      enabled: values.enabled,
    }

    if (account) {
      updateMutation.mutate({ ...payload, id: account.id })
    } else {
      createMutation.mutate(payload)
    }
  }

  const derivedHost = deriveHost(form.watch('base_url'))

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>
            {t(isUpdate ? 'Edit upstream account' : 'Add upstream account')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Channels that point to this host reuse the account credentials for upstream probes and usage sync.'
            )}
          </DialogDescription>
        </DialogHeader>

        <Form {...form}>
          <form
            onSubmit={form.handleSubmit(onSubmit)}
            className='flex flex-col gap-4'
          >
            <fieldset
              disabled={isPending}
              className='m-0 flex min-w-0 flex-col gap-4 border-0 p-0'
            >
              <FormField
                control={form.control}
                name='base_url'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Base URL')}</FormLabel>
                    <FormControl>
                      <Input
                        autoComplete='off'
                        placeholder='https://example.com'
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {derivedHost
                        ? t(
                            'Host: {{host}} — derived from the base URL and normalized by the server.',
                            { host: derivedHost }
                          )
                        : t(
                            'The host is derived from the base URL and normalized by the server.'
                          )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <div className='grid gap-4 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='kind'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Type')}</FormLabel>
                      <Select
                        items={[
                          { value: 'sub2api', label: 'sub2api' },
                          { value: 'newapi', label: 'newapi' },
                        ]}
                        value={field.value}
                        onValueChange={field.onChange}
                      >
                        <FormControl>
                          <SelectTrigger className='w-full'>
                            <SelectValue />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectItem value='sub2api'>sub2api</SelectItem>
                          <SelectItem value='newapi'>newapi</SelectItem>
                        </SelectContent>
                      </Select>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='username'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Username')}</FormLabel>
                      <FormControl>
                        <Input autoComplete='off' {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>

              <FormField
                control={form.control}
                name='password'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Password')}</FormLabel>
                    <FormControl>
                      <Input
                        type='password'
                        autoComplete='new-password'
                        placeholder={account?.password_hint || undefined}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        isUpdate
                          ? 'Leave blank to keep the current password'
                          : 'Stored securely and never displayed after saving.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='jk_group_id'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('JK Group ID')}</FormLabel>
                    <FormControl>
                      <Input inputMode='numeric' autoComplete='off' {...field} />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Group ID in the public /jk/status.json monitor. Leave empty to infer it from the channel models.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='access_token'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Access Token')}</FormLabel>
                    <FormControl>
                      <Input
                        type='password'
                        autoComplete='off'
                        placeholder={account?.access_token_hint || undefined}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        isUpdate
                          ? 'Leave blank to keep the current access token'
                          : 'Optional. Used directly for upstream API calls when no password is set.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='refresh_token'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Refresh Token')}</FormLabel>
                    <FormControl>
                      <Input
                        type='password'
                        autoComplete='off'
                        placeholder={account?.refresh_token_hint || undefined}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        isUpdate
                          ? 'Leave blank to keep the current refresh token'
                          : 'Optional. Renews the access token without signing in again.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='enabled'
                render={({ field }) => (
                  <FormItem className='flex items-center justify-between gap-4'>
                    <div className='space-y-0.5'>
                      <FormLabel>{t('Enabled')}</FormLabel>
                      <FormDescription>
                        {t(
                          'Disabled accounts stay in the list but are skipped by probes and usage sync.'
                        )}
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </FormItem>
                )}
              />
            </fieldset>

            <DialogFooter>
              <Button
                type='button'
                variant='outline'
                disabled={isPending}
                onClick={() => onOpenChange(false)}
              >
                {t('Cancel')}
              </Button>
              <Button type='submit' disabled={isPending}>
                {t(isPending ? 'Saving...' : 'Save')}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}
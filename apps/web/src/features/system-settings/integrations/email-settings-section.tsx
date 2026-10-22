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
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

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
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { MailDeliverySection } from './mail-delivery-section'
import { SMTPPoolEditor } from './smtp-pool-editor'
import {
  parsePool,
  serializePool,
  type SMTPPoolAccount,
} from './smtp-pool-model'

const createEmailSchema = (t: (key: string) => string) =>
  z.object({
    SMTPServer: z.string(),
    SMTPPort: z.string().refine((value) => {
      const trimmed = value.trim()
      if (!trimmed) return true
      return /^\d+$/.test(trimmed)
    }, t('Port must be a positive integer')),
    SMTPAccount: z.string(),
    SMTPFrom: z.string().refine((value) => {
      const trimmed = value.trim()
      if (!trimmed) return true
      return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(trimmed)
    }, t('Enter a valid email or leave blank')),
    SMTPToken: z.string(),
    SMTPSSLEnabled: z.boolean(),
    SMTPStartTLSEnabled: z.boolean(),
    SMTPInsecureSkipVerify: z.boolean(),
    SMTPForceAuthLogin: z.boolean(),
    SMTPAccountPool: z.string().refine((value) => {
      const trimmed = value.trim()
      if (!trimmed) return true
      let parsed: unknown
      try {
        parsed = JSON.parse(trimmed)
      } catch {
        return false
      }
      if (!Array.isArray(parsed)) return false
      return parsed.every((entry) => {
        if (typeof entry !== 'object' || entry === null) return false
        const account = entry as Record<string, unknown>
        const hasText = (key: string) =>
          typeof account[key] === 'string' &&
          (account[key] as string).trim() !== ''
        const optionalNonNegativeNumber = (key: string) =>
          account[key] === undefined ||
          (typeof account[key] === 'number' && account[key] >= 0)
        return (
          hasText('server') &&
          hasText('account') &&
          typeof account.port === 'number' &&
          account.port > 0 &&
          optionalNonNegativeNumber('dailyLimit') &&
          optionalNonNegativeNumber('priority')
        )
      })
    }, t('Enter a valid JSON array of SMTP accounts')),
  })

type EmailFormValues = z.infer<ReturnType<typeof createEmailSchema>>

type EmailSettingsSectionProps = {
  defaultValues: EmailFormValues
}

type SmtpSecurityMode = 'none' | 'ssl_tls' | 'starttls'

function getSmtpSecurityMode(values: {
  SMTPSSLEnabled: boolean
  SMTPStartTLSEnabled: boolean
}): SmtpSecurityMode {
  if (values.SMTPSSLEnabled) return 'ssl_tls'
  if (values.SMTPStartTLSEnabled) return 'starttls'
  return 'none'
}

export function EmailSettingsSection({
  defaultValues,
}: EmailSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const emailSchema = createEmailSchema(t)
  const [activeTab, setActiveTab] = useState('config')
  const [poolAccounts, setPoolAccounts] = useState<SMTPPoolAccount[]>(() =>
    parsePool(defaultValues.SMTPAccountPool)
  )

  // Reload the editable rows whenever the stored value changes from outside
  // (another admin saved it, or a reset happened).
  useEffect(() => {
    setPoolAccounts(parsePool(defaultValues.SMTPAccountPool))
  }, [defaultValues.SMTPAccountPool])

  const form = useForm<EmailFormValues>({
    resolver: zodResolver(emailSchema),
    defaultValues,
  })

  useResetForm(form, defaultValues)

  const onSubmit = async (values: EmailFormValues) => {
    const securityMode = getSmtpSecurityMode(values)
    const sanitized = {
      SMTPServer: values.SMTPServer.trim(),
      SMTPPort: values.SMTPPort.trim(),
      SMTPAccount: values.SMTPAccount.trim(),
      SMTPFrom: values.SMTPFrom.trim(),
      SMTPToken: values.SMTPToken.trim(),
      SMTPSSLEnabled: securityMode === 'ssl_tls',
      SMTPStartTLSEnabled: securityMode === 'starttls',
      SMTPInsecureSkipVerify: values.SMTPInsecureSkipVerify,
      SMTPForceAuthLogin: values.SMTPForceAuthLogin,
    }

    const initial = {
      SMTPServer: defaultValues.SMTPServer.trim(),
      SMTPPort: defaultValues.SMTPPort.trim(),
      SMTPAccount: defaultValues.SMTPAccount.trim(),
      SMTPFrom: defaultValues.SMTPFrom.trim(),
      SMTPToken: defaultValues.SMTPToken.trim(),
      SMTPSSLEnabled: defaultValues.SMTPSSLEnabled,
      SMTPStartTLSEnabled: defaultValues.SMTPStartTLSEnabled,
      SMTPInsecureSkipVerify: defaultValues.SMTPInsecureSkipVerify,
      SMTPForceAuthLogin: defaultValues.SMTPForceAuthLogin,
    }

    const updates: Array<{ key: string; value: string | boolean }> = []

    if (sanitized.SMTPServer !== initial.SMTPServer) {
      updates.push({ key: 'SMTPServer', value: sanitized.SMTPServer })
    }

    if (sanitized.SMTPPort !== initial.SMTPPort) {
      updates.push({ key: 'SMTPPort', value: sanitized.SMTPPort })
    }

    if (sanitized.SMTPAccount !== initial.SMTPAccount) {
      updates.push({ key: 'SMTPAccount', value: sanitized.SMTPAccount })
    }

    if (sanitized.SMTPFrom !== initial.SMTPFrom) {
      updates.push({ key: 'SMTPFrom', value: sanitized.SMTPFrom })
    }

    if (sanitized.SMTPToken && sanitized.SMTPToken !== initial.SMTPToken) {
      updates.push({ key: 'SMTPToken', value: sanitized.SMTPToken })
    }

    if (sanitized.SMTPSSLEnabled !== initial.SMTPSSLEnabled) {
      updates.push({
        key: 'SMTPSSLEnabled',
        value: sanitized.SMTPSSLEnabled,
      })
    }

    if (sanitized.SMTPStartTLSEnabled !== initial.SMTPStartTLSEnabled) {
      updates.push({
        key: 'SMTPStartTLSEnabled',
        value: sanitized.SMTPStartTLSEnabled,
      })
    }

    if (sanitized.SMTPInsecureSkipVerify !== initial.SMTPInsecureSkipVerify) {
      updates.push({
        key: 'SMTPInsecureSkipVerify',
        value: sanitized.SMTPInsecureSkipVerify,
      })
    }

    if (sanitized.SMTPForceAuthLogin !== initial.SMTPForceAuthLogin) {
      updates.push({
        key: 'SMTPForceAuthLogin',
        value: sanitized.SMTPForceAuthLogin,
      })
    }

    // The pool is compared through the row editor, not the form field: the
    // rows are the source of truth and the JSON field is only their serialized
    // form. Both sides are serialized against the same stored pool, so a blank
    // token (which means "keep the stored credential") never counts as a change.
    const storedPool = parsePool(defaultValues.SMTPAccountPool)
    const serializedPool = serializePool(poolAccounts, storedPool)
    const poolChanged = serializedPool !== serializePool(storedPool, storedPool)

    if (poolChanged) {
      updates.push({ key: 'SMTPAccountPool', value: serializedPool })
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
  }

  return (
    <SettingsSection title={t('SMTP Email')}>
      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value='config'>{t('Configuration')}</TabsTrigger>
          <TabsTrigger value='status'>{t('Delivery status')}</TabsTrigger>
        </TabsList>

        <TabsContent value='config'>
          <Form {...form}>
            <SettingsForm
              onSubmit={form.handleSubmit(onSubmit)}
              autoComplete='off'
            >
              <SettingsPageFormActions
                onSave={form.handleSubmit(onSubmit)}
                isSaving={updateOption.isPending}
                saveLabel='Save SMTP settings'
              />
              <FormField
                control={form.control}
                name='SMTPServer'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('SMTP Host')}</FormLabel>
                    <FormControl>
                      <Input
                        autoComplete='off'
                        placeholder={t('smtp.example.com')}
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Hostname or IP of your SMTP provider')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <div className='grid gap-6 md:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='SMTPPort'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Port')}</FormLabel>
                      <FormControl>
                        <Input
                          autoComplete='off'
                          type='number'
                          placeholder='587'
                          {...field}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Common ports include 25, 465, and 587')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormItem>
                  <FormLabel>{t('SMTP encryption')}</FormLabel>
                  <FormControl>
                    <RadioGroup
                      value={getSmtpSecurityMode({
                        SMTPSSLEnabled: form.watch('SMTPSSLEnabled'),
                        SMTPStartTLSEnabled: form.watch('SMTPStartTLSEnabled'),
                      })}
                      onValueChange={(value) => {
                        const mode = value as SmtpSecurityMode
                        form.setValue('SMTPSSLEnabled', mode === 'ssl_tls', {
                          shouldDirty: true,
                        })
                        form.setValue(
                          'SMTPStartTLSEnabled',
                          mode === 'starttls',
                          {
                            shouldDirty: true,
                          }
                        )
                      }}
                      className='gap-3'
                    >
                      <div className='flex items-center gap-2'>
                        <RadioGroupItem value='none' id='smtp-security-none' />
                        <Label
                          htmlFor='smtp-security-none'
                          className='cursor-pointer font-normal'
                        >
                          {t('No encryption')}
                        </Label>
                      </div>
                      <div className='flex items-center gap-2'>
                        <RadioGroupItem
                          value='ssl_tls'
                          id='smtp-security-ssl-tls'
                        />
                        <Label
                          htmlFor='smtp-security-ssl-tls'
                          className='cursor-pointer font-normal'
                        >
                          {t('SSL/TLS')}
                        </Label>
                      </div>
                      <div className='flex items-center gap-2'>
                        <RadioGroupItem
                          value='starttls'
                          id='smtp-security-starttls'
                        />
                        <Label
                          htmlFor='smtp-security-starttls'
                          className='cursor-pointer font-normal'
                        >
                          {t('STARTTLS')}
                        </Label>
                      </div>
                    </RadioGroup>
                  </FormControl>
                  <FormDescription>
                    {t('Choose one SMTP transport security mode')}
                  </FormDescription>
                </FormItem>

                <FormField
                  control={form.control}
                  name='SMTPInsecureSkipVerify'
                  render={({ field }) => (
                    <SettingsSwitchItem>
                      <SettingsSwitchContent>
                        <FormLabel>
                          {t('Skip SMTP TLS certificate verification')}
                        </FormLabel>
                        <FormDescription>
                          {t(
                            'Allow self-signed or hostname-mismatched SMTP certificates'
                          )}
                        </FormDescription>
                      </SettingsSwitchContent>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </SettingsSwitchItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='SMTPForceAuthLogin'
                  render={({ field }) => (
                    <SettingsSwitchItem>
                      <SettingsSwitchContent>
                        <FormLabel>{t('Force AUTH LOGIN')}</FormLabel>
                        <FormDescription>
                          {t(
                            'Force SMTP authentication using AUTH LOGIN method'
                          )}
                        </FormDescription>
                      </SettingsSwitchContent>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </SettingsSwitchItem>
                  )}
                />
              </div>

              <FormField
                control={form.control}
                name='SMTPAccount'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Username')}</FormLabel>
                    <FormControl>
                      <Input
                        autoComplete='off'
                        placeholder={t('noreply@example.com')}
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Account used when authenticating with the SMTP server'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='SMTPFrom'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('From Address')}</FormLabel>
                    <FormControl>
                      <Input
                        autoComplete='off'
                        placeholder={t('New API &lt;noreply@example.com&gt;')}
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Display name and email used in outgoing messages')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='SMTPToken'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Password / Access Token')}</FormLabel>
                    <FormControl>
                      <Input
                        autoComplete='off'
                        type='password'
                        placeholder={t('Enter new token to update')}
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Leave blank to keep the existing credential')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='SMTPAccountPool'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('SMTP account pool')}</FormLabel>
                    <FormControl>
                      <SMTPPoolEditor
                        accounts={poolAccounts}
                        onChange={(accounts) => {
                          setPoolAccounts(accounts)
                          field.onChange(
                            serializePool(
                              accounts,
                              parsePool(defaultValues.SMTPAccountPool)
                            )
                          )
                        }}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Accounts are tried in priority order — the lowest value first — and the higher tiers only take over once the lower ones run out of their daily quota. Leave the pool empty to send through the single account above.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SettingsForm>
          </Form>
        </TabsContent>

        <TabsContent value='status'>
          <MailDeliverySection />
        </TabsContent>
      </Tabs>
    </SettingsSection>
  )
}

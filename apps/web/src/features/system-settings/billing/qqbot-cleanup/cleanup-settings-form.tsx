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
import { useMemo } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'
import { toast } from 'sonner'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import { SettingsForm } from '../../components/settings-form-layout'
import { SettingsPageFormActions } from '../../components/settings-page-context'
import { useUpdateOption } from '../../hooks/use-update-option'
import { useResetForm } from '../../hooks/use-reset-form'

// 与 apps/api/internal/billing/qqbot_setting.go 的 Cleanup* 字段一一对应。
// 沿用 QQ 机器人主表单的嵌套 schema 约定（react-hook-form 把带点的字段名当路径）。
const cleanupSchema = z.object({
  qq_bot_setting: z.object({
    cleanup_enabled: z.boolean(),
    cleanup_groups: z.string(),
    napcat_onebot_http_address: z.string(),
    napcat_onebot_access_token: z.string(),
    cleanup_inactive_days: z.coerce.number().int().min(1),
    cleanup_grace_days: z.coerce.number().int().min(0),
    cleanup_warning_template: z.string(),
    cleanup_kick_min_seconds: z.coerce.number().int().min(0),
    cleanup_kick_max_seconds: z.coerce.number().int().min(0),
    cleanup_batch_size: z.coerce.number().int().min(0),
    cleanup_dry_run: z.boolean(),
    cleanup_exempt_bound_users: z.boolean(),
    cleanup_warn_hours: z.coerce.number().int().min(0),
    cleanup_group_numbers: z.string(),
  }),
})

type CleanupFormValues = z.infer<typeof cleanupSchema>

type CleanupSettingsFormProps = {
  /** 扁平 option map，键为 'qq_bot_setting.cleanup_xxx' */
  defaultValues: Record<string, string | number | boolean>
}

function unflattenDefaults(
  flat: CleanupSettingsFormProps['defaultValues']
): CleanupFormValues {
  const out: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(flat)) {
    const short = key.replace('qq_bot_setting.', '')
    out[short] = value
  }
  return { qq_bot_setting: out } as CleanupFormValues
}

export function CleanupSettingsForm({
  defaultValues,
}: CleanupSettingsFormProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const formDefaults = useMemo(
    () => unflattenDefaults(defaultValues),
    [defaultValues]
  )

  const form = useForm<CleanupFormValues>({
    resolver: zodResolver(cleanupSchema) as unknown as Resolver<CleanupFormValues>,
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)

  const { isDirty } = form.formState

  const onSubmit = async (data: CleanupFormValues) => {
    const defaults = formDefaults.qq_bot_setting as Record<
      string,
      string | number | boolean
    >
    const updates = Object.entries(data.qq_bot_setting)
      .filter(([key, value]) => value !== defaults[key])
      .map(([key, value]) => ({
        key: `qq_bot_setting.${key}`,
        value: String(value),
      }))
    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }
    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
    toast.success(t('Setting updated successfully'))
  }

  return (
    <Form {...form}>
      <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
        <SettingsPageFormActions
          onSave={form.handleSubmit(onSubmit)}
          isSaving={updateOption.isPending}
          isSaveDisabled={!isDirty}
          saveLabel={t('Save Changes')}
        />

        <FormField
          control={form.control}
          name='qq_bot_setting.cleanup_enabled'
          render={({ field }) => (
            <FormItem>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
              <div className='space-y-0.5'>
                <FormLabel>{t('Enable inactive member cleanup')}</FormLabel>
                <FormDescription>
                  {t(
                    'The official bot posts a public @ warning; a NapCat instance signed in with a veteran account performs the actual removal'
                  )}
                </FormDescription>
              </div>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='qq_bot_setting.cleanup_groups'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Cleanup enabled groups')}</FormLabel>
              <FormControl>
                <Input placeholder='GROUP_OPENID_A,GROUP_OPENID_B' {...field} />
              </FormControl>
              <FormDescription>
                {t(
                  'Comma separated group_openid allowlist. Only these groups are scanned'
                )}
              </FormDescription>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='qq_bot_setting.cleanup_group_numbers'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Group number mapping (JSON)')}</FormLabel>
              <FormControl>
                <Input
                  placeholder='{"GROUP_OPENID_A": 123456789}'
                  {...field}
                />
              </FormControl>
              <FormDescription>
                {t(
                  'The official bot uses group_openid while NapCat uses the real group number. Kicks are impossible without this mapping; warnings still go out'
                )}
              </FormDescription>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='qq_bot_setting.napcat_onebot_http_address'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('NapCat OneBot HTTP address')}</FormLabel>
              <FormControl>
                <Input placeholder='http://127.0.0.1:3000' {...field} />
              </FormControl>
              <FormDescription>
                {t(
                  'OneBot v11 HTTP endpoint of the NapCat instance that holds the veteran account'
                )}
              </FormDescription>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='qq_bot_setting.napcat_onebot_access_token'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('NapCat access token')}</FormLabel>
              <FormControl>
                <Input type='password' autoComplete='off' {...field} />
              </FormControl>
              <FormDescription>
                {t(
                  'Shared by the NapCat webhook (for @ bridging) and outbound calls. Requests are rejected when left empty'
                )}
              </FormDescription>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='qq_bot_setting.cleanup_inactive_days'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Inactive threshold (days)')}</FormLabel>
              <FormControl>
                <Input type='number' min={1} {...field} />
              </FormControl>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='qq_bot_setting.cleanup_grace_days'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Grace period (days)')}</FormLabel>
              <FormControl>
                <Input type='number' min={0} {...field} />
              </FormControl>
              <FormDescription>
                {t(
                  'Members are only removed after this many days pass from the warning without any activity'
                )}
              </FormDescription>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='qq_bot_setting.cleanup_warn_hours'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Warn cooldown (hours)')}</FormLabel>
              <FormControl>
                <Input type='number' min={0} {...field} />
              </FormControl>
              <FormDescription>
                {t('Do not @ the same member twice within this window')}
              </FormDescription>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='qq_bot_setting.cleanup_warning_template'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Warning template')}</FormLabel>
              <FormControl>
                <Input {...field} />
              </FormControl>
              <FormDescription>
                {t(
                  'Placeholders: {@} {days} {grace days}. {@} becomes the official <qqbot-at-user> mention'
                )}
              </FormDescription>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='qq_bot_setting.cleanup_kick_min_seconds'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Kick interval min (seconds)')}</FormLabel>
              <FormControl>
                <Input type='number' min={0} {...field} />
              </FormControl>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='qq_bot_setting.cleanup_kick_max_seconds'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Kick interval max (seconds)')}</FormLabel>
              <FormControl>
                <Input type='number' min={0} {...field} />
              </FormControl>
              <FormDescription>
                {t(
                  'Random sleep between removals so the veteran account looks human'
                )}
              </FormDescription>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='qq_bot_setting.cleanup_batch_size'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Batch size per run')}</FormLabel>
              <FormControl>
                <Input type='number' min={0} {...field} />
              </FormControl>
              <FormDescription>
                {t(
                  'Caps both warnings and removals per scan. 0 or below means unlimited'
                )}
              </FormDescription>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='qq_bot_setting.cleanup_exempt_bound_users'
          render={({ field }) => (
            <FormItem>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
              <div className='space-y-0.5'>
                <FormLabel>{t('Exempt bound site users')}</FormLabel>
                <FormDescription>
                  {t(
                    'Members linked to a site account are paying or active users; skipping them avoids costly mistakes'
                  )}
                </FormDescription>
              </div>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='qq_bot_setting.cleanup_dry_run'
          render={({ field }) => (
            <FormItem>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
              <div className='space-y-0.5'>
                <FormLabel>{t('Dry run')}</FormLabel>
                <FormDescription>
                  {t(
                    'Only identify candidates and send warnings; never remove anyone. Use it to verify the list before going live'
                  )}
                </FormDescription>
              </div>
            </FormItem>
          )}
        />
      </SettingsForm>
    </Form>
  )
}

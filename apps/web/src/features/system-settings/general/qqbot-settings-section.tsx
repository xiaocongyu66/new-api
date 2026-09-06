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
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'

// Keys mirror QQBotSetting in apps/api/internal/billing/qqbot_setting.go.
// Quota amounts are raw quota units, matching the backend defaults.
const qqbotSchema = z.object({
  'qq_bot_setting.app_id': z.string(),
  'qq_bot_setting.app_secret': z.string(),
  'qq_bot_setting.qq_checkin_enabled': z.boolean(),
  'qq_bot_setting.web_checkin_enabled': z.boolean(),
  'qq_bot_setting.single_platform_only': z.boolean(),
  'qq_bot_setting.min_quota': z.coerce.number().int().min(0),
  'qq_bot_setting.max_quota': z.coerce.number().int().min(0),
  'qq_bot_setting.checkin_disabled_groups': z.string(),
  'qq_bot_setting.notify_template': z.string(),
  'qq_bot_setting.auto_approve_enabled': z.boolean(),
  'qq_bot_setting.auto_approve_keyword': z.string(),
  'qq_bot_setting.drop_enabled': z.boolean(),
  'qq_bot_setting.drop_groups': z.string(),
  'qq_bot_setting.drop_min_messages': z.coerce.number().int().min(1),
  'qq_bot_setting.drop_max_messages': z.coerce.number().int().min(1),
  'qq_bot_setting.drop_min_quota': z.coerce.number().int().min(0),
  'qq_bot_setting.drop_max_quota': z.coerce.number().int().min(0),
  'qq_bot_setting.drop_daily_limit': z.coerce.number().int(),
  'qq_bot_setting.drop_template': z.string(),
  'qq_bot_setting.transfer_enabled': z.boolean(),
  'qq_bot_setting.transfer_disabled_groups': z.string(),
  'qq_bot_setting.transfer_daily_limit': z.coerce.number().int(),
  'qq_bot_setting.transfer_min_amount': z.coerce.number().int().min(0),
  'qq_bot_setting.transfer_max_amount': z.coerce.number().int(),
  'qq_bot_setting.transfer_fee_brackets': z.string(),
  'qq_bot_setting.red_packet_enabled': z.boolean(),
  'qq_bot_setting.red_packet_disabled_groups': z.string(),
  'qq_bot_setting.red_packet_daily_limit': z.coerce.number().int(),
  'qq_bot_setting.red_packet_min_amount': z.coerce.number().int().min(0),
  'qq_bot_setting.red_packet_max_amount': z.coerce.number().int(),
  'qq_bot_setting.red_packet_default_count': z.coerce.number().int().min(1),
  'qq_bot_setting.red_packet_max_count': z.coerce.number().int().min(1),
  'qq_bot_setting.red_packet_expire_seconds': z.coerce.number().int().min(1),
  'qq_bot_setting.red_packet_allow_own_grab': z.boolean(),
})

type QQBotFormValues = z.infer<typeof qqbotSchema>

type QQBotSettingsSectionProps = {
  defaultValues: QQBotFormValues
}

export function QQBotSettingsSection({
  defaultValues,
}: QQBotSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const form = useForm({
    resolver: zodResolver(qqbotSchema),
    defaultValues,
  })

  useResetForm(form, defaultValues)

  const onSubmit = async (data: QQBotFormValues) => {
    const updates = Object.entries(data).filter(
      ([key, value]) => value !== defaultValues[key as keyof QQBotFormValues]
    )
    for (const [key, value] of updates) {
      await updateOption.mutateAsync({ key, value })
    }
  }

  return (
    <SettingsSection title={t('QQ Bot')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.app_id'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('QQ AppID')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.app_secret'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('QQ AppSecret')}</FormLabel>
                <FormControl>
                  <Input type='password' autoComplete='off' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Webhook callbacks are rejected until a secret is set, because signatures cannot be verified without it'
                  )}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.qq_checkin_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable QQ check-in')}</FormLabel>
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
            name='qq_bot_setting.web_checkin_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable web check-in')}</FormLabel>
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
            name='qq_bot_setting.single_platform_only'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Single platform check-in only')}</FormLabel>
                  <FormDescription>
                    {t(
                      'QQ and web share one daily reward: checking in on either counts for the day'
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
            name='qq_bot_setting.min_quota'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('QQ check-in minimum quota')}</FormLabel>
                <FormControl>
                  <Input type='number' min={0} {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.max_quota'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('QQ check-in maximum quota')}</FormLabel>
                <FormControl>
                  <Input type='number' min={0} {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.notify_template'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Check-in notice template')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
                <FormDescription>
                  {t('Placeholders: {货币} {金额}')}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.checkin_disabled_groups'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Check-in disabled groups')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Comma separated group_openid blocklist; other groups are unaffected'
                  )}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.auto_approve_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Auto-approve join requests')}</FormLabel>
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
            name='qq_bot_setting.auto_approve_keyword'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Join request keyword')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.drop_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable group message drops')}</FormLabel>
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
            name='qq_bot_setting.drop_groups'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Drop enabled groups')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Comma separated group_openid allowlist; admins can register a group in-chat instead'
                  )}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.drop_min_messages'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Drop minimum messages')}</FormLabel>
                <FormControl>
                  <Input type='number' min={1} {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.drop_max_messages'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Drop maximum messages')}</FormLabel>
                <FormControl>
                  <Input type='number' min={1} {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.drop_min_quota'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Drop minimum quota')}</FormLabel>
                <FormControl>
                  <Input type='number' min={0} {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.drop_max_quota'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Drop maximum quota')}</FormLabel>
                <FormControl>
                  <Input type='number' min={0} {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.drop_daily_limit'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Drop daily limit per user')}</FormLabel>
                <FormControl>
                  <Input type='number' {...field} />
                </FormControl>
                <FormDescription>
                  {t('0 or below means unlimited')}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.drop_template'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Drop message template')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
                <FormDescription>
                  {t('Placeholders: {@} {金额} {货币} {单位} {余额}')}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.transfer_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable in-group transfers')}</FormLabel>
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
            name='qq_bot_setting.transfer_disabled_groups'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Transfer disabled groups')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.transfer_daily_limit'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Transfer daily limit per user')}</FormLabel>
                <FormControl>
                  <Input type='number' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Both sender and receiver consume one, so relaying through alts cannot bypass it'
                  )}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.transfer_min_amount'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Transfer minimum amount')}</FormLabel>
                <FormControl>
                  <Input type='number' min={0} {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.transfer_max_amount'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Transfer maximum amount')}</FormLabel>
                <FormControl>
                  <Input type='number' {...field} />
                </FormControl>
                <FormDescription>
                  {t('0 or below means unlimited')}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.transfer_fee_brackets'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Transfer fee brackets (JSON)')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Progressive brackets like [{"up_to":1,"rate":0.03},{"up_to":0,"rate":0.18}]; empty uses the built-in defaults'
                  )}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.red_packet_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable group red packets')}</FormLabel>
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
            name='qq_bot_setting.red_packet_disabled_groups'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Red packet disabled groups')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.red_packet_daily_limit'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Red packets per user per day')}</FormLabel>
                <FormControl>
                  <Input type='number' {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.red_packet_min_amount'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Red packet minimum total')}</FormLabel>
                <FormControl>
                  <Input type='number' min={0} {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.red_packet_max_amount'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Red packet maximum total')}</FormLabel>
                <FormControl>
                  <Input type='number' {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.red_packet_default_count'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Red packet default share count')}</FormLabel>
                <FormControl>
                  <Input type='number' min={1} {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.red_packet_max_count'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Red packet maximum share count')}</FormLabel>
                <FormControl>
                  <Input type='number' min={1} {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.red_packet_expire_seconds'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Red packet expiry (seconds)')}</FormLabel>
                <FormControl>
                  <Input type='number' min={1} {...field} />
                </FormControl>
                <FormDescription>
                  {t('Unclaimed remainder is refunded to the sender on expiry')}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.red_packet_allow_own_grab'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Allow grabbing your own red packet')}</FormLabel>
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
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}

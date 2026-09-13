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

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { useResetForm } from '../hooks/use-reset-form'

// Keys mirror QQBotSetting in apps/api/internal/billing/qqbot_setting.go.
// Quota amounts are raw quota units, matching the backend defaults.
//
// The schema is NESTED on purpose: react-hook-form treats dots in field names
// as paths (`qq_bot_setting.min_quota` writes formValues.qq_bot_setting.min_quota),
// so a flat schema keyed by dotted strings never sees the user's edits. This section
// unflattens incoming settings ('qq_bot_setting.x' -> {qq_bot_setting: {x}}) and
// flattens submitted values back to the option keys for the API.
const qqbotSchema = z.object({
  qq_bot_setting: z.object({
    app_id: z.string(),
    app_secret: z.string(),
    qq_checkin_enabled: z.boolean(),
    web_checkin_enabled: z.boolean(),
    single_platform_only: z.boolean(),
    min_quota: z.coerce.number().int().min(0),
    max_quota: z.coerce.number().int().min(0),
    checkin_disabled_groups: z.string(),
    notify_template: z.string(),
    auto_approve_enabled: z.boolean(),
    auto_approve_keyword: z.string(),
    drop_enabled: z.boolean(),
    drop_groups: z.string(),
    drop_min_messages: z.coerce.number().int().min(1),
    drop_max_messages: z.coerce.number().int().min(1),
    drop_min_quota: z.coerce.number().int().min(0),
    drop_max_quota: z.coerce.number().int().min(0),
    drop_daily_limit: z.coerce.number().int(),
    drop_template: z.string(),
    drop_balance_anchor: z.coerce.number().int().min(0),
    drop_daily_guarantee: z.coerce.number().int().min(0),
    transfer_enabled: z.boolean(),
    transfer_disabled_groups: z.string(),
    transfer_daily_limit: z.coerce.number().int(),
    transfer_min_amount: z.coerce.number().int().min(0),
    transfer_max_amount: z.coerce.number().int(),
    transfer_fee_brackets: z.string(),
    command_cooldown_seconds: z.coerce.number().int().min(0),
    recall_failed_messages: z.boolean(),
    recall_delay_seconds: z.coerce.number().int().min(1).max(120),
    recall_policies: z.string(),
    admin_open_ids: z.string(),
    red_packet_enabled: z.boolean(),
    red_packet_disabled_groups: z.string(),
    red_packet_daily_limit: z.coerce.number().int(),
    red_packet_min_amount: z.coerce.number().int().min(0),
    red_packet_max_amount: z.coerce.number().int(),
    red_packet_default_count: z.coerce.number().int().min(1),
    red_packet_max_count: z.coerce.number().int().min(1),
    red_packet_expire_seconds: z.coerce.number().int().min(1),
    red_packet_allow_own_grab: z.boolean(),
    steal_enabled: z.boolean(),
    steal_success_rate: z.coerce.number().int().min(0).max(100),
    steal_min_amount: z.coerce.number().min(0),
    steal_max_amount: z.coerce.number().min(0),
    steal_daily_limit: z.coerce.number().int(),
    steal_recipient_grace_seconds: z.coerce.number().int().min(0),
  }),
})

type QQBotFormValues = z.infer<typeof qqbotSchema>

type QQBotSettingsSectionProps = {
  /** Flat option map keyed by 'qq_bot_setting.x' */
  defaultValues: Record<`qq_bot_setting.${string}`, string | number | boolean>
}

/** 'qq_bot_setting.x' entries -> nested form shape consumed by RHF paths. */
function unflattenDefaults(
  flat: QQBotSettingsSectionProps['defaultValues']
): QQBotFormValues {
  const out: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(flat)) {
    out[key.replace('qq_bot_setting.', '')] = value
  }
  return { qq_bot_setting: out } as QQBotFormValues
}

export function QQBotSettingsSection({
  defaultValues,
}: QQBotSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const formDefaults = useMemo(() => unflattenDefaults(defaultValues), [defaultValues])

  // z.coerce.number() makes the resolver input `unknown` while output is `number`;
  // the cast bridges that variance without weakening the schema itself.
  const form = useForm<QQBotFormValues>({
    resolver: zodResolver(qqbotSchema) as unknown as Resolver<QQBotFormValues>,
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)

  const { isDirty } = form.formState

  const onSubmit = async (data: QQBotFormValues) => {
    const defaults = formDefaults.qq_bot_setting as Record<string, string | number | boolean>
    const updates = Object.entries(data.qq_bot_setting)
      .filter(([key, value]) => value !== defaults[key])
      .map(([key, value]) => ({ key: `qq_bot_setting.${key}`, value: String(value) }))
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
    <SettingsSection title={t('QQ Bot')}>
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
            name='qq_bot_setting.drop_balance_anchor'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Drop balance anchor (quota)')}</FormLabel>
                <FormControl>
                  <Input type='number' min={0} {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Scales each drop by clamp(anchor / balance, 0.5, 2.0), re-clamped to the per-drop range. 0 disables weighting'
                  )}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.drop_daily_guarantee'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Drop daily guarantee (quota)')}</FormLabel>
                <FormControl>
                  <Input type='number' min={0} {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Tops up the last award of the day so the recent 7-day total reaches this amount. 0 disables'
                  )}
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

          <FormField
            control={form.control}
            name='qq_bot_setting.steal_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable stealing cheese')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Group members can move a random amount of another member\'s balance to themselves with 偷奶酪 @someone'
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
            name='qq_bot_setting.steal_success_rate'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Steal success rate (percent)')}</FormLabel>
                <FormControl>
                  <Input type='number' min={0} max={100} {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.steal_min_amount'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Steal minimum amount (currency units)')}</FormLabel>
                <FormControl>
                  <Input type='number' min={0} step='0.1' {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.steal_max_amount'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Steal maximum amount (currency units)')}</FormLabel>
                <FormControl>
                  <Input type='number' min={0} step='0.1' {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.steal_daily_limit'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Steal daily attempts per user')}</FormLabel>
                <FormControl>
                  <Input type='number' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Failed rolls also count. 0 or below means unlimited'
                  )}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.steal_recipient_grace_seconds'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Steal victim grace window (seconds)')}</FormLabel>
                <FormControl>
                  <Input type='number' min={0} {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'After a successful steal the same victim cannot be targeted again for this many seconds. 0 disables'
                  )}
                </FormDescription>
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='qq_bot_setting.command_cooldown_seconds'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Command cooldown (seconds)')}</FormLabel>
                <FormControl>
                  <Input type='number' min={0} {...field} />
                </FormControl>
                <FormDescription>
                  {t('Minimum seconds between commands from the same user. 0 disables the cooldown.')}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.recall_failed_messages'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Auto-recall failed replies')}</FormLabel>
                  <FormDescription>
                    {t('Automatically recall failure messages after a delay. Requires QQ platform recall permission.')}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch checked={field.value} onCheckedChange={field.onChange} />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.recall_delay_seconds'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Recall delay (seconds)')}</FormLabel>
                <FormControl>
                  <Input type='number' min={1} max={120} {...field} />
                </FormControl>
                <FormDescription>
                  {t('Seconds to wait before recalling a failed reply.')}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.recall_policies'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Recall policies (JSON, seconds per type)')}</FormLabel>
                <FormControl>
                  <Input placeholder='{"drop_award":30,"failure_notice":10}' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Map of reply type to seconds before auto-recall, e.g. {"drop_award":30}. Valid types: drop_award, checkin_success, checkin_fail, transfer, balance, menu, red_packet, bind_success, bind_fail, drop_command, failure_notice. 0 or a missing type keeps the message; empty falls back to recalling failed replies only (legacy switch). Recallable types are sent as proactive messages and require QQ platform recall permission.'
                  )}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='qq_bot_setting.admin_open_ids'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Admin QQ OpenIDs (comma-separated)')}</FormLabel>
                <FormControl>
                  <Input placeholder='OPENID_A,OPENID_B' {...field} />
                </FormControl>
                <FormDescription>
                  {t('Only these users can invoke admin commands like /balance and /ban. Leave empty to disable admin commands.')}
                </FormDescription>
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}

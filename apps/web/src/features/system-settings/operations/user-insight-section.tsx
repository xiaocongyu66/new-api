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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
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

// Keys mirror UserInsightSetting in apps/api/internal/usage/user_insight_setting.go.
const insightSchema = z.object({
  'user_insight_setting.enabled': z.boolean(),
  'user_insight_setting.record_in_log': z.boolean(),
  'user_insight_setting.gender_inference_enabled': z.boolean(),
  'user_insight_setting.jailbreak_alert_score': z.coerce
    .number()
    .int()
    .min(1)
    .max(100),
  'user_insight_setting.sample_enabled': z.boolean(),
  'user_insight_setting.sample_rate_percent': z.coerce
    .number()
    .int()
    .min(0)
    .max(100),
  'user_insight_setting.sample_keep_body': z.boolean(),
  'user_insight_setting.sample_quota_mb': z.coerce.number().int().min(1),
  'user_insight_setting.sample_retention_days': z.coerce.number().int().min(0),
  'user_insight_setting.auto_ban_enabled': z.boolean(),
  'user_insight_setting.auto_ban_min_risk': z.enum([
    'suspect',
    'likely',
    'confirmed',
  ]),
  'user_insight_setting.auto_ban_code_ratio_enabled': z.boolean(),
  'user_insight_setting.auto_ban_code_ratio_percent': z.coerce
    .number()
    .int()
    .min(1)
    .max(100),
  'user_insight_setting.auto_ban_code_min_requests': z.coerce
    .number()
    .int()
    .min(1),
})

type InsightFormValues = z.infer<typeof insightSchema>

type UserInsightSectionProps = {
  defaultValues: InsightFormValues
}

export function UserInsightSection({ defaultValues }: UserInsightSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const form = useForm({
    resolver: zodResolver(insightSchema),
    defaultValues,
  })

  useResetForm(form, defaultValues)

  const onSubmit = async (data: InsightFormValues) => {
    try {
      const updates = Object.entries(data).filter(
        ([key, value]) => value !== defaultValues[key as keyof InsightFormValues]
      )
      for (const [key, value] of updates) {
        await updateOption.mutateAsync({ key, value })
      }
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('Failed to update setting')
      )
    }
  }

  return (
    <SettingsSection title={t('User Insights')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />

          <FormField
            control={form.control}
            name='user_insight_setting.enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable user insights')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Analyze the request body prefix to profile client, usage and jailbreak risk'
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
            name='user_insight_setting.record_in_log'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Record insight in consume log')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Write each request profile into the consume log; disable to keep only user-level aggregates'
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
            name='user_insight_setting.gender_inference_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Infer gender preference')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Probabilistic inference for roleplay usage; disable for stricter privacy'
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
            name='user_insight_setting.jailbreak_alert_score'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Jailbreak alert score')}</FormLabel>
                <FormControl>
                  <Input type='number' min={1} max={100} {...field} />
                </FormControl>
                <FormDescription>
                  {t('Log a warning once the jailbreak score reaches this value')}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='user_insight_setting.sample_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Keep evidence samples')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Retain the matched sentences so an admin can review why a request was flagged'
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
            name='user_insight_setting.sample_rate_percent'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Sample rate (%)')}</FormLabel>
                <FormControl>
                  <Input type='number' min={0} max={100} {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Sampling rate for ordinary requests; jailbreak and relay hits are always kept'
                  )}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='user_insight_setting.sample_keep_body'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Keep full request body')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Store the original request text as well; this fills the sample quota quickly'
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
            name='user_insight_setting.sample_quota_mb'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Sample storage quota (MB)')}</FormLabel>
                <FormControl>
                  <Input type='number' min={1} {...field} />
                </FormControl>
                <FormDescription>
                  {t('Older samples are evicted once this cap is reached')}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='user_insight_setting.sample_retention_days'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Sample retention (days)')}</FormLabel>
                <FormControl>
                  <Input type='number' min={0} {...field} />
                </FormControl>
                <FormDescription>
                  {t('0 means samples are limited only by the storage quota')}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='user_insight_setting.auto_ban_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Auto-ban on jailbreak plus code')}</FormLabel>
                  <FormDescription>
                    {t(
                      'A ban is an irreversible user-facing event, so this stays off until you enable it'
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
            name='user_insight_setting.auto_ban_min_risk'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Minimum risk level for auto-ban')}</FormLabel>
                <Select value={field.value} onValueChange={field.onChange}>
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectItem value='suspect'>{t('Suspect')}</SelectItem>
                    <SelectItem value='likely'>{t('Likely')}</SelectItem>
                    <SelectItem value='confirmed'>{t('Confirmed')}</SelectItem>
                  </SelectContent>
                </Select>
                <FormDescription>
                  {t('Confirmed is the most conservative choice')}
                </FormDescription>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='user_insight_setting.auto_ban_code_ratio_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Auto-ban on code ratio')}</FormLabel>
                  <FormDescription>
                    {t(
                      'This rule needs no jailbreak signal, so a genuine developer can trip it; keep it off unless you accept that'
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
            name='user_insight_setting.auto_ban_code_ratio_percent'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Code ratio threshold (%)')}</FormLabel>
                <FormControl>
                  <Input type='number' min={1} max={100} {...field} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='user_insight_setting.auto_ban_code_min_requests'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Minimum requests before ratio applies')}</FormLabel>
                <FormControl>
                  <Input type='number' min={1} {...field} />
                </FormControl>
                <FormDescription>
                  {t('A ratio over too few requests is not statistically meaningful')}
                </FormDescription>
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}

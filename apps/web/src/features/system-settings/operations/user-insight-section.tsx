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
import { useEffect, useRef } from 'react'
import { useForm, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
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
import { useUpdateOption } from '../hooks/use-update-option'
import {
  buildInsightFormDefaults,
  DEFAULT_INSIGHT_VALUES,
  insightSchema,
  normalizeInsightFormValues,
  type InsightFlatDefaults,
  type InsightFormValues,
} from './user-insight-defaults'

type UserInsightSectionProps = {
  defaultValues?: InsightFlatDefaults
}

export function UserInsightSection({
  defaultValues = DEFAULT_INSIGHT_VALUES,
}: UserInsightSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const form = useForm<InsightFormValues, unknown, InsightFormValues>({
    resolver: zodResolver(insightSchema) as Resolver<
      InsightFormValues,
      unknown,
      InsightFormValues
    >,
    defaultValues: buildInsightFormDefaults(defaultValues),
  })

  // 服务端配置在挂载后才到达（页面的 options 查询）：只有序列化基线真正变化时
  // 才重置表单，避免覆盖用户输入。
  const baselineRef = useRef<InsightFlatDefaults>(defaultValues)
  const baselineSerializedRef = useRef<string>(JSON.stringify(defaultValues))

  useEffect(() => {
    const serialized = JSON.stringify(defaultValues)
    if (serialized === baselineSerializedRef.current) return
    baselineRef.current = defaultValues
    baselineSerializedRef.current = serialized
    form.reset(buildInsightFormDefaults(defaultValues))
  }, [defaultValues, form])

  const onSubmit = async (values: InsightFormValues) => {
    try {
      const normalized = normalizeInsightFormValues(values)
      const changed = (
        Object.keys(normalized) as Array<keyof InsightFlatDefaults>
      ).filter((key) => normalized[key] !== baselineRef.current[key])

      if (changed.length === 0) {
        toast.info(t('No changes to save'))
        return
      }

      for (const key of changed) {
        await updateOption.mutateAsync({
          key,
          value: normalized[key],
        })
      }

      baselineRef.current = normalized
      baselineSerializedRef.current = JSON.stringify(normalized)
      form.reset(buildInsightFormDefaults(normalized))
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
            isSaving={updateOption.isPending || form.formState.isSubmitting}
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
                  {t(
                    'Log a warning once the jailbreak score reaches this value'
                  )}
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
                <FormLabel>
                  {t('Minimum requests before ratio applies')}
                </FormLabel>
                <FormControl>
                  <Input type='number' min={1} {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'A ratio over too few requests is not statistically meaningful'
                  )}
                </FormDescription>
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}

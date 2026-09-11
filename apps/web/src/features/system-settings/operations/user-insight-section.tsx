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
import { useEffect, useMemo, useRef } from 'react'
import { useForm, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Checkbox } from '@/components/ui/checkbox'
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
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { useClientCatalog } from '@/features/user-insights/hooks/use-user-insights'
import type { ClientCatalogEntry } from '@/features/user-insights/types'

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

          <FormField
            control={form.control}
            name='user_insight_setting.client_ban_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Ban clients by request header')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Reject relay requests identified as banned clients by their request headers (User-Agent + tool-specific headers) with a 403, before channel distribution'
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
            name='user_insight_setting.blocked_clients'
            render={({ field }) => (
              // span=full：客户端目录有 40+ 项，挤在半列里会堆成长条；
              // 桌面端跨双列、组级再自动分栏，手机端单栏换行。
              <FormItem data-settings-form-span='full'>
                <FormLabel>{t('Blocked clients (site-wide)')}</FormLabel>
                <FormControl>
                  <BlockedClientsPicker
                    value={field.value ?? []}
                    onChange={field.onChange}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Pick which request-header clients are banned for every user. Banned only while the switch above is on; single users can be treated differently from the dashboard.'
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

// 封禁勾选列表的分组顺序与措辞：运营方的心智是"编程 harness / 手机端 /
// 网页聊天"三大类，agent_cli 与 ide 都属于编程 harness，合并展示。
const CLIENT_KIND_GROUPS = [
  { kinds: ['agent_cli', 'ide'], labelKey: 'Coding harness (agents & IDEs)' },
  { kinds: ['mobile'], labelKey: 'Mobile apps' },
  { kinds: ['chat_ui'], labelKey: 'Chat UIs' },
  { kinds: ['browser'], labelKey: 'Web browsers' },
  { kinds: ['sdk'], labelKey: 'SDKs' },
  { kinds: ['http_tool'], labelKey: 'HTTP tools & scripts' },
  // 自动发现项排在最后：数量最多、变化最快，且需要靠请求量判断该不该封。
  { kinds: ['discovered'], labelKey: 'Seen in traffic (no built-in rule)' },
] as const

/**
 * 全站封禁客户端的勾选选择器。
 *
 * 选项来自后端识别规则目录（/api/user-insight/client-catalog），
 * 按"编程 harness / 手机端 / 聊天界面 / 浏览器 / SDK"分组勾选，
 * 规则增删后自动跟上，不存在前端静态镜像的漂移。
 */
function BlockedClientsPicker({
  value,
  onChange,
}: {
  value: string[]
  onChange: (value: string[]) => void
}) {
  const { t } = useTranslation()
  const catalogQuery = useClientCatalog()
  const catalogData = catalogQuery.data?.data

  const groups = useMemo(() => {
    const byKind = new Map<string, ClientCatalogEntry[]>()
    for (const entry of catalogData ?? []) {
      const list = byKind.get(entry.kind) ?? []
      list.push(entry)
      byKind.set(entry.kind, list)
    }
    return CLIENT_KIND_GROUPS.map((group) => ({
      labelKey: group.labelKey,
      // 组内按站内请求量降序：真正需要处置的客户端排在最前，
      // 没见过的（0 请求）沉到末尾。
      items: group.kinds
        .flatMap((kind) => byKind.get(kind) ?? [])
        .sort((a, b) => b.requests - a.requests || a.name.localeCompare(b.name)),
    })).filter((group) => group.items.length > 0)
  }, [catalogData])

  if (catalogQuery.isLoading) {
    return <Skeleton className='h-24 w-full' />
  }

  return (
    // 组级自动分栏：手机单栏，sm 两栏，xl 三栏，按剩余宽度自适应。
    // 用 CSS multi-column 而不是等宽 grid——各组条目数差异大
    // （编程 harness 17 项、浏览器 1 项），等宽栅格会留大片空洞，
    // 分栏则像瀑布流一样把组填满。break-inside-avoid 保证组不跨栏断开。
    <div className='columns-1 gap-4 sm:columns-2 xl:columns-3'>
      {groups.map((group) => (
        <div key={group.labelKey} className='mb-4 break-inside-avoid'>
          <div className='text-muted-foreground text-xs font-medium'>
            {t(group.labelKey)}
            <span className='ml-1 tabular-nums opacity-70'>
              ({group.items.length})
            </span>
          </div>
          <div className='mt-1.5 flex flex-wrap gap-1.5'>
            {group.items.map((item) => {
              const checked = value.includes(item.id)
              return (
                <label
                  key={item.id}
                  className='has-data-[state=checked]:bg-muted hover:bg-muted/40 flex max-w-full cursor-pointer items-center gap-1.5 rounded-md border px-2 py-1 text-xs'
                  title={item.id}
                >
                  <Checkbox
                    checked={checked}
                    onCheckedChange={() => {
                      onChange(
                        checked
                          ? value.filter((id) => id !== item.id)
                          : [...value, item.id]
                      )
                    }}
                  />
                  <span className='truncate'>{item.name}</span>
                  {item.requests > 0 && (
                    <span className='text-muted-foreground hidden text-[11px] tabular-nums sm:inline'>
                      {t('{{requests}} reqs · {{users}} users', {
                        requests: item.requests,
                        users: item.users,
                      })}
                    </span>
                  )}
                </label>
              )
            })}
          </div>
        </div>
      ))}
    </div>
  )
}

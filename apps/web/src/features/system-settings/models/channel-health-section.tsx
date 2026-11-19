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
import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { isAxiosError } from 'axios'
import { toast } from 'sonner'
import {
  ChevronDown,
  Loader2,
  RefreshCw,
  Search,
  Settings2,
  Shield,
  Zap,
} from 'lucide-react'

import { Dialog } from '@/components/dialog'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { cn } from '@/lib/utils'
import { NumericSpinnerInput } from '@/features/channels/components/numeric-spinner-input'
import { SettingsSection } from '../components/settings-section'
import {
  getProbeOverview,
  runProbeNow,
  testProbeWebhook,
  updateProbePolicy,
  updateProbeSettings,
  type ProbeChannelStat,
  type ProbeModelStat,
  type ProbePolicy,
  type ProbeSettings,
} from './api-probe'

/** 渠道 24h 探测可用率档位，判定逻辑镜像后端 ClassifyProbeGrade。 */
// ============================================================================
// 类型与常量：档位定义、色阶映射
// ============================================================================

type ProbeGrade = 'ok' | 'warning' | 'critical' | 'emergency' | 'unknown'

const GRADE_TONE: Record<ProbeGrade, StatusVariant> = {
  ok: 'success',
  warning: 'warning',
  critical: 'orange',
  emergency: 'danger',
  unknown: 'neutral',
}

// 与可用率同源的文本色阶：正常绿、警告黄、严重橙、紧急红、未探测灰。
const GRADE_TEXT_CLASS: Record<ProbeGrade, string> = {
  ok: 'text-green-600 dark:text-green-400',
  warning: 'text-amber-600 dark:text-amber-400',
  critical: 'text-orange-600 dark:text-orange-400',
  emergency: 'text-red-600 dark:text-red-400',
  unknown: 'text-muted-foreground',
}

const GRADE_DOT_CLASS: Record<ProbeGrade, string> = {
  ok: 'bg-success',
  warning: 'bg-warning',
  critical: 'bg-warning',
  emergency: 'bg-destructive',
  unknown: 'bg-neutral',
}

// ============================================================================
// 工具函数：档位判定（镜像后端）、错误状态提取、时间格式化
// ============================================================================

function classifyGrade(
  availability: number | null,
  saved: {
    warning_threshold: number
    critical_threshold: number
    emergency_threshold: number
  }
): ProbeGrade {
  if (availability === null) return 'unknown'
  if (availability >= saved.warning_threshold) return 'ok'
  if (availability >= saved.critical_threshold) return 'warning'
  if (availability >= saved.emergency_threshold) return 'critical'
  return 'emergency'
}

// axios 的 409 语义：已有同类型探测任务在跑（后端 active_key 去重）。
function conflictStatusOf(error: unknown): number | undefined {
  return isAxiosError(error) ? error.response?.status : undefined
}

function formatProbeTime(unixSeconds: number) {
  if (unixSeconds <= 0) return '—'
  return new Date(unixSeconds * 1000).toLocaleString(undefined, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

// ============================================================================
// 组件：上游情况（渠道探测可用率监控）
// ============================================================================

export function ChannelHealthSection() {
  const { t } = useTranslation()
  const [settings, setSettings] = useState<ProbeSettings | null>(null)
  const [draft, setDraft] = useState<ProbeSettings | null>(null)
  const [channels, setChannels] = useState<ProbeChannelStat[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [search, setSearch] = useState('')
  const [gradeFilter, setGradeFilter] = useState<ProbeGrade | 'all'>('all')
  const [sortBy, setSortBy] = useState<'availability' | 'id'>('availability')
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [expandedChannels, setExpandedChannels] = useState<Set<number>>(
    () => new Set()
  )
  const [probingChannelIds, setProbingChannelIds] = useState<Set<number>>(
    () => new Set()
  )
  const [isProbingAll, setIsProbingAll] = useState(false)
  const [isSavingSettings, setIsSavingSettings] = useState(false)
  const [isTestingWebhook, setIsTestingWebhook] = useState(false)
  const [reloadToken, setReloadToken] = useState(0)
  // load 需要读 dirty 状态但不能把它放进依赖（否则每次输入都触发整页刷新）；
  // 用 ref 保存最新值，同时在 load 内用请求序号丢弃过期响应。
  const draftDirtyRef = useRef(false)
  const requestSeqRef = useRef(0)
  // 渠道策略弹窗：policyChannel 为 null 表示关闭。
  const [policyChannel, setPolicyChannel] = useState<ProbeChannelStat | null>(
    null
  )
  const [policyDraft, setPolicyDraft] = useState<ProbePolicy>({
    auto_control_enabled: false,
    disable_below: 30,
    enable_above: 70,
  })
  const [isSavingPolicy, setIsSavingPolicy] = useState(false)

  // --------------------------------------------------------------------------
  // 数据加载：拉取 overview（设置 + 全启用渠道 24h 聚合）
  // --------------------------------------------------------------------------
  const load = useCallback(async () => {
    const seq = ++requestSeqRef.current
    setIsLoading(true)
    try {
      const res = await getProbeOverview()
      if (!res.success) {
        throw new Error(res.message || t('Failed to load probe overview'))
      }
      // 只应用最新一次请求的响应：先发后到的旧响应会把新数据/草稿回滚。
      if (seq !== requestSeqRef.current) return
      const data = res.data
      setChannels(data?.channels ?? [])
      if (data?.settings) {
        setSettings(data.settings)
        // 有未保存草稿时不覆盖输入中的修改；保存成功后清除 dirty 再跟随。
        if (!draftDirtyRef.current) {
          setDraft({ ...data.settings })
        }
      }
    } catch (error: unknown) {
      if (seq !== requestSeqRef.current) return
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to load probe overview')
      )
    } finally {
      if (seq === requestSeqRef.current) {
        setIsLoading(false)
      }
    }
  }, [t])

  useEffect(() => {
    load()
  }, [load, reloadToken])

  const savedSettings = settings ?? {
    auto_probe_enabled: false,
    probe_interval_minutes: 30,
    warning_threshold: 70,
    critical_threshold: 50,
    emergency_threshold: 30,
    webhook_enabled: false,
    webhook_url: '',
    webhook_secret: '',
    webhook_secret_configured: false,
  }

  const gradeOf = useCallback(
    (availability: number | null) => classifyGrade(availability, savedSettings),
    [savedSettings]
  )

  const editDraft = (patch: Partial<ProbeSettings>) => {
    draftDirtyRef.current = true
    setDraft((prev) => (prev === null ? prev : { ...prev, ...patch }))
  }

  // --------------------------------------------------------------------------
  // 派生数据：搜索/档位筛选/排序、档位计数、下拉选项
  // --------------------------------------------------------------------------
  const filteredChannels = useMemo(() => {
    const query = search.trim().toLowerCase()
    let list = channels
    if (query) {
      list = list.filter(
        (channel) =>
          channel.name.toLowerCase().includes(query) ||
          String(channel.channel_id).includes(query) ||
          channel.models.some((model) =>
            model.model.toLowerCase().includes(query)
          )
      )
    }
    if (gradeFilter !== 'all') {
      list = list.filter(
        (channel) => gradeOf(channel.availability) === gradeFilter
      )
    }
    const sorted = [...list]
    if (sortBy === 'availability') {
      sorted.sort((a, b) => {
        if (a.availability === null && b.availability === null)
          return a.channel_id - b.channel_id
        if (a.availability === null) return 1
        if (b.availability === null) return -1
        return a.availability - b.availability
      })
    } else {
      sorted.sort((a, b) => a.channel_id - b.channel_id)
    }
    return sorted
  }, [channels, search, gradeFilter, gradeOf, sortBy])

  const gradeCount = useMemo(() => {
    const counts: Record<ProbeGrade, number> = {
      ok: 0,
      warning: 0,
      critical: 0,
      emergency: 0,
      unknown: 0,
    }
    for (const channel of channels) {
      counts[gradeOf(channel.availability)]++
    }
    return counts
  }, [channels, gradeOf])

  const gradeFilterItems = useMemo(
    () => [
      { value: 'all', label: t('All') },
      { value: 'ok', label: t('Healthy') },
      {
        value: 'warning',
        label: t('Below {{threshold}}%', {
          threshold: savedSettings.warning_threshold,
        }),
      },
      {
        value: 'critical',
        label: t('Below {{threshold}}%', {
          threshold: savedSettings.critical_threshold,
        }),
      },
      {
        value: 'emergency',
        label: t('Below {{threshold}}%', {
          threshold: savedSettings.emergency_threshold,
        }),
      },
      { value: 'unknown', label: t('Not probed') },
    ],
    [t, savedSettings]
  )

  const sortByItems = useMemo(
    () => [
      { value: 'availability', label: t('Sort by Availability') },
      { value: 'id', label: t('Sort by ID') },
    ],
    [t]
  )

  // --------------------------------------------------------------------------
  // 动作：触发探测、保存设置、测试 webhook、展开/折叠
  // --------------------------------------------------------------------------
  const handleProbe = async (channelIds: number[], channelId?: number) => {
    if (channelId !== undefined) {
      setProbingChannelIds((prev) => new Set(prev).add(channelId))
    } else {
      setIsProbingAll(true)
    }
    try {
      const res = await runProbeNow(channelIds)
      if (!res.success) {
        throw new Error(res.message || t('Failed to start probe task'))
      }
      toast.success(t('Probe task started'))
    } catch (error: unknown) {
      toast.error(
        conflictStatusOf(error) === 409
          ? t('A probe task is already running')
          : error instanceof Error
            ? error.message
            : t('Failed to start probe task')
      )
    } finally {
      if (channelId !== undefined) {
        setProbingChannelIds((prev) => {
          const next = new Set(prev)
          next.delete(channelId)
          return next
        })
      } else {
        setIsProbingAll(false)
      }
    }
  }

  const handleSaveSettings = async () => {
    if (!draft) return
    setIsSavingSettings(true)
    try {
      const res = await updateProbeSettings(draft)
      if (!res.success) {
        throw new Error(res.message || t('Failed to save probe settings'))
      }
      toast.success(t('Probe settings saved'))
      draftDirtyRef.current = false
      setReloadToken((token) => token + 1)
    } catch (error: unknown) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to save probe settings')
      )
    } finally {
      setIsSavingSettings(false)
    }
  }

  const handleTestWebhook = async () => {
    if (!draft) return
    setIsTestingWebhook(true)
    try {
      const res = await testProbeWebhook(draft.webhook_url, draft.webhook_secret)
      if (!res.success) {
        throw new Error(res.message || t('Failed to send test notification'))
      }
      toast.success(t('Test notification sent'))
    } catch (error: unknown) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to send test notification')
      )
    } finally {
      setIsTestingWebhook(false)
    }
  }

  // 打开策略弹窗：已有策略回填；否则给默认值（50/70，自动控制关闭）。
  const openPolicyEditor = (channel: ProbeChannelStat) => {
    setPolicyChannel(channel)
    setPolicyDraft(
      channel.policy
        ? { ...channel.policy }
        : { auto_control_enabled: false, disable_below: 30, enable_above: 70 }
    )
  }

  const savePolicy = async () => {
    if (!policyChannel) return
    if (policyDraft.disable_below >= policyDraft.enable_above) {
      toast.error(t('Disable below (%)') + ' < ' + t('Enable above (%)'))
      return
    }
    setIsSavingPolicy(true)
    try {
      const res = await updateProbePolicy(policyChannel.channel_id, policyDraft)
      if (!res.success) {
        throw new Error(res.message || t('Failed to save policy'))
      }
      toast.success(t('Policy saved'))
      setPolicyChannel(null)
      setReloadToken((token) => token + 1)
    } catch (error: unknown) {
      toast.error(
        error instanceof Error ? error.message : t('Failed to save policy')
      )
    } finally {
      setIsSavingPolicy(false)
    }
  }

  const toggleChannelExpanded = (channelId: number) => {
    setExpandedChannels((prev) => {
      const next = new Set(prev)
      if (next.has(channelId)) {
        next.delete(channelId)
      } else {
        next.add(channelId)
      }
      return next
    })
  }

  // --------------------------------------------------------------------------
  // 渲染辅助：档位文案、可用率文案、模型明细行、档位汇总徽章
  // --------------------------------------------------------------------------
  // 档位展示文案：warning/critical/emergency 共用「低于 N%」插值模板。
  const gradeLabel = (grade: ProbeGrade) => {
    if (grade === 'unknown') return t('Not probed')
    if (grade === 'ok') return t('Healthy')
    const threshold =
      grade === 'warning'
        ? savedSettings.warning_threshold
        : grade === 'critical'
          ? savedSettings.critical_threshold
          : savedSettings.emergency_threshold
    return t('Below {{threshold}}%', { threshold })
  }

  const availabilityText = (availability: number | null) =>
    availability === null ? t('Not probed') : `${availability.toFixed(1)}%`

  const renderModelRow = (stat: ProbeModelStat) => {
    const grade = gradeOf(stat.availability)
    return (
      <TableRow key={stat.model} className='hover:bg-transparent'>
        <TableCell className='py-1.5' />
        <TableCell className='py-1.5' title={stat.model}>
          <div className='flex flex-col gap-0.5'>
            <span className='text-muted-foreground truncate font-mono text-xs'>{stat.model}</span>
            {/* 探测时间：放在模型名下方，字号更小 */}
            <span className='text-muted-foreground text-[10px]!'>
              {formatProbeTime(stat.last_probe_at)}
            </span>
          </div>
        </TableCell>
        <TableCell
          className={cn('py-1.5 text-sm tabular-nums', GRADE_TEXT_CLASS[grade])}
        >
          {availabilityText(stat.availability)}
        </TableCell>
        <TableCell className='py-1.5'>
          {stat.auto_disabled ? (
            <StatusBadge variant='danger' className='text-[11px]!'>
              {t('Auto-disabled')}
            </StatusBadge>
          ) : !stat.enabled ? (
            <StatusBadge variant='neutral' className='text-[11px]!'>
              {t('Disabled')}
            </StatusBadge>
          ) : (
            <span
              className='text-muted-foreground block truncate text-xs'
              title={stat.last_error_code || undefined}
            >
              {stat.last_error_code || '—'}
            </span>
          )}
        </TableCell>
        {/* 探测列：上下两行——成功（带状态点）/ 总数（不带点） */}
        <TableCell className='py-1.5'>
          <div className='flex flex-col gap-0.5'>
            <StatusBadge
              variant={stat.success_count > 0 ? 'success' : 'danger'}
              showDot
              className='text-[11px]!'
            >
              {t('Success')} {stat.success_count}
            </StatusBadge>
            <StatusBadge variant='neutral' className='text-[11px]!'>
              {t('Total')} {stat.total_count}
            </StatusBadge>
          </div>
        </TableCell>
        <TableCell className='py-1.5' />
      </TableRow>
    )
  }

  const gradeSummary = (
    [
      ['ok', gradeCount.ok],
      ['warning', gradeCount.warning],
      ['critical', gradeCount.critical],
      ['emergency', gradeCount.emergency],
      ['unknown', gradeCount.unknown],
    ] as const
  ).map(([grade, count]) =>
    count > 0 ? (
      <Badge key={grade} variant='outline' className='gap-1.5'>
        <span className={cn('size-1.5 shrink-0 rounded-full', GRADE_DOT_CLASS[grade])} />
        {gradeLabel(grade)}: {count}
      </Badge>
    ) : null
  )

  // --------------------------------------------------------------------------
  // 渲染：设置折叠面板 → 工具行 → 档位汇总 → 渠道表格（行点击展开模型明细）
  // --------------------------------------------------------------------------
  return (
    <SettingsSection title={t('Upstream Status')}>
      <div className='space-y-4 rounded-xl bg-card-surface p-4 backdrop-blur-card'>
        {/* 监控设置折叠面板：展开可编辑探测开关/间隔、三阈值与 webhook，
            折叠时搜索、筛选、探测按钮仍然可用。 */}
        <Collapsible open={settingsOpen} onOpenChange={setSettingsOpen}>
          <CollapsibleTrigger
            className={`
              hover:bg-muted flex w-full items-center justify-between rounded-md
              border px-3 py-2 text-sm font-medium transition-colors
            `}
          >
            <span className='flex items-center gap-2'>
              <Settings2 className='size-4' />
              {t('Monitor settings')}
            </span>
            <ChevronDown
              className={`size-4 transition-transform ${settingsOpen ? 'rotate-180' : ''}`}
            />
          </CollapsibleTrigger>
          <CollapsibleContent>
            {draft && (
              <div className='mt-3 space-y-4 rounded-md border p-3'>
                <div className='grid grid-cols-1 items-center gap-x-6 gap-y-3 sm:grid-cols-2'>
                  <div className='flex items-center justify-between gap-2'>
                    <Label htmlFor='probe-auto-enabled' className='shrink-0'>
                      {t('Auto probe')}
                    </Label>
                    <Switch
                      id='probe-auto-enabled'
                      checked={draft.auto_probe_enabled}
                      onCheckedChange={(checked) =>
                        editDraft({ auto_probe_enabled: checked === true })
                      }
                    />
                  </div>
                  <div className='flex items-center justify-between gap-2'>
                    <Label htmlFor='probe-interval' className='shrink-0'>
                      {t('Probe interval (minutes)')}
                    </Label>
                    <NumericSpinnerInput
                      value={draft.probe_interval_minutes}
                      onChange={(value) =>
                        editDraft({ probe_interval_minutes: value })
                      }
                      min={1}
                      max={1440}
                      disabled={!draft.auto_probe_enabled}
                    />
                  </div>
                </div>

                <div className='space-y-3'>
                  <div className='flex items-center gap-2'>
                    <Switch
                      id='probe-webhook-enabled'
                      checked={draft.webhook_enabled}
                      onCheckedChange={(checked) =>
                        editDraft({ webhook_enabled: checked === true })
                      }
                    />
                    <Label htmlFor='probe-webhook-enabled' className='shrink-0'>
                      {t('Webhook notifications')}
                    </Label>
                  </div>
                  <div className='flex flex-wrap items-center gap-x-4 gap-y-2'>
                    <Input
                      value={draft.webhook_url}
                      onChange={(e) =>
                        editDraft({ webhook_url: e.target.value })
                      }
                      placeholder={t('Webhook URL')}
                      className='min-w-[240px] flex-1'
                      disabled={!draft.webhook_enabled}
                    />
                    <Input
                      type='password'
                      value={draft.webhook_secret}
                      onChange={(e) =>
                        editDraft({ webhook_secret: e.target.value })
                      }
                      placeholder={
                        draft.webhook_secret_configured
                          ? t('Webhook secret configured')
                          : t('Webhook secret')
                      }
                      className='min-w-[160px] flex-1'
                      disabled={!draft.webhook_enabled}
                    />
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      onClick={handleTestWebhook}
                      disabled={isTestingWebhook || !draft.webhook_enabled}
                    >
                      {isTestingWebhook && (
                        <Loader2 className='mr-2 h-4 w-4 animate-spin' />
                      )}
                      {t('Send test notification')}
                    </Button>
                  </div>
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Availability notifications are sent on threshold crossings; leaving the secret empty keeps the saved one.',
                    )}
                  </p>
                </div>

                <div className='flex justify-end'>
                  <Button
                    type='button'
                    size='sm'
                    onClick={handleSaveSettings}
                    disabled={isSavingSettings}
                  >
                    {isSavingSettings && (
                      <Loader2 className='mr-2 h-4 w-4 animate-spin' />
                    )}
                    {t('Save settings')}
                  </Button>
                </div>
              </div>
            )}
          </CollapsibleContent>
        </Collapsible>

        {/* 常驻工具行：搜索独占一行；档位筛选/排序/立即探测/刷新一行。 */}
        <div className='space-y-2'>
          <div className='relative'>
            <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2' />
            <Input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={t('Search channel or model...')}
              className='pl-9'
            />
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            <Select
              items={gradeFilterItems}
              value={gradeFilter}
              onValueChange={(value) => {
                const option = gradeFilterItems.find(
                  (item) => item.value === value
                )
                if (!option) return
                setGradeFilter(option.value as ProbeGrade | 'all')
              }}
            >
              <SelectTrigger className='w-[190px]' aria-label={t('State')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {gradeFilterItems.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <Select
              items={sortByItems}
              value={sortBy}
              onValueChange={(value) => {
                const option = sortByItems.find((item) => item.value === value)
                if (!option) return
                setSortBy(option.value as 'availability' | 'id')
              }}
            >
              <SelectTrigger className='w-[170px]' aria-label={t('Sort by')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {sortByItems.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <Button
              type='button'
              size='sm'
              onClick={() => handleProbe([])}
              disabled={isProbingAll || isLoading}
            >
              {isProbingAll ? (
                <Loader2 className='mr-2 h-4 w-4 animate-spin' />
              ) : (
                <Zap className='mr-2 h-4 w-4' />
              )}
              {t('Probe now')}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => setReloadToken((token) => token + 1)}
              disabled={isLoading}
            >
              <RefreshCw
                className={isLoading ? 'h-4 w-4 animate-spin' : 'h-4 w-4'}
              />
              {t('Refresh')}
            </Button>
          </div>
        </div>

        {/* 档位计数徽章（与表格同源），一眼看清分布。 */}
        <div className='flex flex-wrap items-center gap-2'>{gradeSummary}</div>

        <div className='overflow-x-auto'>
          <Table className='table-fixed'>
            <TableHeader>
              <TableRow>
                <TableHead className='w-[40px]'></TableHead>
                <TableHead className='w-[240px]'>{t('Channel')}</TableHead>
                <TableHead className='w-[130px]'>
                  {t('Availability (24h)')}
                </TableHead>
                <TableHead className='w-[140px]'>{t('State')}</TableHead>
                <TableHead className='w-[110px]'>{t('Probe')}</TableHead>
                <TableHead className='w-[210px]'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                <TableRow>
                  <TableCell colSpan={6} className='py-8 text-center'>
                    <div className='flex items-center justify-center gap-2'>
                      <Loader2 className='h-5 w-5 animate-spin text-muted-foreground' />
                      <span>{t('Loading...')}</span>
                    </div>
                  </TableCell>
                </TableRow>
              ) : filteredChannels.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className='py-8 text-center text-muted-foreground'
                  >
                    {channels.length === 0
                      ? t('No enabled channels')
                      : t('No matching routes found.')}
                  </TableCell>
                </TableRow>
              ) : (
                filteredChannels.map((channel) => {
                  const grade = gradeOf(channel.availability)
                  const isExpanded = expandedChannels.has(channel.channel_id)
                  const isProbing =
                    isProbingAll || probingChannelIds.has(channel.channel_id)
                  const lastProbeAt = channel.models.reduce(
                    (latest, model) =>
                      model.last_probe_at > latest ? model.last_probe_at : latest,
                    0
                  )
                  return (
                    <Fragment key={channel.channel_id}>
                    <TableRow
                      className='hover:bg-muted/40 cursor-pointer'
                      onClick={() => toggleChannelExpanded(channel.channel_id)}
                    >
                      <TableCell>
                        <button
                          type='button'
                          aria-label={t('Model details')}
                          aria-expanded={isExpanded}
                          onClick={(event) => {
                            event.stopPropagation()
                            toggleChannelExpanded(channel.channel_id)
                          }}
                          className='hover:bg-muted rounded p-1 transition-colors'
                        >
                          <ChevronDown
                            className={`size-4 transition-transform ${isExpanded ? 'rotate-180' : ''}`}
                          />
                        </button>
                      </TableCell>
                      <TableCell>
                        <div className='flex flex-col gap-0.5'>
                          <span>
                            <span className='font-medium text-primary'>{channel.name}</span>
                            <span className='text-muted-foreground ml-1.5 font-mono text-xs'>
                              #{channel.channel_id}
                            </span>
                            <StatusBadge variant='neutral' className='ml-1.5 text-[11px]!'>
                              {channel.models.length}
                            </StatusBadge>
                          </span>
                          {/* 探测时间：放在渠道名下方，字号更小 */}
                          <span className='text-muted-foreground text-[10px]!'>
                            {formatProbeTime(lastProbeAt)}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell
                        className={cn(
                          'font-medium tabular-nums',
                          GRADE_TEXT_CLASS[grade]
                        )}
                      >
                        {availabilityText(channel.availability)}
                      </TableCell>
                      <TableCell>
                        <StatusBadge variant={GRADE_TONE[grade]}>
                          {gradeLabel(grade)}
                        </StatusBadge>
                      </TableCell>
                      {/* 探测列：上下两行——成功（带状态点）/ 总数（不带点） */}
                      <TableCell>
                        <div className='flex flex-col gap-0.5'>
                          <StatusBadge
                            variant={channel.success_count > 0 ? 'success' : 'danger'}
                            showDot
                            className='text-[11px]!'
                          >
                            {t('Success')} {channel.success_count}
                          </StatusBadge>
                          <StatusBadge variant='neutral' className='text-[11px]!'>
                            {t('Total')} {channel.total_count}
                          </StatusBadge>
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className='flex items-center gap-1.5'>
                          <Button
                            variant='outline'
                            size='sm'
                            onClick={(event) => {
                              event.stopPropagation()
                              openPolicyEditor(channel)
                            }}
                          >
                            <Shield className='mr-1 size-3.5' />
                            {t('Policy')}
                          </Button>
                          <Button
                            variant='outline'
                            size='sm'
                            disabled={isProbing}
                            onClick={(event) => {
                              event.stopPropagation()
                              handleProbe(
                                [channel.channel_id],
                                channel.channel_id
                              )
                            }}
                          >
                            {isProbing && (
                              <Loader2 className='mr-1 size-3.5 animate-spin' />
                            )}
                            {t('Probe now')}
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                    {/* 模型级明细：每个模型一行真实表格行，列宽与主表头一一对齐。 */}
                    {isExpanded &&
                      (channel.models.length === 0 ? (
                        <TableRow className='hover:bg-transparent'>
                          <TableCell colSpan={6} className='text-muted-foreground py-3 text-center text-sm'>
                            {t('No models configured for this channel')}
                          </TableCell>
                        </TableRow>
                      ) : (
                        channel.models.map(renderModelRow)
                      ))}
                    </Fragment>
                  )
                })
              )}
            </TableBody>
          </Table>
        </div>

        <p className='text-muted-foreground text-xs'>
          {t(
            'Probing sends a small real request per channel model billed to the root account; use the interval setting to control cost.',
          )}
        </p>
      </div>

      {/* 渠道策略弹窗：基于模型级 24h 可用率自动禁用/恢复模型路由。 */}
      <Dialog
        open={policyChannel !== null}
        onOpenChange={(open) => {
          if (!open) setPolicyChannel(null)
        }}
        title={t('Probe policy')}
        description={
          policyChannel
            ? `${policyChannel.name} #${policyChannel.channel_id}`
            : undefined
        }
        contentHeight='auto'
        footer={
          <div className='flex w-full items-center justify-end gap-2'>
            <Button
              variant='outline'
              onClick={() => setPolicyChannel(null)}
              disabled={isSavingPolicy}
            >
              {t('Cancel')}
            </Button>
            <Button onClick={savePolicy} disabled={isSavingPolicy}>
              {isSavingPolicy && (
                <Loader2 className='mr-2 size-4 animate-spin' />
              )}
              {t('Save settings')}
            </Button>
          </div>
        }
      >
        <div className='space-y-4 py-2'>
          <div className='flex items-center justify-between gap-2'>
            <Label htmlFor='probe-policy-auto-control' className='shrink-0'>
              {t('Auto recover and disable')}
            </Label>
            <Switch
              id='probe-policy-auto-control'
              checked={policyDraft.auto_control_enabled}
              onCheckedChange={(checked) =>
                setPolicyDraft({
                  ...policyDraft,
                  auto_control_enabled: checked === true,
                })
              }
            />
          </div>
          <div className='grid grid-cols-2 items-center gap-x-6 gap-y-3'>
            <div className='flex items-center justify-between gap-2'>
              <Label className='min-w-0 shrink text-left leading-tight'>
                {t('Disable below (%)')}
              </Label>
              <NumericSpinnerInput
                value={policyDraft.disable_below}
                onChange={(value) =>
                  setPolicyDraft({ ...policyDraft, disable_below: value })
                }
                min={0}
                max={100}
              />
            </div>
            <div className='flex items-center justify-between gap-2'>
              <Label className='min-w-0 shrink text-left leading-tight'>
                {t('Enable above (%)')}
              </Label>
              <NumericSpinnerInput
                value={policyDraft.enable_above}
                onChange={(value) =>
                  setPolicyDraft({ ...policyDraft, enable_above: value })
                }
                min={0}
                max={100}
              />
            </div>
          </div>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Model routes are disabled automatically when 24h availability drops below the disable threshold, and restored above the enable threshold.',
            )}
          </p>
        </div>
      </Dialog>
    </SettingsSection>
  )
}

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  FloatingBackToTop,
  FloatingSectionNav,
  type FloatingSectionItem,
} from '@/components/floating-section-nav'
import { Badge } from '@/components/ui/badge'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { getChannels } from '@/features/channels/api'
import { getModels } from '@/features/models/api'
import { handleServerError } from '@/lib/handle-server-error'

import {
  createJailbreakProfile,
  previewJailbreakPrompt,
  testJailbreakRequest,
  updateJailbreakProfile,
} from '../api'
import type {
  JailbreakMode,
  JailbreakPreviewResult,
  JailbreakProfile,
  JailbreakProfileInput,
} from '../types'

const EMPTY_FORM: JailbreakProfileInput = {
  name: '',
  description: '',
  kind: 'prompt',
  prompt: '',
  headers: '',
  channel_models: {},
  mode: 'prepend',
  force: false,
  enabled: true,
  channels: [],
  models: [],
}

const MODE_LABELS: Record<JailbreakMode, string> = {
  prepend: 'Prepend',
  append: 'Append',
  replace: 'Replace',
}

const HEADER_TEMPLATE = JSON.stringify(
  { 'X-Trace-Mode': 'jailbreak' },
  null,
  2
)
const HEADER_PASSTHROUGH_TEMPLATE = JSON.stringify({ '*': true }, null, 2)

type ProfileDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  profile: JailbreakProfile | null
  onSaved: (profile: JailbreakProfile) => void
}

export function ProfileDrawer({
  open,
  onOpenChange,
  profile,
  onSaved,
}: ProfileDrawerProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [form, setForm] = useState<JailbreakProfileInput>(EMPTY_FORM)
  const [focusedGroup, setFocusedGroup] = useState('')
  const [focusedChannelId, setFocusedChannelId] = useState(0)
  const [channelFilter, setChannelFilter] = useState('')
  const [modelFilter, setModelFilter] = useState('')
  const [testChannelId, setTestChannelId] = useState(0)
  const [testModel, setTestModel] = useState('')
  const [testBasePrompt, setTestBasePrompt] = useState('')
  const [testBaseSource, setTestBaseSource] = useState<'channel' | 'token'>(
    'channel'
  )
  const [testUserMessage, setTestUserMessage] = useState('你好')
  const [testResult, setTestResult] = useState<JailbreakPreviewResult | null>(
    null
  )
  const [activeSection, setActiveSection] = useState('jailbreak-basics')
  const [showBackToTop, setShowBackToTop] = useState(false)

  const sectionItems: FloatingSectionItem[] = [
    { id: 'jailbreak-basics', title: t('Basics') },
    {
      id: 'jailbreak-content',
      title:
        form.kind === 'header'
          ? t('Header Modification')
          : t('Prompt Injection'),
    },
    { id: 'jailbreak-bindings', title: t('Bindings') },
    ...(profile
      ? [{ id: 'jailbreak-test', title: t('Test Request') }]
      : []),
  ]
  const [previewResult, setPreviewResult] = useState<JailbreakPreviewResult | null>(
    null
  )
  const [previewOpen, setPreviewOpen] = useState(false)

  const { data: channels = [] } = useQuery({
    queryKey: ['jailbreak-channel-options'],
    queryFn: async () => {
      const res = await getChannels({ p: 1, page_size: 200 })
      return res.data?.items ?? []
    },
  })

  const { data: modelOptions = [] } = useQuery({
    queryKey: ['jailbreak-model-options'],
    queryFn: async () => {
      const res = await getModels({ p: 1, page_size: 200 })
      return res.data?.items ?? []
    },
  })

  const testChannel = channels.find((channel) => channel.id === testChannelId)
  const testModelItems = useMemo(() => {
    const names = (testChannel?.models ?? '')
      .split(',')
      .map((name) => name.trim())
      .filter(Boolean)
    const fromList = modelOptions.map((model) => model.model_name)
    for (const name of fromList) {
      if (!names.includes(name)) {
        names.push(name)
      }
    }
    return names
  }, [testChannel, modelOptions])


  const buildChannelModels = (source: JailbreakProfile) => {
    const seeded: Record<string, string[]> = {}
    const stored = source.channel_models ?? {}
    for (const [id, names] of Object.entries(stored)) {
      if (names.length > 0) seeded[id] = [...names]
    }
    const legacyChannels = source.channels ?? []
    const legacyModels = source.models ?? []
    for (const id of legacyChannels) {
      const names = modelNamesFor(id)
      const chosen =
        legacyModels.length > 0
          ? names.filter((name) => legacyModels.includes(name))
          : names
      if (chosen.length > 0) seeded[String(id)] = chosen
    }
    if (legacyModels.length > 0) {
      for (const channel of channels) {
        const hits = modelNamesFor(channel.id).filter((name) =>
          legacyModels.includes(name)
        )
        if (hits.length > 0 && !seeded[String(channel.id)]) {
          seeded[String(channel.id)] = hits
        }
      }
    }
    return seeded
  }

  useEffect(() => {
    if (!open) return
    setForm(
      profile
        ? {
            name: profile.name,
            description: profile.description,
            kind: profile.kind,
            prompt: profile.prompt,
            headers: profile.headers,
            mode: profile.mode,
            force: profile.force,
            enabled: profile.enabled,
            channels: [],
            models: [],
            channel_models: buildChannelModels(profile),
          }
        : EMPTY_FORM
    )
    setChannelFilter('')
    setModelFilter('')
    const boundChannelIDs = Object.keys(profile?.channel_models ?? {})
    const defaultChannelId = Number(
      profile?.channels[0] ?? boundChannelIDs[0] ?? 0
    )
    const defaultChannel = channels.find((c) => c.id === defaultChannelId)
    const defaultModel =
      profile?.channel_models?.[String(defaultChannelId)]?.[0] ??
      profile?.models[0] ??
      (defaultChannel?.models ?? '').split(',')[0]?.trim() ??
      ''
    setFocusedChannelId(defaultChannelId)
    const defaultGroupChannel = channels.find((c) => c.id === defaultChannelId)
    setFocusedGroup(
      (defaultGroupChannel?.group || 'default')
        .split(',')[0]
        ?.trim() ?? ''
    )
    setTestChannelId(defaultChannelId)
    setTestModel(defaultModel)
    setTestBasePrompt('')
    setTestBaseSource('channel')
    setTestUserMessage('你好')
    setTestResult(null)
    setTestError(null)
  }, [open, profile, channels])

  useEffect(() => {
    if (!open || focusedChannelId !== 0 || channels.length === 0) return
    const bound = Object.keys(profile?.channel_models ?? {})
    const fallback = Number(bound[0] ?? profile?.channels?.[0] ?? channels[0].id)
    const channel = channels.find((c) => c.id === fallback) ?? channels[0]
    setFocusedChannelId(channel.id)
    setFocusedGroup(
      (channel.group || 'default').split(',')[0]?.trim() ?? ''
    )
  }, [open, channels, focusedChannelId, profile])

  const mutation = useMutation({
    mutationFn: (payload: JailbreakProfileInput) =>
      profile
        ? updateJailbreakProfile(profile.id, payload)
        : createJailbreakProfile(payload),
    onSuccess: (saved) => {
      toast.success(t('Saved'))
      queryClient.invalidateQueries({ queryKey: ['jailbreak-profiles'] })
      onSaved(saved)
    },
    onError: handleServerError,
  })

  const [testError, setTestError] = useState<string | null>(null)

  const buildPreviewPayload = () => ({
    kind: form.kind,
    channel_id: testChannelId,
    model: testModel,
    base_prompt: testBasePrompt,
    base_source: testBaseSource,
    base_override: false,
    user_message: testUserMessage,
    extra_kind: form.kind,
    extra_mode: form.mode,
    extra_prompt: form.kind === 'prompt' ? form.prompt : '',
    extra_headers: form.kind === 'header' ? form.headers : '',
    extra_name: t('Current rule (unsaved)'),
    exclude_rule_id: profile?.id ?? 0,
  })

  const testMutation = useMutation({
    mutationFn: () =>
      testJailbreakRequest({
        ...buildPreviewPayload(),
        user_message: testUserMessage,
      }),
    onSuccess: (data) => {
      if (!data) return
      const result: JailbreakPreviewResult = {
        kind: form.kind,
        rules: [],
        prompt: data.content,
        headers: null,
        override: false,
        source: 'response',
        finish_reason: data.finish_reason,
        prompt_tokens: data.prompt_tokens,
        completion_tokens: data.completion_tokens,
        messages: [
          ...(data.reasoning
            ? [{ role: 'reasoning', content: data.reasoning }]
            : []),
          { role: 'assistant', content: data.content },
        ],
      }
      setTestResult(result)
      setTestError(null)
    },
    onError: (error) => {
      setTestError(String(error))
      handleServerError(error)
    },
  })

  const previewMutation = useMutation({
    mutationFn: () => previewJailbreakPrompt(buildPreviewPayload()),
    onSuccess: (data) => {
      setPreviewResult(data)
      setPreviewOpen(true)
    },
    onError: handleServerError,
  })

  const update = <K extends keyof JailbreakProfileInput>(
    key: K,
    value: JailbreakProfileInput[K]
  ) => setForm((current) => ({ ...current, [key]: value }))

  const modelNamesFor = (channelId: number) => {
    const channel = channels.find((item) => item.id === channelId)
    return (channel?.models ?? '')
      .split(',')
      .map((name) => name.trim())
      .filter(Boolean)
  }

  const toggleModel = (name: string) =>
    setForm((current) => {
      const key = String(focusedChannelId)
      const selected = current.channel_models[key] ?? []
      const next = selected.includes(name)
        ? selected.filter((value) => value !== name)
        : [...selected, name]
      const channelModels = { ...current.channel_models }
      if (next.length > 0) {
        channelModels[key] = next
      } else {
        delete channelModels[key]
      }
      return { ...current, channel_models: channelModels }
    })

  const keyword = channelFilter.trim().toLowerCase()
  const visibleChannels = keyword
    ? channels.filter(
        (channel) =>
          String(channel.id).includes(keyword) ||
          channel.name.toLowerCase().includes(keyword)
      )
    : channels

  const groupOptions = Array.from(
    channels.reduce((map, channel) => {
      const groups = (channel.group || 'default')
        .split(',')
        .map((name) => name.trim())
        .filter(Boolean)
      for (const name of groups) {
        const bucket = map.get(name) ?? []
        bucket.push(channel)
        map.set(name, bucket)
      }
      return map
    }, new Map<string, typeof channels>())
  ).map(([name, list]) => ({ name, channels: list }))

  const activeGroup = focusedGroup || groupOptions[0]?.name || ''
  const visibleGroupChannels = visibleChannels.filter((channel) =>
    (channel.group || 'default')
      .split(',')
      .map((name) => name.trim())
      .includes(activeGroup)
  )

  const focusedChannel = channels.find(
    (channel) => channel.id === focusedChannelId
  )
  const focusedModelNames = modelNamesFor(focusedChannelId)
  const allFocusedSelected =
    focusedModelNames.length > 0 &&
    focusedModelNames.every((name) =>
      (form.channel_models[String(focusedChannelId)] ?? []).includes(name)
    )

  const modelKeyword = modelFilter.trim().toLowerCase()
  const visibleModels = modelKeyword
    ? focusedModelNames.filter((name) =>
        name.toLowerCase().includes(modelKeyword)
      )
    : focusedModelNames

  const isHeaderKind = form.kind === 'header'

  const getScrollContainer = () =>
    document
      .querySelector<HTMLElement>('#jailbreak-basics')
      ?.closest<HTMLElement>('[data-slot="sheet-content"]') ?? null

  const navigateToSection = (id: string) => {
    const container = getScrollContainer()
    const element = container?.querySelector<HTMLElement>(`#${id}`)
    if (!container || !element) return
    const top =
      element.getBoundingClientRect().top -
      container.getBoundingClientRect().top +
      container.scrollTop -
      16
    container.scrollTo({ top: Math.max(0, top), behavior: 'smooth' })
    setActiveSection(id)
  }

  const httpPreviewText = (result: JailbreakPreviewResult) => {
    const headerLines = [
      'POST /v1/chat/completions HTTP/1.1',
      `Host: ${testChannel?.base_url || 'upstream'}`,
      'Content-Type: application/json',
      'Authorization: Bearer <redacted>',
      ...Object.entries(result.headers ?? {})
        .filter(([key]) => !key.startsWith('re:'))
        .map(([key, value]) => `${key}: ${value}`),
    ].join('\n')
    const body = JSON.stringify(
      { model: testModel, messages: result.messages ?? [], stream: false },
      null,
      2
    )
    return `${headerLines}\n\n${body}`
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        className='w-full rounded-md overflow-y-auto sm:max-w-2xl'
        onScroll={(event) => {
          const container = event.currentTarget as HTMLElement
          setShowBackToTop(container.scrollTop > 240)
          const containerTop = container.getBoundingClientRect().top
          let next = 'jailbreak-basics'
          for (const item of sectionItems) {
            const element = container.querySelector<HTMLElement>(`#${item.id}`)
            if (!element) continue
            if (element.getBoundingClientRect().top - containerTop <= 96) {
              next = item.id
            }
          }
          setActiveSection(next)
        }}
      >
        <SheetHeader>
          <SheetTitle>
            {profile ? t('Edit Request Rule') : t('Create Request Rule')}
          </SheetTitle>
          <SheetDescription>
            {t(
              'Rules rewrite the system prompt or upstream request headers for bound channels and models. Channel settings stay untouched.'
            )}
          </SheetDescription>
        </SheetHeader>

        <div className='space-y-6 px-4 pb-6'>
          <section id='jailbreak-basics' className='scroll-mt-4 space-y-3'>
            <h3 className='text-primary text-xs font-semibold tracking-wide uppercase'>
              {t('Basics')}
            </h3>
            <div className='grid gap-3 sm:grid-cols-2'>
              <div className='space-y-2'>
                <Label htmlFor='jailbreak-name'>{t('Name')}</Label>
                <Input
                  id='jailbreak-name'
                  value={form.name}
                  maxLength={64}
                  onChange={(event) => update('name', event.target.value)}
                  placeholder={t('e.g. OpenAI jailbreak v7')}
                />
              </div>
              <div className='space-y-2 sm:col-span-2'>
                <Label htmlFor='jailbreak-description'>{t('Description')}</Label>
                <Input
                  id='jailbreak-description'
                  value={form.description}
                  maxLength={255}
                  onChange={(event) => update('description', event.target.value)}
                  placeholder={t('Optional note for operators')}
                />
              </div>
            </div>
            <label className='flex items-center justify-between rounded-lg border p-3 text-sm'>
              <span>{t('Enabled')}</span>
              <Switch
                checked={form.enabled}
                onCheckedChange={(value) => update('enabled', value)}
              />
            </label>
          </section>

          <section id='jailbreak-content' className='scroll-mt-4 space-y-3'>
            <h3 className='text-primary text-xs font-semibold tracking-wide uppercase'>
              {isHeaderKind
                ? t('Header Modification')
                : t('Prompt Injection')}
            </h3>
                <Tabs
                  value={isHeaderKind ? 'header' : form.mode}
                  onValueChange={(value) => {
                    setForm((current) =>
                      value === 'header'
                        ? { ...current, kind: 'header' }
                        : {
                            ...current,
                            kind: 'prompt',
                            mode: value as JailbreakMode,
                          }
                    )
                    setTestResult(null)
                  }}
                >
                  <TabsList className='grid h-auto! w-full grid-cols-4 gap-1 p-1'>
                    <TabsTrigger value='prepend' className='text-xs'>
                      {t(MODE_LABELS.prepend)}
                    </TabsTrigger>
                    <TabsTrigger value='append' className='text-xs'>
                      {t(MODE_LABELS.append)}
                    </TabsTrigger>
                    <TabsTrigger value='replace' className='text-xs'>
                      {t(MODE_LABELS.replace)}
                    </TabsTrigger>
                    <TabsTrigger value='header' className='text-xs'>
                      {t('Header Modification')}
                    </TabsTrigger>
                  </TabsList>
                </Tabs>
            <p className='text-muted-foreground text-xs'>
              {isHeaderKind
                ? t('Modify upstream request headers using this JSON.')
                : form.mode === 'prepend'
                  ? t('Prepend this rule before the existing system prompt.')
                  : form.mode === 'append'
                    ? t('Append this rule after the existing system prompt.')
                    : t('Replace the existing system prompt with this rule.')}
            </p>
            {isHeaderKind ? (
              <>
                <div className='flex flex-wrap gap-2'>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => update('headers', HEADER_TEMPLATE)}
                  >
                    {t('Fill example')}
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => update('headers', HEADER_PASSTHROUGH_TEMPLATE)}
                  >
                    {t('Passthrough template')}
                  </Button>
                </div>
                <Textarea
                  value={form.headers}
                  rows={8}
                  className='font-mono text-xs'
                  onChange={(event) => update('headers', event.target.value)}
                  placeholder={t('{"X-Custom": "value", "*": true}')}
                />
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Same JSON format as the channel header override. "*" passes all client headers through; "re:<regex>" passes matching header names; values support {api_key} and {client_header:NAME}.'
                  )}
                </p>
              </>
            ) : (
              <>
                <Textarea
                  value={form.prompt}
                  rows={8}
                  className='font-mono text-xs'
                  onChange={(event) => update('prompt', event.target.value)}
                  placeholder={t('Prompt text injected into matching requests')}
                />
              </>
            )}
          </section>

          <section id='jailbreak-bindings' className='scroll-mt-4 space-y-3'>
            <h3 className='text-primary text-xs font-semibold tracking-wide uppercase'>
              {t('Bindings')}
            </h3>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Bound channels and models receive this rule automatically. Leave both empty to disable the rule.'
              )}
            </p>
            <div className='grid min-w-0 gap-3 md:grid-cols-[150px_210px_1fr]'>
              <div className='min-w-0 space-y-2'>
                <div className='flex items-center justify-between'>
                  <span className='text-sm font-medium'>{t('Groups')}</span>
                  <Badge variant='outline'>{groupOptions.length}</Badge>
                </div>
                <div className='max-h-64 min-w-0 space-y-1 overflow-x-hidden overflow-y-auto rounded-lg border p-2'>
                  {groupOptions.map((group) => (
                    <button
                      key={group.name}
                      type='button'
                      onClick={() => {
                        setFocusedGroup(group.name)
                        if (!group.channels.some((c) => c.id === focusedChannelId)) {
                          const first = group.channels[0]
                          if (first) setFocusedChannelId(first.id)
                        }
                      }}
                      className={`flex w-full items-center justify-between gap-2 rounded-md px-2 py-1.5 text-start text-sm transition-colors hover:bg-muted ${
                        group.name === focusedGroup ? 'bg-muted' : ''
                      }`}
                    >
                      <span className='truncate'>{group.name}</span>
                      <span className='text-muted-foreground text-xs'>
                        {group.channels.length}
                      </span>
                    </button>
                  ))}
                </div>
              </div>

              <div className='min-w-0 space-y-2'>
                <div className='flex items-center justify-between'>
                  <span className='text-sm font-medium'>{t('Channels')}</span>
                  <Badge variant='outline'>
                    {Object.keys(form.channel_models).length}
                  </Badge>
                </div>
                <Input
                  value={channelFilter}
                  onChange={(event) => setChannelFilter(event.target.value)}
                  placeholder={t('Filter channels')}
                />
                <div className='max-h-64 min-w-0 space-y-1 overflow-x-hidden overflow-y-auto rounded-lg border p-2'>
                  {visibleGroupChannels.length === 0 ? (
                    <p className='text-muted-foreground p-2 text-xs'>
                      {t('No channels found')}
                    </p>
                  ) : (
                    visibleGroupChannels.map((channel) => {
                      const hasSelectedModels =
                        (form.channel_models[String(channel.id)]?.length ?? 0) > 0
                      return (
                        <button
                          key={channel.id}
                          type='button'
                          onClick={() => setFocusedChannelId(channel.id)}
                          className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-start text-sm transition-colors hover:bg-muted ${
                            channel.id === focusedChannelId ? 'bg-muted' : ''
                          }`}
                        >
                          <span
                            className={`size-2 shrink-0 rounded-full ${
                              hasSelectedModels
                                ? 'bg-primary'
                                : 'bg-muted-foreground/30'
                            }`}
                          />
                          <span className='truncate'>
                            #{channel.id} {channel.name}
                          </span>
                        </button>
                      )
                    })
                  )}
                </div>
              </div>

              <div className='min-w-0 space-y-2'>
                <div className='flex flex-wrap items-center justify-between gap-2'>
                  <span className='text-sm font-medium'>
                    {t('Models')}
                    {focusedChannel ? ` · #${focusedChannel.id}` : ''}
                  </span>
                  <div className='flex items-center gap-2'>
                    <Badge variant='outline'>
                      {Object.values(form.channel_models).reduce(
                        (total, names) => total + names.length,
                        0
                      )}
                    </Badge>
                    <label className='flex items-center gap-1.5 text-xs'>
                      <span>{t('Select all')}</span>
                      <Switch
                        checked={allFocusedSelected}
                        disabled={focusedModelNames.length === 0}
                        onCheckedChange={(checked) => {
                          setForm((current) => {
                            const key = String(focusedChannelId)
                            const channelModels = { ...current.channel_models }
                            if (checked) {
                              channelModels[key] = [...focusedModelNames]
                            } else {
                              delete channelModels[key]
                            }
                            return { ...current, channel_models: channelModels }
                          })
                        }}
                      />
                    </label>
                  </div>
                </div>
                <Input
                  value={modelFilter}
                  onChange={(event) => setModelFilter(event.target.value)}
                  placeholder={t('Filter models')}
                />
                <div className='max-h-64 min-w-0 space-y-1 overflow-x-hidden overflow-y-auto rounded-lg border p-2'>
                  {focusedModelNames.length === 0 ? (
                    <p className='text-muted-foreground p-2 text-xs'>
                      {t('Select a channel to view its models')}
                    </p>
                  ) : visibleModels.length === 0 ? (
                    <p className='text-muted-foreground p-2 text-xs'>
                      {t('No models found')}
                    </p>
                  ) : (
                    visibleModels.map((name) => (
                      <label
                        key={name}
                        className='hover:bg-muted flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm'
                      >
                        <Checkbox
                          checked={(
                            form.channel_models[String(focusedChannelId)] ?? []
                          ).includes(name)}
                          onCheckedChange={() => toggleModel(name)}
                        />
                        <span className='truncate font-mono text-xs'>{name}</span>
                      </label>
                    ))
                  )}
                </div>
              </div>
            </div>
          </section>

          {profile ? (
            <section id='jailbreak-test' className='scroll-mt-4 space-y-3 rounded-md border p-3'>
              <h3 className='text-primary text-xs font-semibold tracking-wide uppercase'>
                {t('Test Request')}
              </h3>
              <p className='text-muted-foreground text-xs'>
                {t('Preview and test use the current form values, including unsaved edits.')}
              </p>
              <div className='grid gap-3 sm:grid-cols-2'>
                <div className='space-y-2'>
                  <Label>{t('Channel')}</Label>
                  <Select
                    items={[
                      { value: '0', label: t('No channel') },
                      ...channels.map((channel) => ({
                        value: String(channel.id),
                        label: `#${channel.id} ${channel.name}`,
                      })),
                    ]}
                    value={String(testChannelId)}
                    onValueChange={(value) => {
                      const next = Number(value)
                      setTestChannelId(next)
                      const nextChannel = channels.find(
                        (channel) => channel.id === next
                      )
                      const firstModel = (nextChannel?.models ?? '')
                        .split(',')[0]
                        ?.trim()
                      setTestModel(firstModel ?? '')
                      setTestResult(null)
                    }}
                  >
                    <SelectTrigger className='w-full'>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        <SelectItem value='0'>{t('No channel')}</SelectItem>
                        {channels.map((channel) => (
                          <SelectItem key={channel.id} value={String(channel.id)}>
                            #{channel.id} {channel.name}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </div>
                <div className='space-y-2'>
                  <Label>{t('Model')}</Label>
                  <Select
                    items={testModelItems.map((name) => ({
                      value: name,
                      label: name,
                    }))}
                    value={testModel}
                    onValueChange={(value) => {
                      setTestModel(value ?? '')
                      setTestResult(null)
                    }}
                  >
                    <SelectTrigger className='w-full'>
                      <SelectValue placeholder={t('Choose a model')} />
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        {testModelItems.length === 0 ? (
                          <p className='text-muted-foreground p-2 text-xs'>
                            {t('This channel has no models')}
                          </p>
                        ) : (
                          testModelItems.map((name) => (
                            <SelectItem key={name} value={name}>
                              {name}
                            </SelectItem>
                          ))
                        )}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </div>
              </div>

              {profile.kind === 'prompt' ? (
                <>
                  <div className='space-y-2'>
                    <Label>{t('Simulated base system prompt')}</Label>
                    <Textarea
                      value={testBasePrompt}
                      rows={3}
                      className='font-mono text-xs'
                      onChange={(event) => setTestBasePrompt(event.target.value)}
                      placeholder={t(
                        'Optional: an existing channel or token prompt'
                      )}
                    />
                  </div>
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Base system prompt simulates what the channel already sends; the rule is applied on top of it.'
                    )}
                  </p>
                </>
              ) : null}

              <div className='space-y-2'>
                <Label>{t('Test user message')}</Label>
                <Textarea
                  value={testUserMessage}
                  rows={3}
                  onChange={(event) => {
                    setTestUserMessage(event.target.value)
                    setTestResult(null)
                  }}
                  placeholder={t('Type the user message to test')}
                />
              </div>

              <div className='flex flex-wrap gap-2'>
                <Button
                  disabled={
                    previewMutation.isPending || testModel.trim() === ''
                  }
                  onClick={() => previewMutation.mutate()}
                >
                  {previewMutation.isPending ? (
                    <Loader2 className='size-4 animate-spin' />
                  ) : null}
                  {t('Preview Request')}
                </Button>
                <Button
                  disabled={testMutation.isPending || testModel.trim() === ''}
                  onClick={() => testMutation.mutate()}
                >
                  {testMutation.isPending ? (
                    <Loader2 className='size-4 animate-spin' />
                  ) : null}
                  {t('Run test')}
                </Button>
              </div>

              {testError ? (
                <div data-testid='jb-error' className='rounded-lg border border-destructive/40 p-2 text-xs text-destructive'>
                  {testError}
                </div>
              ) : null}

              {testResult ? (
                <div className='space-y-3 rounded-lg border p-3'>
                  <div className='flex flex-wrap items-center gap-2'>
                    <span className='text-sm font-medium'>
                      {testResult.source === 'response'
                        ? t('Response')
                        : t('Matched rules')}
                    </span>
                    {(testResult.rules ?? []).length === 0
                      ? null
                      : (testResult.rules ?? []).map((rule) => (
                          <Badge key={rule}>{rule}</Badge>
                        ))}
                    {testResult.kind === 'prompt' && testResult.prompt_tokens > 0 ? (
                      <Badge variant='secondary'>
                        {t('Prompt tokens')} {testResult.prompt_tokens}
                      </Badge>
                    ) : null}
                    {testResult.completion_tokens ? (
                      <Badge variant='secondary'>
                        {t('Completion tokens')} {testResult.completion_tokens}
                      </Badge>
                    ) : null}
                    {testResult.override ? (
                      <Badge variant='warning'>
                        {t('Replaces original prompt')}
                      </Badge>
                    ) : null}
                  </div>

                  {testResult.kind === 'header' ? (
                    Object.keys(testResult.headers ?? {}).length === 0 ? (
                      <p className='text-muted-foreground text-sm'>
                        {t('No headers would be applied.')}
                      </p>
                    ) : (
                      <pre className='font-mono text-xs break-words whitespace-pre-wrap rounded-lg border p-2'>
                        {JSON.stringify(testResult.headers, null, 2)}
                      </pre>
                    )
                  ) : testResult.messages.length === 0 ? (
                    <p className='text-muted-foreground text-sm'>
                      {t('Nothing would be injected for this channel and model.')}
                    </p>
                  ) : (
                    <div className='space-y-2'>
                      {testResult.messages.map((message, index) =>
                        message.role === 'reasoning' ? (
                          <details
                            key={index}
                            className='rounded-lg border p-2'
                          >
                            <summary className='cursor-pointer text-xs font-semibold text-muted-foreground'>
                              {t('Reasoning')}
                            </summary>
                            <pre className='mt-2 font-mono text-xs break-words whitespace-pre-wrap text-muted-foreground'>
                              {message.content}
                            </pre>
                          </details>
                        ) : (
                          <div key={index} className='rounded-lg border p-2'>
                            <div className='text-muted-foreground mb-1 text-xs font-semibold uppercase'>
                              {message.role}
                            </div>
                            {message.content ? (
                              <pre className='font-mono text-xs break-words whitespace-pre-wrap'>
                                {message.content}
                              </pre>
                            ) : (
                              <p className='text-sm text-destructive'>
                                {t(
                                  'The upstream returned no content (only reasoning or an empty response).'
                                )}
                              </p>
                            )}
                          </div>
                        )
                      )}
                    </div>
                  )}
                </div>
              ) : null}
            </section>
          ) : (
            <p className='text-muted-foreground text-xs'>
              {t(
                'Save the rule first, then test it here against a channel and model.'
              )}
            </p>
          )}

          <Dialog open={previewOpen} onOpenChange={setPreviewOpen}>
            <DialogContent className='max-h-[80vh] w-full overflow-y-auto rounded-md sm:max-w-2xl'>
              <DialogHeader>
                <DialogTitle>{t('Request Preview')}</DialogTitle>
                <DialogDescription>
                  {t(
                    'Resolved upstream HTTP request, including headers and body with the system prompt and user message.'
                  )}
                </DialogDescription>
              </DialogHeader>
              {previewResult ? (
                <pre className='overflow-x-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs whitespace-pre'>
                  {httpPreviewText(previewResult)}
                </pre>
              ) : (
                <p className='text-muted-foreground text-sm'>
                  {t('No response yet')}
                </p>
              )}
            </DialogContent>
          </Dialog>


          <div className='flex flex-col-reverse gap-2 pt-2 sm:flex-row sm:justify-end'>
            <Button variant='outline' onClick={() => onOpenChange(false)}>
              {t('Cancel')}
            </Button>
            <Button
              disabled={
                mutation.isPending ||
                form.name.trim() === '' ||
                (isHeaderKind
                  ? form.headers.trim() === ''
                  : form.prompt.trim() === '')
              }
              onClick={() => mutation.mutate(form)}
            >
              {profile ? t('Save changes') : t('Create')}
            </Button>
          </div>
        </div>
      </SheetContent>
      {open ? (
        <FloatingSectionNav
          items={sectionItems}
          activeItemId={activeSection}
          onNavigate={navigateToSection}
          navigationLabel={t('Request Rules')}
        />
      ) : null}
      <FloatingBackToTop
        visible={open && showBackToTop}
        onClick={() => getScrollContainer()?.scrollTo({ top: 0, behavior: 'smooth' })}
        label={t('Back to top')}
      />
    </Sheet>
  )
}
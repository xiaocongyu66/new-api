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
import { refreshWallpaperBackground } from '@/features/auth/lib/dynamic-background'
import { zodResolver } from '@hookform/resolvers/zod'
import { RefreshCw } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { TagInput } from '@/components/tag-input'
import { Button } from '@/components/ui/button'
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
  SettingsForm,
  SettingsFormGrid,
  SettingsFormGridItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { RECOMMENDED_WALLPAPER_APIS } from './wallpaper-defaults'

const DEFAULT_KEYWORDS = [
  'chiikawa',
  'cinnamoroll',
  'kuromi',
  'my melody',
  'pompompurin',
  'pochacco',
  'keroppi',
  'tuxedosam',
  'chococat',
  'pikachu',
  'eevee',
  'rilakkuma',
]

const backgroundSchema = z.object({
  // 多地址有序回退：按顺序尝试，第一个返回真实图片的生效。
  imageUrls: z.array(z.string()),
  keywords: z.array(z.string()),
  category: z.string(),
  refreshInterval: z.string(),
  sourcePriority: z.enum(['url', 'local']),
})

type BackgroundFormValues = z.infer<typeof backgroundSchema>

type BackgroundSectionProps = {
  defaultImageUrl: string
  defaultKeywords: string
  defaultCategory: string
  defaultRefreshInterval: string
  defaultSourcePriority: string
}

function parseImageUrls(raw: string | undefined): string[] {
  const parts = (raw ?? '')
    .split(/[\n,;]/)
    .map((part) => part.trim())
    .filter(Boolean)
  return parts.length > 0 ? parts : ['']
}

function parseKeywords(raw: string | undefined): string[] {
  const parts = (raw ?? '')
    .split(/[,;\n]/)
    .map((part) => part.trim())
    .filter(Boolean)
  return parts.length > 0 ? parts : [...DEFAULT_KEYWORDS]
}

export function BackgroundSection({
  defaultImageUrl,
  defaultKeywords,
  defaultCategory,
  defaultRefreshInterval,
  defaultSourcePriority,
}: BackgroundSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [isRefreshing, setIsRefreshing] = useState(false)

  const defaults: BackgroundFormValues = {
    imageUrls: parseImageUrls(defaultImageUrl),
    keywords: parseKeywords(defaultKeywords),
    category: defaultCategory ?? '',
    refreshInterval: defaultRefreshInterval ?? '60',
    sourcePriority: defaultSourcePriority === 'local' ? 'local' : 'url',
  }

  const form = useForm<BackgroundFormValues>({
    resolver: zodResolver(backgroundSchema),
    defaultValues: defaults,
  })

  useEffect(() => {
    form.reset({
      imageUrls: parseImageUrls(defaultImageUrl),
      keywords: parseKeywords(defaultKeywords),
      category: defaultCategory ?? '',
      refreshInterval: defaultRefreshInterval ?? '60',
      sourcePriority: defaultSourcePriority === 'local' ? 'local' : 'url',
    })
    // 依赖是稳定的字符串 props，避免父组件重渲染时用新对象打断输入。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [defaultImageUrl, defaultKeywords, defaultCategory, defaultRefreshInterval, defaultSourcePriority, form])

  const onRefresh = async () => {
    setIsRefreshing(true)
    try {
      // 原地换图：不 reload 页面，避免真实登录态被重置。
      await refreshWallpaperBackground()
    } finally {
      setIsRefreshing(false)
    }
  }

  const onSubmit = async (values: BackgroundFormValues) => {
    const keywords = values.keywords.join(', ')
    const updates = [
      {
        key: 'wallpaper.image_url',
        value: values.imageUrls
          .map((url) => url.trim())
          .filter(Boolean)
          .join('\n'),
        previous: defaultImageUrl,
      },
      { key: 'wallpaper.keywords', value: keywords, previous: defaultKeywords },
      { key: 'wallpaper.category', value: values.category.trim(), previous: defaultCategory },
      { key: 'wallpaper.refresh_interval', value: values.refreshInterval.trim(), previous: defaultRefreshInterval },
      { key: 'wallpaper.source_priority', value: values.sourcePriority, previous: defaultSourcePriority },
    ]
    for (const entry of updates) {
      if (entry.value === (entry.previous ?? '')) continue
      await updateOption.mutateAsync({ key: entry.key, value: entry.value })
    }
  }

  return (
    <SettingsSection title={t('Background settings')}>
      <Form {...form}>
        <SettingsForm
          onSubmit={form.handleSubmit(onSubmit)}
          className='rounded-xl bg-card-surface p-4 backdrop-blur-card'
        >
          <SettingsPageFormActions
            beforeActions={
              <>
                {/* 来源优先级状态按钮：URL 优先 / 本地缓存优先 */}
                <Button
                  type='button'
                  size='sm'
                  variant={form.watch('sourcePriority') === 'local' ? 'default' : 'outline'}
                  title={t('Background source priority help')}
                  aria-pressed={form.watch('sourcePriority') === 'local'}
                  onClick={() =>
                    form.setValue('sourcePriority',
                      form.watch('sourcePriority') === 'local' ? 'url' : 'local',
                      { shouldDirty: true })
                  }
                >
                  {form.watch('sourcePriority') === 'local'
                    ? t('Source: Local cache')
                    : t('Source: Online API')}
                </Button>
                <Button
                  type='button'
                  size='sm'
                  variant='outline'
                  onClick={onRefresh}
                  disabled={isRefreshing}
                >
                  <RefreshCw
                    data-icon='inline-start'
                    className={isRefreshing ? 'animate-spin' : undefined}
                  />
                  <span>{t('Refresh background')}</span>
                </Button>
                <Button
                  type='button'
                  size='sm'
                  variant='outline'
                  onClick={() =>
                    form.setValue('imageUrls', [...RECOMMENDED_WALLPAPER_APIS], { shouldDirty: true })
                  }
                >
                  {t('Use recommended wallpaper APIs')}
                </Button>
              </>
            }
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save background settings'
          />
          <SettingsFormGrid>
            <SettingsFormGridItem span='full'>
              <FormField
                control={form.control}
                name='imageUrls'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Wallpaper API URLs')}</FormLabel>
                    <FormControl>
                      <div className='space-y-2'>
                        {field.value.map((url, index) => (
                          <div key={index} className='flex items-center gap-2'>
                            <span className='text-muted-foreground w-6 shrink-0 text-right text-xs tabular-nums'>
                              {index + 1}
                            </span>
                            <Input
                              value={url}
                              placeholder='gelbooru|https://...tags={keyword}'
                              onChange={(event) => {
                                const next = [...field.value]
                                next[index] = event.target.value
                                field.onChange(next)
                              }}
                              onBlur={field.onBlur}
                            />
                            <Button
                              type='button'
                              variant='outline'
                              size='sm'
                              disabled={field.value.length <= 1}
                              onClick={() =>
                                field.onChange(
                                  field.value.filter((_, i) => i !== index)
                                )
                              }
                            >
                              aria-label={`${t('Remove')} ${index + 1}`}
                              {t('Remove')}
                            </Button>
                          </div>
                        ))}
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          onClick={() => field.onChange([...field.value, ''])}
                        >
                          {t('Add URL')}
                        </Button>
                      </div>
                    </FormControl>
                    <FormDescription>{t('Wallpaper API URLs help')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SettingsFormGridItem>
            <SettingsFormGridItem span='full'>
              <FormField
                control={form.control}
                name='keywords'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Wallpaper keywords')}</FormLabel>
                    <FormControl>
                      <TagInput
                        value={field.value}
                        onChange={field.onChange}
                        placeholder={t('Add keywords...')}
                      />
                    </FormControl>
                    <FormDescription>{t('Wallpaper keywords help')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SettingsFormGridItem>
            <SettingsFormGridItem>
              <FormField
                control={form.control}
                name='category'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Wallpaper category')}</FormLabel>
                    <FormControl>
                      <Input placeholder='anime' {...field} />
                    </FormControl>
                    <FormDescription>{t('Wallpaper category help')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SettingsFormGridItem>
            <SettingsFormGridItem>
              <FormField
                control={form.control}
                name='refreshInterval'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Auto refresh interval (minutes)')}</FormLabel>
                    <FormControl>
                      <Input type='number' min={1} max={1440} {...field} />
                    </FormControl>
                    <FormDescription>{t('Auto refresh interval help')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SettingsFormGridItem>
          </SettingsFormGrid>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}

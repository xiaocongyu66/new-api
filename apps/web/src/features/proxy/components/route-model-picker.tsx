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
import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { getChannels } from '@/features/channels/api'
import { getModels } from '@/features/models/api'
import {
  TabbedPaginatedSelector,
  type TabCategory,
} from './tabbed-paginated-selector'

export type RouteModelPickerProps = {
  selectedIds: Set<string>
  onToggle: (id: string) => void
  pageSize?: number
}

export function RouteModelPicker(props: RouteModelPickerProps) {
  const { t } = useTranslation()

  // 1. 获取启用的渠道列表
  const channelsQuery = useQuery({
    queryKey: ['channels', 'proxy-picker'],
    queryFn: async () => {
      const res = await getChannels({ p: 1, page_size: 500 })
      return res.data?.items ?? []
    },
    staleTime: 60 * 1000,
  })

  // 2. 获取模型广场的模型列表
  const modelsQuery = useQuery({
    queryKey: ['models', 'proxy-picker'],
    queryFn: async () => {
      const res = await getModels({ p: 1, page_size: 500 })
      return res.data?.items ?? []
    },
    staleTime: 60 * 1000,
  })

  // 3. 解析模型映射分类 (Model Mappings)
  const modelMappingItems = useMemo(() => {
    const set = new Set<string>()
    const items: TabCategory['items'] = []

    // 优先从模型广场列表中获取
    const modelsList = modelsQuery.data ?? []
    for (const m of modelsList) {
      const name = m.model_name?.trim()
      if (name && !set.has(name)) {
        set.add(name)
        items.push({
          id: `m:${name}`,
          label: name,
        })
      }
    }

    // 同时补充渠道中声明的 models 及 model_mapping 源模型
    const channelsList = channelsQuery.data ?? []
    for (const ch of channelsList) {
      if (ch.models) {
        for (const raw of ch.models.split(',')) {
          const m = raw.trim()
          if (m && !set.has(m)) {
            set.add(m)
            items.push({ id: `m:${m}`, label: m })
          }
        }
      }
      if (ch.model_mapping) {
        try {
          const mapping = JSON.parse(ch.model_mapping)
          if (mapping && typeof mapping === 'object') {
            for (const key of Object.keys(mapping)) {
              const src = key.trim()
              if (src && !set.has(src)) {
                set.add(src)
                items.push({ id: `m:${src}`, label: src })
              }
            }
          }
        } catch {
          // Ignore invalid JSON
        }
      }
    }

    return items
  }, [modelsQuery.data, channelsQuery.data])

  // 4. 解析配置好的渠道分类 (Channels)
  const channelItems = useMemo(() => {
    const channelsList = channelsQuery.data ?? []
    return channelsList
      .filter((ch) => ch.status === 1) // 仅启用的渠道
      .map((ch) => ({
        id: `c:${ch.id}`,
        label: ch.name || `Channel #${ch.id}`,
        sublabel: String(ch.type),
      }))
  }, [channelsQuery.data])

  const categories: TabCategory[] = useMemo(
    () => [
      {
        id: 'channel',
        label: t('Channels'),
        items: channelItems,
      },
      {
        id: 'model_alias',
        label: t('Model Alias'),
        items: modelMappingItems,
      },
    ],
    [t, channelItems, modelMappingItems]
  )

  return (
    <section
      aria-label={t('Scope')}
      className='bg-muted/10 rounded-lg border p-3 flex flex-col'
    >
      <h3 className='text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2'>
        {t('Scope')}
      </h3>

      <TabbedPaginatedSelector
        categories={categories}
        selectedIds={props.selectedIds}
        onToggle={props.onToggle}
        pageSize={props.pageSize ?? 30}
      />
    </section>
  )
}

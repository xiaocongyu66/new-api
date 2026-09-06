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
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { evidenceKindLabel, evidenceTagLabel } from '../lib/labels'
import type { InsightEvidence, InsightEvidenceKind } from '../types'

/** 证据分组的展示顺序：破甲最重要，客户端指纹最次要。 */
const KIND_ORDER: InsightEvidenceKind[] = [
  'jailbreak',
  'gender',
  'stack',
  'code',
  'roleplay',
  'qa',
  'translate',
  'client',
]

type EvidenceListProps = {
  items: InsightEvidence[]
}

/**
 * 按判定类别分组展示命中证据。
 *
 * 每条证据显示：命中的关键词、命中位置（系统提示词 / 用户对话）、
 * 以及关键词所在的原句片段，片段中的关键词做高亮。
 * 这是"人工复核"的核心视图——管理员据此判断分数是否可信。
 */
export function EvidenceList({ items }: EvidenceListProps) {
  const { t } = useTranslation()

  const groups = useMemo(() => {
    const map = new Map<InsightEvidenceKind, InsightEvidence[]>()
    for (const item of items) {
      const list = map.get(item.kind)
      if (list) {
        list.push(item)
      } else {
        map.set(item.kind, [item])
      }
    }
    return KIND_ORDER.filter((kind) => map.has(kind)).map((kind) => ({
      kind,
      items: map.get(kind) as InsightEvidence[],
    }))
  }, [items])

  if (items.length === 0) {
    return (
      <p className='text-muted-foreground py-6 text-center text-sm'>
        {t('No keyword hits recorded for this request')}
      </p>
    )
  }

  return (
    <div className='space-y-4'>
      {groups.map((group) => (
        <div key={group.kind} className='space-y-2'>
          <div className='flex items-center gap-2'>
            <h4 className='text-sm font-medium'>
              {evidenceKindLabel(group.kind, t)}
            </h4>
            <span className='text-muted-foreground text-xs tabular-nums'>
              {group.items.length}
            </span>
          </div>
          <div className='space-y-2'>
            {group.items.map((item, index) => (
              <div
                key={`${item.kind}-${item.section}-${item.keyword}-${index}`}
                className='bg-muted/40 space-y-1.5 rounded-md p-3'
              >
                <div className='flex flex-wrap items-center gap-1.5'>
                  <Badge variant='secondary' className='font-mono text-[11px]'>
                    {item.keyword}
                  </Badge>
                  {item.tag && (
                    <Badge variant='outline' className='text-[11px]'>
                      {evidenceTagLabel(item.tag, t)}
                    </Badge>
                  )}
                  <Badge
                    variant={
                      item.section === 'system' ? 'destructive' : 'outline'
                    }
                    className='text-[11px]'
                  >
                    {item.section === 'system'
                      ? t('System prompt')
                      : t('User conversation')}
                  </Badge>
                </div>
                <p className='text-xs leading-relaxed break-words whitespace-pre-wrap'>
                  <HighlightedSnippet
                    snippet={item.snippet}
                    keyword={item.keyword}
                  />
                </p>
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

/**
 * 在原句片段中高亮关键词。
 *
 * 后端匹配是大小写无关的，所以这里也用不区分大小写的方式定位，
 * 但渲染时保留原文大小写，避免管理员看到被改写过的句子。
 */
function HighlightedSnippet({
  snippet,
  keyword,
}: {
  snippet: string
  keyword: string
}) {
  if (!keyword) return <>{snippet}</>
  const lowerSnippet = snippet.toLowerCase()
  const lowerKeyword = keyword.toLowerCase()
  const parts: Array<{ text: string; hit: boolean }> = []

  let cursor = 0
  while (cursor < snippet.length) {
    const index = lowerSnippet.indexOf(lowerKeyword, cursor)
    if (index < 0) {
      parts.push({ text: snippet.slice(cursor), hit: false })
      break
    }
    if (index > cursor) {
      parts.push({ text: snippet.slice(cursor, index), hit: false })
    }
    parts.push({
      text: snippet.slice(index, index + keyword.length),
      hit: true,
    })
    cursor = index + keyword.length
  }

  return (
    <>
      {parts.map((part, index) =>
        part.hit ? (
          <mark
            key={index}
            className='bg-yellow-200 font-medium text-black dark:bg-yellow-500/40 dark:text-yellow-50'
          >
            {part.text}
          </mark>
        ) : (
          <span key={index}>{part.text}</span>
        )
      )}
    </>
  )
}

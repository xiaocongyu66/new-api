import { useMutation } from '@tanstack/react-query'
import { Loader2, Plus } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { toast } from '@/hooks/use-toast'

import { RouteModelPicker } from './components/route-model-picker'
import { createProxyNodesBatch } from './proxy-node-api'
import type {
  ProxyNodeBatchRequest,
  ProxyNodeBatchResult,
} from './proxy-node-types'

const createDefaultBatch = (): ProxyNodeBatchRequest => ({
  name_prefix: '',
  enabled: true,
  proxy_text: '',
  scope_type: 'custom',
  scope_value: '',
})

export function ProxyNodeQuickAdd(props: { onAdded: () => void }) {
  const { t } = useTranslation()
  const [batch, setBatch] = useState<ProxyNodeBatchRequest>(createDefaultBatch)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(() => new Set())

  const handleToggle = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const mutation = useMutation({
    mutationFn: (payload: ProxyNodeBatchRequest) =>
      createProxyNodesBatch(payload),
    onSuccess: (result: ProxyNodeBatchResult) => {
      setBatch(createDefaultBatch())
      setSelectedIds(new Set())
      props.onAdded()
      const { created, failed, skipped } = result
      if (created > 0) {
        toast.success(
          t('Created {{created}}, failed {{failed}}, skipped {{skipped}}', {
            created,
            failed,
            skipped,
          })
        )
      } else if (failed > 0 || skipped > 0) {
        toast.error(
          t(
            'Failed to create any node ({{failed}} failed, {{skipped}} skipped)',
            {
              failed,
              skipped,
            }
          )
        )
      }
      if (result.errors.length > 0) {
        toast.error(result.errors.join('\n'))
      }
    },
    onError: (error: Error) =>
      toast.error(error.message || t('Batch import failed')),
  })

  const lines = useMemo(() => {
    const seen = new Set<string>()
    return batch.proxy_text
      .split(/\r?\n/)
      .map((line) => line.trim())
      .filter((line) => {
        if (!line || line.startsWith('#') || seen.has(line)) return false
        seen.add(line)
        return true
      })
  }, [batch.proxy_text])
  const canSubmit =
    !mutation.isPending && lines.length > 0 && lines.length <= 500

  return (
    <Card>
      <CardHeader className='flex flex-row items-center justify-between gap-4'>
        <CardTitle className='text-base font-semibold'>{t('Batch Add')}</CardTitle>
        <div className='flex items-center gap-2'>
          <Button
            type='button'
            variant={batch.enabled ? 'default' : 'outline'}
            size='sm'
            onClick={() => setBatch({ ...batch, enabled: !batch.enabled })}
          >
            {batch.enabled ? t('Enabled') : t('Disabled')}
          </Button>
          <Button
            onClick={() => {
              // Convert selectedIds to custom scope JSON (always custom)
              const models: string[] = []
              const channels: number[] = []
              for (const id of selectedIds) {
                if (id.startsWith('m:')) models.push(id.slice(2))
                else if (id.startsWith('c:')) channels.push(Number(id.slice(2)))
              }

              mutation.mutate({
                ...batch,
                name_prefix: batch.name_prefix.trim(),
                proxy_text: batch.proxy_text.trim(),
                scope_type: 'custom',
                scope_value: JSON.stringify({ models, channels }),
              })
            }}

            disabled={!canSubmit}
            size='sm'
          >
            {mutation.isPending ? (
              <Loader2 className='size-4 animate-spin' />
            ) : (
              <Plus className='size-4' />
            )}
            {t('Add')}
          </Button>
        </div>
      </CardHeader>
      <CardContent className='space-y-4'>
        <div className='space-y-3'>
          <Input
            id='batch-add-name-prefix'
            value={batch.name_prefix}
            placeholder={t('Default: Proxy Node')}
            onChange={(event) =>
              setBatch({ ...batch, name_prefix: event.target.value })
            }
          />
          <Textarea
            id='batch-add-proxy-text'
            value={batch.proxy_text}
            placeholder={'socks5://…\nvmess://…'}
            rows={5}
            onChange={(event) =>
              setBatch({ ...batch, proxy_text: event.target.value })
            }
          />
          {lines.length > 500 && (
            <p className='text-destructive text-sm'>
              {t('Maximum 500 entries')}
            </p>
          )}
        </div>
        <div>
          <RouteModelPicker
            selectedIds={selectedIds}
            onToggle={handleToggle}
            pageSize={36}
          />
        </div>
      </CardContent>
    </Card>
  )
}

import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronLeft, ChevronRight } from 'lucide-react'

import { api } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

// LogTypeSensitive：与后端 model/log.go 的常量保持一致
const SENSITIVE_LOG_TYPE = 8
const PAGE_SIZE = 10

interface SensitiveAuditEntry {
  id: number
  user_id: number
  username: string
  created_at: number
  content: string
  ip: string
  other: string
}

interface SensitiveAuditRow {
  id: number
  time: string
  username: string
  modelName: string
  direction: string
  layer: string
  matched: string
  snippet: string
  ip: string
}

function parseEntry(entry: SensitiveAuditEntry): SensitiveAuditRow {
  let op: Record<string, unknown> = {}
  try {
    const other = JSON.parse(entry.other ?? '{}')
    op = (other.op as Record<string, unknown>) ?? {}
  } catch {
    // other 字段解析失败时仅展示基础列
  }
  const params = (op.params as Record<string, unknown>) ?? {}
  return {
    id: entry.id,
    time: entry.created_at
      ? new Date(entry.created_at * 1000).toLocaleString()
      : '-',
    username: entry.username || `#${entry.user_id}`,
    modelName: String(params.model_name ?? '-') || '-',
    direction: String(params.direction ?? '-'),
    layer: String(params.layer ?? '-'),
    matched: String(params.matched ?? ''),
    snippet: String(params.snippet ?? ''),
    ip: entry.ip || '-',
  }
}

export function SensitiveAuditTable() {
  const { t } = useTranslation()
  const [rows, setRows] = useState<SensitiveAuditRow[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)

  const fetchPage = useCallback(async (p: number) => {
    setLoading(true)
    try {
      const res = await api.get(
        `/api/log/?type=${SENSITIVE_LOG_TYPE}&p=${p}&page_size=${PAGE_SIZE}`
      )
      const data = res.data?.data
      setRows((data?.items as SensitiveAuditEntry[] ?? []).map(parseEntry))
      setTotal(data?.total ?? 0)
    } catch {
      setRows([])
      setTotal(0)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void fetchPage(page)
  }, [fetchPage, page])

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  return (
    <div className='mt-6'>
      <div className='mb-2 flex items-center justify-between'>
        <h4 className='text-sm font-medium'>{t('Recent Blocks')}</h4>
        <span className='text-muted-foreground text-xs'>
          {t('{{total}} records', { total })}
        </span>
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Time')}</TableHead>
            <TableHead>{t('User')}</TableHead>
            <TableHead>{t('Model')}</TableHead>
            <TableHead>{t('Direction')}</TableHead>
            <TableHead>{t('Layer')}</TableHead>
            <TableHead>{t('Matched')}</TableHead>
            <TableHead>{t('Snippet')}</TableHead>
            <TableHead>{t('IP')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading ? (
            <TableRow>
              <TableCell colSpan={8} className='text-muted-foreground py-4 text-center'>
                {t('Loading...')}
              </TableCell>
            </TableRow>
          ) : rows.length === 0 ? (
            <TableRow>
              <TableCell colSpan={8} className='text-muted-foreground py-4 text-center'>
                {t('No blocked requests yet')}
              </TableCell>
            </TableRow>
          ) : (
            rows.map((row) => (
              <TableRow key={row.id}>
                <TableCell className='whitespace-nowrap'>{row.time}</TableCell>
                <TableCell>{row.username}</TableCell>
                <TableCell>{row.modelName}</TableCell>
                <TableCell>
                  <Badge variant={row.direction === 'input' ? 'secondary' : 'outline'}>
                    {row.direction}
                  </Badge>
                </TableCell>
                <TableCell>
                  <Badge variant='outline'>{row.layer}</Badge>
                </TableCell>
                <TableCell className='max-w-[160px] truncate' title={row.matched}>
                  {row.matched}
                </TableCell>
                <TableCell className='max-w-[220px] truncate' title={row.snippet}>
                  {row.snippet || '-'}
                </TableCell>
                <TableCell>{row.ip}</TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
      {totalPages > 1 && (
        <div className='mt-2 flex items-center justify-end gap-2'>
          <Button
            variant='outline'
            size='sm'
            disabled={page <= 1 || loading}
            onClick={() => setPage((v) => Math.max(1, v - 1))}
          >
            <ChevronLeft className='size-4' />
          </Button>
          <span className='text-muted-foreground text-xs'>
            {page} / {totalPages}
          </span>
          <Button
            variant='outline'
            size='sm'
            disabled={page >= totalPages || loading}
            onClick={() => setPage((v) => v + 1)}
          >
            <ChevronRight className='size-4' />
          </Button>
        </div>
      )}
    </div>
  )
}

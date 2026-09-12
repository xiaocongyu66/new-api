/*
Copyright (C) 2023-2026 QuantumNous
*/

import { useState, useCallback } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Copy, Check, Unlink } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { IconBadge } from '@/components/ui/icon-badge'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { copyToClipboard } from '@/lib/copy-to-clipboard'

import { generateQQBindCode, getQQBindStatus, unbindQQ } from '../api'

interface QQBindCodeCardProps {
  show: boolean
  className?: string
}

export function QQBindCodeCard({ show, className }: QQBindCodeCardProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [copied, setCopied] = useState(false)
  const [confirmUnbind, setConfirmUnbind] = useState(false)
  const [unbinding, setUnbinding] = useState(false)

  const { data: status, isLoading } = useQuery({
    queryKey: ['qq-bind-status'],
    queryFn: async () => {
      const res = await getQQBindStatus()
      if (res.success && res.data) return res.data
      return null
    },
    enabled: show,
  })

  const { data: codeData, isFetching: isGenerating } = useQuery({
    queryKey: ['qq-bind-code'],
    queryFn: async () => {
      const res = await generateQQBindCode()
      if (res.success && res.data) return res.data
      return null
    },
    enabled: false,
  })

  const generate = useCallback(async () => {
    const res = await generateQQBindCode()
    if (res.success && res.data) {
      queryClient.setQueryData(['qq-bind-code'], res.data)
      setCopied(false)
    } else {
      toast.error(res.message || t('Failed to generate binding code'))
    }
  }, [queryClient, t])

  const copy = useCallback(async () => {
    if (!codeData?.code) return
    const ok = await copyToClipboard(codeData.code)
    if (ok) {
      setCopied(true)
      toast.success(t('Copied to clipboard'))
      setTimeout(() => setCopied(false), 2000)
    }
  }, [codeData, t])

  const unbind = useCallback(async () => {
    setUnbinding(true)
    try {
      const res = await unbindQQ()
      if (res.success) {
        toast.success(t('QQ account unbound'))
        // 绑定时用掉的那个验证码还留在缓存里，不清掉解绑后会显示一个已失效的码
        queryClient.setQueryData(['qq-bind-code'], null)
        await queryClient.invalidateQueries({ queryKey: ['qq-bind-status'] })
      } else {
        toast.error(res.message || t('Failed to unbind QQ account'))
      }
    } catch {
      toast.error(t('Failed to unbind QQ account'))
    } finally {
      setUnbinding(false)
      setConfirmUnbind(false)
    }
  }, [queryClient, t])

  if (!show) return null
  // QQ 签到关掉后仍要给已绑定的用户留出解绑入口，否则绑定只能找管理员清
  if (status && !status.qq_checkin_enabled && !status.bound) return null

  let generateLabel = t('Generate code')
  if (isGenerating) generateLabel = t('Generating...')
  else if (codeData) generateLabel = t('Regenerate')

  let body: React.ReactNode = (
    <>
      {codeData && (
        <TooltipProvider delay={100}>
          <div className='flex items-center gap-2'>
            <code className='bg-muted/50 flex-1 rounded-md border px-3 py-2 font-mono text-lg tracking-[0.3em]'>
              {codeData.code}
            </code>
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant='outline'
                    size='icon'
                    onClick={copy}
                    aria-label={t('Copy')}
                  >
                    {copied ? (
                      <Check className='h-4 w-4' />
                    ) : (
                      <Copy className='h-4 w-4' />
                    )}
                  </Button>
                }
              />
              <TooltipContent>{t('Copy')}</TooltipContent>
            </Tooltip>
          </div>
          <p className='text-muted-foreground text-xs'>
            {t('Valid for {{seconds}} seconds', {
              seconds: codeData.expires_in,
            })}
          </p>
        </TooltipProvider>
      )}
      <Button onClick={generate} disabled={isGenerating} className='w-full'>
        {generateLabel}
      </Button>
    </>
  )
  if (isLoading) {
    body = <Skeleton className='h-10 w-full' />
  } else if (status?.bound) {
    body = (
      <div className='rounded-lg border border-emerald-500/30 bg-emerald-500/5 p-3 text-sm'>
        <div className='flex items-center gap-2'>
          <div className='flex min-w-0 flex-1 items-center gap-2 text-emerald-700 dark:text-emerald-400'>
            <Check className='h-4 w-4 shrink-0' />
            <span className='font-medium'>{t('Bound QQ account')}</span>
          </div>
          <Button
            variant='ghost'
            size='sm'
            className='text-destructive hover:text-destructive h-7 shrink-0 px-2.5 text-xs'
            onClick={() => setConfirmUnbind(true)}
          >
            <Unlink className='mr-1 h-3 w-3' />
            {t('Unbind')}
          </Button>
        </div>
        {status.qq_username && (
          <div className='text-muted-foreground mt-1 text-xs'>
            {status.qq_username}
          </div>
        )}
      </div>
    )
  }

  return (
    <Card
      data-card-hover='false'
      className={`gap-0 overflow-hidden py-0 ${className ?? ''}`}
    >
      <div className='space-y-4 p-4 sm:p-6'>
        <div className='flex items-center gap-3'>
          <IconBadge tone='warning' size='md'>
            <KeyRound className='h-4 w-4' strokeWidth={2} />
          </IconBadge>
          <div className='flex-1'>
            <h3 className='text-sm font-semibold sm:text-base'>
              {t('QQ bind code')}
            </h3>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Generate a code and send it to the bot in the QQ group to finish binding'
              )}
            </p>
          </div>
        </div>

        {body}
      </div>

      <ConfirmDialog
        open={confirmUnbind}
        onOpenChange={setConfirmUnbind}
        title={t('Confirm Unbind')}
        desc={t(
          'Are you sure you want to unbind your QQ account? QQ check-in will stop working until you bind again.'
        )}
        confirmText={t('Confirm Unbind')}
        destructive
        handleConfirm={unbind}
        isLoading={unbinding}
      />
    </Card>
  )
}

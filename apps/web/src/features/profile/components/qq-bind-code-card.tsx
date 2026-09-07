/*
Copyright (C) 2023-2026 QuantumNous
*/

import { useState, useCallback } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Copy, Check } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { IconBadge } from '@/components/ui/icon-badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { copyToClipboard } from '@/lib/copy-to-clipboard'

import { generateQQBindCode, getQQBindStatus } from '../api'

interface QQBindCodeCardProps {
  show: boolean
  className?: string
}

export function QQBindCodeCard({ show, className }: QQBindCodeCardProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [copied, setCopied] = useState(false)

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
      toast.error(res.message || t('生成验证码失败'))
    }
  }, [queryClient])

  const copy = useCallback(async () => {
    if (!codeData?.code) return
    const ok = await copyToClipboard(codeData.code)
    if (ok) {
      setCopied(true)
      toast.success(t('已复制到剪贴板'))
      setTimeout(() => setCopied(false), 2000)
    }
  }, [codeData])

  if (!show) return null

  return (
    <Card data-card-hover='false' className={`gap-0 overflow-hidden py-0 ${className ?? ''}`}>
      <div className='space-y-4 p-4 sm:p-6'>
        <div className='flex items-center gap-3'>
          <IconBadge tone='warning' size='md'>
            <KeyRound className='h-4 w-4' strokeWidth={2} />
          </IconBadge>
          <div className='flex-1'>
            <h3 className='text-sm font-semibold sm:text-base'>
              {t('QQ 绑定验证码')}
            </h3>
            <p className='text-muted-foreground text-xs'>
              {t('生成验证码后在 QQ 群内发送给机器人（#开头）即可完成绑定')}
            </p>
          </div>
        </div>

        {isLoading ? (
          <Skeleton className='h-10 w-full' />
        ) : status?.bound ? (
          <div className='rounded-lg border border-emerald-500/30 bg-emerald-500/5 p-3 text-sm'>
            <div className='flex items-center gap-2 text-emerald-700 dark:text-emerald-400'>
              <Check className='h-4 w-4' />
              <span className='font-medium'>{t('已绑定')}</span>
            </div>
            {status.qq_username && (
              <div className='text-muted-foreground mt-1 text-xs'>
                {status.qq_username}
              </div>
            )}
          </div>
        ) : (
          <>
            {codeData && (
              <TooltipProvider delay={100}>
                <div className='flex items-center gap-2'>
                  <code className='flex-1 rounded-md border bg-muted/50 px-3 py-2 font-mono text-lg tracking-[0.3em]'>
                    {codeData.code}
                  </code>
                  <Tooltip>
                    <TooltipTrigger
                      render={
                        <Button
                          variant='outline'
                          size='icon'
                          onClick={copy}
                          aria-label={t('复制')}
                        >
                          {copied ? (
                            <Check className='h-4 w-4' />
                          ) : (
                            <Copy className='h-4 w-4' />
                          )}
                        </Button>
                      }
                    />
                    <TooltipContent>{t('复制')}</TooltipContent>
                  </Tooltip>
                </div>
                <p className='text-muted-foreground text-xs'>
                  {t('有效期 ' + codeData.expires_in + ' 秒')}
                </p>
              </TooltipProvider>
            )}
            <Button
              onClick={generate}
              disabled={isGenerating}
              className='w-full'
            >
              {isGenerating
                ? t('生成中…')
                : codeData
                  ? t('重新生成')
                  : t('生成验证码')}
            </Button>
          </>
        )}
      </div>
    </Card>
  )
}

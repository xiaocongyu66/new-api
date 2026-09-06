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
import { CheckCircle2 } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import { generateQQBindCode, getQQBindStatus } from '../../api'

// 绑定成功后的轮询间隔
const POLL_INTERVAL_MS = 3000

interface QQBindDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: () => void
}

export function QQBindDialog({
  open,
  onOpenChange,
  onSuccess,
}: QQBindDialogProps) {
  const { t } = useTranslation()
  const [initializing, setInitializing] = useState(false)
  const [code, setCode] = useState('')
  const [expiresIn, setExpiresIn] = useState(0)
  const [bound, setBound] = useState(false)

  const handleGenerate = useCallback(async () => {
    setInitializing(true)
    setCode('')
    setExpiresIn(0)
    try {
      const res = await generateQQBindCode()
      if (res.success && res.data?.code) {
        setCode(res.data.code)
        setExpiresIn(res.data.expires_in ?? 300)
      } else {
        toast.error(res.message || t('Failed to generate binding code'))
      }
    } catch (err) {
      toast.error(t('Network error'))
    } finally {
      setInitializing(false)
    }
  }, [onOpenChange, t])

  // 打开时自动申请验证码
  useEffect(() => {
    if (open && !code && !bound && !initializing) {
      handleGenerate()
    }
  }, [open, code, bound, initializing, handleGenerate])

  // 验证码倒计时
  useEffect(() => {
    if (!open || expiresIn <= 0) return

    const timer = setInterval(() => {
      setExpiresIn((prev) => {
        if (prev <= 1) {
          clearInterval(timer)
          return 0
        }
        return prev - 1
      })
    }, 1000)

    return () => clearInterval(timer)
  }, [open, expiresIn])

  // 轮询绑定状态，群内发送验证码后页面自动确认成功
  useEffect(() => {
    if (!open || !code || bound) return

    const poll = setInterval(async () => {
      try {
        const res = await getQQBindStatus()
        if (res.success && res.data?.bound) {
          setBound(true)
          clearInterval(poll)
          onSuccess()
          toast.success(t('QQ binding successful'))
        }
      } catch (err) {
        // 忽略错误，继续轮询
      }
    }, POLL_INTERVAL_MS)

    return () => clearInterval(poll)
  }, [open, bound, code, onSuccess, t])

  const handleOpenChange = (next: boolean) => {
    if (!next) {
      // 关闭时清理状态
      setCode('')
      setExpiresIn(0)
      setBound(false)
    }
    onOpenChange(next)
  }

  const expired = !!code && expiresIn <= 0 && !bound

  return (
    <Dialog
      open={open}
      onOpenChange={handleOpenChange}
      title={t('Bind QQ Account')}
      description={t('Please bind your QQ account to enable QQ checkin features.')}
    >
      <div className='space-y-4 py-4'>
        {!bound ? (
          <div className='space-y-3'>
            <div className='text-sm text-muted-foreground'>
              {t('Please enter the verification code sent in your QQ group chat.')}
            </div>

            <div className='flex items-center space-x-2'>
              <Input
                readOnly
                value={code}
                placeholder='000000'
                className='font-mono text-center text-lg'
                aria-label={t('Verification code')}
              />
              <CopyButton
                value={code}
                tooltip={t('Copy verification code')}
                aria-label={t('Copy verification code')}
              />
            </div>

            {expiresIn > 0 && (
              <div className='text-sm text-muted-foreground'>
                {t('Expires in')}: {Math.floor(expiresIn / 60)}:
                {String(expiresIn % 60).padStart(2, '0')}
              </div>
            )}

            {expired && (
              <div className='text-sm text-destructive'>
                {t('Code has expired, please generate again.')}
              </div>
            )}

            {/* No local "confirm" button: binding is established by the QQ bot
             * when the code is sent in the group, and the poll above flips the
             * dialog once the backend reports it. A client-side confirm would
             * claim success for a binding that never happened. */}
            <Button
              variant='outline'
              onClick={handleGenerate}
              disabled={initializing || expiresIn > 0}
              className='w-full'
            >
              {initializing
                ? t('Generating...')
                : expiresIn > 0
                  ? t('Regenerate')
                  : t('Generate')}
            </Button>
          </div>
        ) : (
          <div className='flex flex-col items-center space-y-3 py-6'>
            <CheckCircle2 className='h-12 w-12 text-green-500' />
            <div className='text-center'>
              <div className='font-medium'>{t('QQ binding successful')}</div>
              <div className='text-sm text-muted-foreground'>{t('You can now use QQ checkin features.')}</div>
            </div>
          </div>
        )}
      </div>
    </Dialog>
  )
}
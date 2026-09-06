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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { getQQBindStatus } from '../api'
import { QQBindDialog } from './dialogs/qq-bind-dialog'

/**
 * QQ 签到按钮
 *
 * 只渲染按钮本身（不含任何容器 / 分隔线），由调用方放进签到卡片头部的
 * 按钮行内，与「签到」按钮并排；网页签到关闭时它会自然顶替原按钮位置。
 * 已绑定后按钮置灰显示「QQ签到 · 已绑定」，不提供解绑入口。
 */
export function QQCheckinButton() {
  const { t } = useTranslation()
  const [dialogOpen, setDialogOpen] = useState(false)

  const { data, refetch } = useQuery({
    queryKey: ['qq-bind-status'],
    queryFn: async () => {
      const res = await getQQBindStatus()
      if (res.success && res.data) {
        return res.data
      }
      throw new Error(res.message || t('Failed to fetch QQ binding status'))
    },
    staleTime: 30000,
  })

  // QQ 签到未开启时不展示该按钮
  if (!data?.qq_checkin_enabled) {
    return null
  }

  const bound = data.bound === true

  return (
    <>
      <QQBindDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onSuccess={() => refetch()}
      />

      <Button
        variant='outline'
        size='sm'
        disabled={bound}
        onClick={() => setDialogOpen(true)}
        className='w-full shrink-0 sm:w-auto'
      >
        {bound ? `${t('QQ Check-in')} · ${t('Bound')}` : t('QQ Check-in')}
      </Button>
    </>
  )
}
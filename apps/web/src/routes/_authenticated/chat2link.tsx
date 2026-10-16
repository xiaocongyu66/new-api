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
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Main } from '@/components/layout'
import { Card, CardContent } from '@/components/ui/card'
import { useActiveChatKey } from '@/features/chat/hooks/use-active-chat-key'
import { useChatPresets } from '@/features/chat/hooks/use-chat-presets'
import { resolveChatUrl } from '@/features/chat/lib/chat-links'

export const Route = createFileRoute('/_authenticated/chat2link')({
  component: Chat2LinkPage,
})

function Chat2LinkPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { chatPresets, serverAddress } = useChatPresets()

  const firstWebPreset = useMemo(
    () => chatPresets.find((p) => p.type === 'web'),
    [chatPresets]
  )

  const { data: activeKey, error: keyError } = useActiveChatKey(
    Boolean(firstWebPreset)
  )

  useEffect(() => {
    if (!firstWebPreset) {
      if (chatPresets.length > 0) {
        toast.error(t('No available Web chat links'))
      }
      return
    }

    if (activeKey === undefined && !keyError) return

    if (keyError || !activeKey) {
      const message =
        keyError instanceof Error
          ? keyError.message
          : t('No enabled tokens available')
      toast.error(message)
      navigate({ to: '/keys' })
      return
    }

    const url = resolveChatUrl({
      template: firstWebPreset.url,
      apiKey: activeKey,
      serverAddress,
    })

    if (url) {
      window.location.href = url
    }
  }, [
    firstWebPreset,
    activeKey,
    keyError,
    serverAddress,
    chatPresets.length,
    navigate,
    t,
  ])

  return (
    <Main className='min-w-0 justify-center overflow-y-auto p-3 max-md:pb-[calc(5.5rem+env(safe-area-inset-bottom,0px))] sm:p-6'>
      <Card className='mx-auto w-full max-w-xl gap-0 py-0'>
        <CardContent className='flex flex-col items-center gap-4 p-5 text-center sm:p-8' role='status'>
          <Loader2 className='text-primary size-8 motion-safe:animate-spin' aria-hidden='true' />
          <p className='text-muted-foreground text-sm'>
            {t('Redirecting to chat page...')}
          </p>
        </CardContent>
      </Card>
    </Main>
  )
}

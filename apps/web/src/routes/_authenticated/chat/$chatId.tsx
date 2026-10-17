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
import { Link, createFileRoute, redirect } from '@tanstack/react-router'
import { Loader2, MessageCircleWarning } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Main } from '@/components/layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { useActiveChatKey } from '@/features/chat/hooks/use-active-chat-key'
import { useChatPresets } from '@/features/chat/hooks/use-chat-presets'
import {
  chatLinkRequiresApiKey,
  resolveChatUrl,
} from '@/features/chat/lib/chat-links'

export const Route = createFileRoute('/_authenticated/chat/$chatId')({
  loader: async ({ params }) => {
    if (!Number.isInteger(Number(params.chatId))) {
      throw redirect({ to: '/dashboard' })
    }
  },
  component: ChatRouteComponent,
})

function ChatRouteComponent() {
  const { t } = useTranslation()
  const { chatId } = Route.useParams()
  const { chatPresets, serverAddress } = useChatPresets()
  const preset = useMemo(() => {
    const index = Number(chatId)
    if (!Number.isInteger(index)) return undefined
    return chatPresets[index]
  }, [chatId, chatPresets])

  const isWebLink = preset?.type === 'web'

  const requiresActiveKey = useMemo(() => {
    if (!preset || !isWebLink) return false
    return chatLinkRequiresApiKey(preset.url ?? '')
  }, [isWebLink, preset])

  const {
    data: activeKey,
    isPending,
    isError,
    error,
  } = useActiveChatKey(Boolean(preset && requiresActiveKey))

  const iframeSrc = useMemo(() => {
    if (!preset || !isWebLink) return ''
    if (requiresActiveKey && !activeKey) return ''
    return resolveChatUrl({
      template: preset.url,
      apiKey: requiresActiveKey ? activeKey : undefined,
      serverAddress,
    })
  }, [activeKey, isWebLink, preset, requiresActiveKey, serverAddress])

  if (!preset) {
    return (
      <Main className='min-w-0 justify-center overflow-y-auto p-3 md:p-6'>
        <Card className='mx-auto w-full max-w-xl gap-0 py-0'>
          <CardContent className='flex min-w-0 flex-col items-center gap-4 p-5 text-center sm:p-8'>
            <div className='bg-secondary text-secondary-foreground flex size-14 items-center justify-center rounded-full'>
              <MessageCircleWarning className='size-6' aria-hidden='true' />
            </div>
            <div className='min-w-0 space-y-1 break-words'>
              <h2 className='text-lg font-semibold'>
                {t('Chat preset not found')}
              </h2>
              <p className='text-muted-foreground'>
                {t('The requested chat preset does not exist or has been removed.')}
              </p>
            </div>
            <Button variant='outline' render={<Link to='/dashboard' />}>
              {t('Return to dashboard')}
            </Button>
          </CardContent>
        </Card>
      </Main>
    )
  }

  if (!isWebLink) {
    return (
      <Main className='min-w-0 justify-center overflow-y-auto p-3 md:p-6'>
        <Card className='mx-auto w-full max-w-xl gap-0 py-0'>
          <CardContent className='flex min-w-0 flex-col items-center gap-4 p-5 text-center sm:p-8'>
            <div className='bg-secondary text-secondary-foreground flex size-14 items-center justify-center rounded-full'>
              <MessageCircleWarning className='size-6' aria-hidden='true' />
            </div>
            <div className='min-w-0 space-y-1 break-words'>
              <h2 className='text-lg font-semibold'>{t('Use sidebar shortcut')}</h2>
              <p className='text-muted-foreground'>
                {preset.name}{' '}
                {t(
                  'opens in an external client. Trigger it from the sidebar or API key actions to launch the configured application.'
                )}
              </p>
            </div>
            <Button variant='outline' render={<Link to='/dashboard' />}>
              {t('Return to dashboard')}
            </Button>
          </CardContent>
        </Card>
      </Main>
    )
  }

  if (requiresActiveKey && isPending) {
    return (
      <Main className='min-w-0 justify-center overflow-y-auto p-3 md:p-6'>
        <Card className='mx-auto w-full max-w-xl gap-0 py-0'>
          <CardContent className='flex flex-col items-center gap-4 p-5 text-center sm:p-8' role='status'>
            <Loader2 className='text-primary size-8 motion-safe:animate-spin' aria-hidden='true' />
            <p className='text-muted-foreground text-sm'>
              {t('Preparing your chat link…')}
            </p>
          </CardContent>
        </Card>
      </Main>
    )
  }

  if (requiresActiveKey && (isError || !activeKey || !iframeSrc)) {
    const message =
      error instanceof Error
        ? error.message
        : 'Unable to generate chat link. Please check your API keys.'
    return (
      <Main className='min-w-0 justify-center overflow-y-auto p-3 md:p-6'>
        <Alert variant='destructive' className='mx-auto w-full max-w-xl break-words'>
          <AlertTitle>{t('Unable to open chat')}</AlertTitle>
          <AlertDescription>{message}</AlertDescription>
        </Alert>
      </Main>
    )
  }

  if (!requiresActiveKey && !iframeSrc) {
    return (
      <Main className='min-w-0 justify-center overflow-y-auto p-3 md:p-6'>
        <Alert variant='destructive' className='mx-auto w-full max-w-xl break-words'>
          <AlertTitle>{t('Unable to open chat')}</AlertTitle>
          <AlertDescription>
            {t(
              'Unable to generate chat link. Please contact your administrator.'
            )}
          </AlertDescription>
        </Alert>
      </Main>
    )
  }

  return (
    <Main className='min-w-0 p-2 md:p-4'>
      <Card className='min-h-0 min-w-0 flex-1 gap-0 py-0'>
        <iframe
          src={iframeSrc}
          key={iframeSrc}
          className='h-full min-h-0 w-full flex-1 border-0'
          allow='camera; microphone'
          title={`Chat preset: ${preset.name}`}
        />
      </Card>
    </Main>
  )
}

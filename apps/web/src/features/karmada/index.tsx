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
import axios from 'axios'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { useTheme } from '@/context/theme-provider'
import { useThemeCustomization } from '@/context/theme-customization-provider'
import { useAuthStore } from '@/stores/auth-store'

import { KARMADA_DASHBOARD_URL } from './config'

const messageChannel = 'new-api:karmada-dashboard'
const messageVersion = 1

type DashboardReadyMessage = {
  channel: typeof messageChannel
  version: number
  type: 'ready'
  nonce: string
}

function isDashboardReadyMessage(value: unknown): value is DashboardReadyMessage {
  if (!value || typeof value !== 'object') return false
  const message = value as Record<string, unknown>
  return (
    message.channel === messageChannel &&
    message.version === messageVersion &&
    message.type === 'ready' &&
    typeof message.nonce === 'string' &&
    message.nonce.length >= 16
  )
}

export function KarmadaDashboard() {
  const { t, i18n } = useTranslation()
  const { theme, resolvedTheme } = useTheme()
  const { customization } = useThemeCustomization()
  const accessToken = useAuthStore((state) => state.auth.accessToken)
  const iframeRef = useRef<HTMLIFrameElement>(null)
  const [sessionReady, setSessionReady] = useState(false)
  const [sessionError, setSessionError] = useState(false)

  const dashboardUrl = new URL(KARMADA_DASHBOARD_URL, window.location.origin)
  const dashboardOrigin = dashboardUrl.origin

  const syncTheme = useCallback(
    (nonce: string) => {
      iframeRef.current?.contentWindow?.postMessage(
        {
          channel: messageChannel,
          version: messageVersion,
          type: 'theme',
          nonce,
          theme,
          resolvedTheme,
          customization,
          language: i18n.language,
        },
        dashboardOrigin
      )
    },
    [customization, dashboardOrigin, i18n.language, resolvedTheme, theme]
  )

  useEffect(() => {
    if (!accessToken) return

    let cancelled = false
    setSessionReady(false)
    setSessionError(false)

    void axios
      .post(
        '/api/karmada/session',
        undefined,
        {
          headers: { Authorization: `Bearer ${accessToken}` },
          withCredentials: true,
        }
      )
      .then(() => {
        if (!cancelled) setSessionReady(true)
      })
      .catch(() => {
        if (!cancelled) setSessionError(true)
      })

    return () => {
      cancelled = true
    }
  }, [accessToken])

  useEffect(() => {
    const onMessage = (event: MessageEvent<unknown>) => {
      if (event.origin !== dashboardOrigin) return
      if (event.source !== iframeRef.current?.contentWindow) return
      if (!isDashboardReadyMessage(event.data)) return
      syncTheme(event.data.nonce)
    }

    window.addEventListener('message', onMessage)
    return () => window.removeEventListener('message', onMessage)
  }, [dashboardOrigin, syncTheme])

  if (sessionError) {
    return (
      <div className='flex h-full items-center justify-center p-6'>
        <Alert variant='destructive' className='max-w-xl'>
          <AlertTitle>{t('Unable to open Karmada Dashboard')}</AlertTitle>
          <AlertDescription>
            {t('Your root session could not be verified for Karmada access.')}
          </AlertDescription>
        </Alert>
      </div>
    )
  }

  if (!sessionReady) {
    return <div className='flex h-full items-center justify-center'>{t('Loading...')}</div>
  }

  return (
    <iframe
      ref={iframeRef}
      src={dashboardUrl.toString()}
      className='h-full w-full border-0'
      title={t('Karmada Dashboard')}
    />
  )
}

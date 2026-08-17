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
const messageVersion = 2

const themeTokenNames = [
  'background',
  'foreground',
  'card',
  'card-foreground',
  'popover',
  'popover-foreground',
  'primary',
  'primary-foreground',
  'secondary',
  'secondary-foreground',
  'muted',
  'muted-foreground',
  'accent',
  'accent-foreground',
  'destructive',
  'destructive-foreground',
  'success',
  'success-foreground',
  'warning',
  'warning-foreground',
  'info',
  'info-foreground',
  'neutral',
  'neutral-foreground',
  'border',
  'input',
  'ring',
  'sidebar',
  'sidebar-foreground',
  'sidebar-primary',
  'sidebar-primary-foreground',
  'sidebar-accent',
  'sidebar-accent-foreground',
  'sidebar-border',
  'sidebar-ring',
  'chart-1',
  'chart-2',
  'chart-3',
  'chart-4',
  'chart-5',
  'radius',
  'font-body',
] as const

type ThemeTokens = Record<(typeof themeTokenNames)[number], string>

function readThemeTokens(): ThemeTokens {
  const styles = getComputedStyle(document.documentElement)
  return Object.fromEntries(
    themeTokenNames.map((name) => [name, styles.getPropertyValue(`--${name}`).trim()])
  ) as ThemeTokens
}

function createThemeMessage(nonce: string, revision: number, values: {
  theme: string
  resolvedTheme: string
  customization: Record<string, string>
  language: string
}) {
  return {
    channel: messageChannel,
    version: messageVersion,
    type: 'theme:update',
    nonce,
    revision,
    theme: values.theme,
    resolvedTheme: values.resolvedTheme,
    customization: values.customization,
    language: values.language,
    tokens: readThemeTokens(),
  }
}

export function KarmadaDashboard() {
  const { t, i18n } = useTranslation()
  const { theme, resolvedTheme } = useTheme()
  const { customization } = useThemeCustomization()
  const accessToken = useAuthStore((state) => state.auth.accessToken)
  const iframeRef = useRef<HTMLIFrameElement>(null)
  const [dashboardNonce, setDashboardNonce] = useState<string | null>(null)
  const [themeRevision, setThemeRevision] = useState(0)
  const [sessionReady, setSessionReady] = useState(false)
  const [sessionError, setSessionError] = useState(false)

  const dashboardUrl = new URL(KARMADA_DASHBOARD_URL, window.location.origin)
  const dashboardOrigin = dashboardUrl.origin

  const syncTheme = useCallback(
    (nonce: string, revision: number) => {
      iframeRef.current?.contentWindow?.postMessage(
        createThemeMessage(nonce, revision, {
          theme,
          resolvedTheme,
          customization,
          language: i18n.language,
        }),
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
    if (dashboardNonce) {
      setThemeRevision((revision) => revision + 1)
    }
  }, [customization, dashboardNonce, i18n.language, resolvedTheme, theme])

  useEffect(() => {
    if (dashboardNonce) syncTheme(dashboardNonce, themeRevision)
  }, [dashboardNonce, syncTheme, themeRevision])

  useEffect(() => {
    const onMessage = (event: MessageEvent<unknown>) => {
      if (event.origin !== dashboardOrigin) return
      if (event.source !== iframeRef.current?.contentWindow) return
      const data = event.data as Record<string, unknown> | null
      if (
        !data ||
        data.channel !== messageChannel ||
        data.version !== messageVersion ||
        data.type !== 'theme:ready' ||
        typeof data.nonce !== 'string' ||
        data.nonce.length < 16
      ) {
        return
      }
      setDashboardNonce(data.nonce)
      setThemeRevision(0)
    }

    window.addEventListener('message', onMessage)
    return () => window.removeEventListener('message', onMessage)
  }, [dashboardOrigin])

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
    <div className='flex min-h-0 flex-1 overflow-hidden'>
      <iframe
        ref={iframeRef}
        src={dashboardUrl.toString()}
        className='block h-full min-h-0 w-full border-0'
        title={t('Karmada Dashboard')}
        onLoad={() => setDashboardNonce(null)}
      />
    </div>
  )
}

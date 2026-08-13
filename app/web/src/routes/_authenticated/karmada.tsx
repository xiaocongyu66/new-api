import { useCallback, useEffect, useRef } from 'react'
import { createFileRoute, redirect } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { useTheme } from '@/context/theme-provider'
import { useThemeCustomization } from '@/context/theme-customization-provider'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

const THEME_TOKENS = [
  '--font-body',
  '--background',
  '--foreground',
  '--card',
  '--card-foreground',
  '--muted',
  '--muted-foreground',
  '--accent',
  '--accent-foreground',
  '--border',
  '--sidebar',
  '--sidebar-foreground',
  '--sidebar-accent',
  '--sidebar-accent-foreground',
  '--sidebar-border',
  '--sidebar-ring',
  '--radius',
] as const

export const Route = createFileRoute('/_authenticated/karmada')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()

    if (auth.user?.role !== ROLE.SUPER_ADMIN) {
      throw redirect({
        to: '/403',
      })
    }
  },
  component: KarmadaPage,
})

function KarmadaPage() {
  const { resolvedTheme } = useTheme()
  const { t } = useTranslation()
  const { customization } = useThemeCustomization()
  const iframeRef = useRef<HTMLIFrameElement>(null)

  const syncTheme = useCallback(() => {
    const frame = iframeRef.current
    if (!frame?.contentWindow) return

    const rootStyle = getComputedStyle(document.body)
    const tokens = Object.fromEntries(
      THEME_TOKENS.map((name) => [name, rootStyle.getPropertyValue(name).trim()])
    )

    frame.contentWindow.postMessage(
      { type: 'theme', theme: resolvedTheme, tokens },
      window.location.origin
    )
  }, [customization, resolvedTheme])

  useEffect(() => {
    const frame = requestAnimationFrame(syncTheme)
    return () => cancelAnimationFrame(frame)
  }, [syncTheme])

  return (
    <iframe
      ref={iframeRef}
      title={t('Karmada Panel')}
      src={`/dioxus/?theme=${resolvedTheme}`}
      onLoad={syncTheme}
      className='h-[calc(100svh-3rem)] w-full border-0'
    />
  )
}
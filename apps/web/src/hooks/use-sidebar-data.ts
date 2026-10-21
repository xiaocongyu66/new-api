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
import {
  Activity,
  FileText,
  FlaskConical,
  Key,
  LayoutDashboard,
  ListTodo,
  MessageSquare,
  Settings,
  Shield,
  Store,
  User,
  Wallet,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  ADMIN_NAVIGATION,
  resolveAdminNavigationUrl,
} from '@/components/layout/config/admin-navigation.config'
import { getSystemSettingsThemeNavItems } from '@/components/layout/config/system-settings.config'
import {
  type NavCollapsible,
  type SidebarData,
} from '@/components/layout/types'
import { useStatus } from '@/hooks/use-status'
import { useWalletBalance } from '@/hooks/use-wallet-balance'
import { hasPermission } from '@/lib/admin-permissions'
import { isPricingModuleEnabled } from '@/lib/nav-modules'
import { useAuthStore } from '@/stores/auth-store'

// Settings sections promoted to first-class icon-rail sections instead of
// living inside the System Settings panel.
const RAIL_SECTION_PREFIXES = [
  '/admin/system-settings/billing',
  '/admin/system-settings/models',
] as const

function isRailSection(section: NavCollapsible): boolean {
  const key = String(section.activeUrls?.[0] ?? '')
  return RAIL_SECTION_PREFIXES.some((prefix) => key.startsWith(prefix))
}

/**
 * Root navigation groups for the application sidebar.
 *
 * Each group is a section of the dual-sidebar icon rail (see
 * `icon-rail.tsx`); the `admin` group is only surfaced for admin roles
 * (visibility filtering happens in `useSidebarView`), and the system
 * settings sections require the system.settings capability.
 */
export function useSidebarData(): SidebarData {
  const { t } = useTranslation()
  const { status } = useStatus()
  const pricingEnabled = isPricingModuleEnabled(status)
  const walletBalance = useWalletBalance()
  const user = useAuthStore((s) => s.auth.user)

  // Admin workspace links; system settings lives in its own rail sections.
  const adminItems = ADMIN_NAVIGATION.filter(
    (item) => item.id !== 'system-settings'
  ).map((item) => ({
    title: t(item.labelKey),
    url: resolveAdminNavigationUrl(item),
    icon: item.icon,
    requiredRole: item.requiredRole,
  }))

  // System-settings sections with URLs rewritten under /admin. Billing and
  // models get dedicated rail icons; the remaining sections stay in the
  // System Settings panel.
  const settingsSections = hasPermission(user, 'system', 'settings')
    ? getSystemSettingsThemeNavItems(t).map((section) => ({
        ...section,
        activeUrls: section.activeUrls?.map((url) =>
          String(url).replace('/system-settings', '/admin/system-settings')
        ),
        items: section.items.map((sub) => ({
          ...sub,
          url: String(sub.url).replace(
            '/system-settings',
            '/admin/system-settings'
          ),
          activeUrls: sub.activeUrls?.map((url) =>
            String(url).replace('/system-settings', '/admin/system-settings')
          ),
        })),
      }))
    : []

  const railSettingGroups = settingsSections
    .filter(isRailSection)
    .map((section) => ({
      id: `settings-${String(section.activeUrls?.[0] ?? '')
        .split('/')
        .pop()}`,
      title: section.title,
      icon: section.icon,
      items: section.items,
    }))

  const systemSettingsGroup = settingsSections.length
    ? [
        {
          id: 'system-settings',
          title: t('System'),
          icon: Settings,
          items: settingsSections.filter((section) => !isRailSection(section)),
        },
      ]
    : []

  return {
    navGroups: [
      {
        id: 'general',
        title: t('General'),
        icon: LayoutDashboard,
        items: [
          {
            title: t('Chat'),
            items: [
              {
                title: t('Playground'),
                url: '/playground',
                icon: FlaskConical,
              },
            ],
          },
          {
            title: t('Chat'),
            icon: MessageSquare,
            type: 'chat-presets',
          },
          {
            title: t('General'),
            items: [
              {
                title: t('Overview'),
                url: '/dashboard/overview',
                icon: Activity,
              },
              {
                title: t('Dashboard'),
                url: '/dashboard/models',
                icon: LayoutDashboard,
              },
              { title: t('API Keys'), url: '/keys', icon: Key },
              {
                title: t('Usage Logs'),
                url: '/usage-logs/common',
                icon: FileText,
              },
              {
                title: t('Task Logs'),
                url: '/usage-logs/task',
                activeUrls: ['/usage-logs/drawing'],
                configUrls: ['/usage-logs/drawing', '/usage-logs/task'],
                icon: ListTodo,
              },
              ...(pricingEnabled
                ? [
                    {
                      title: t('Model Square'),
                      url: '/pricing',
                      icon: Store,
                    },
                  ]
                : []),
            ],
          },
          {
            title: t('Personal'),
            items: [
              {
                title: t('Wallet'),
                url: '/wallet',
                icon: Wallet,
                badge: walletBalance ?? undefined,
              },
              { title: t('Profile'), url: '/profile', icon: User },
            ],
          },
        ],
      },
      {
        id: 'admin',
        title: t('Manage'),
        icon: Shield,
        items: adminItems,
      },
      ...railSettingGroups,
      ...systemSettingsGroup,
    ],
  }
}

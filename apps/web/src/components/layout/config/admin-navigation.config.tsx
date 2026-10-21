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
  Box,
  CreditCard,
  LayoutDashboard,
  Network,
  Radio,
  ScanSearch,
  ServerCog,
  Settings,
  Ticket,
  Users,
} from 'lucide-react'

import type { AuthUser } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'

export type AdminNavigationItem = {
  id: string
  labelKey: `admin.${string}`
  to: string
  params?: Record<string, string>
  search?: Record<string, unknown>
  icon: React.ElementType
  requiredRole?: number
  requiredCapability?: {
    resource: string
    action: string
  }
}

export const ADMIN_NAVIGATION: readonly AdminNavigationItem[] = [
  { id: 'overview', labelKey: 'admin.overview', to: '/admin', icon: LayoutDashboard },
  { id: 'channels', labelKey: 'admin.channels', to: '/admin/channels', icon: Radio },
  { id: 'models', labelKey: 'admin.models', to: '/admin/models/$section', params: { section: 'metadata' }, icon: Box },
  { id: 'users', labelKey: 'admin.users', to: '/admin/users', icon: Users },
  { id: 'insights', labelKey: 'admin.insights', to: '/admin/user-insights', icon: ScanSearch },
  { id: 'redemptions', labelKey: 'admin.redemptions', to: '/admin/redemption-codes', icon: Ticket },
  { id: 'subscriptions', labelKey: 'admin.subscriptions', to: '/admin/subscriptions', icon: CreditCard },
  {
    id: 'proxy',
    labelKey: 'admin.proxy',
    to: '/admin/proxy',
    icon: Network,
    requiredRole: ROLE.SUPER_ADMIN,
  },
  {
    id: 'system-info',
    labelKey: 'admin.systemInfo',
    to: '/admin/system-info',
    icon: ServerCog,
    requiredRole: ROLE.SUPER_ADMIN,
  },
  {
    id: 'system-settings',
    labelKey: 'admin.systemSettings',
    to: '/admin/system-settings',
    icon: Settings,
    requiredCapability: { resource: 'system', action: 'settings' },
  },
] as const

/**
 * Resolve an admin nav entry to a concrete URL, substituting path params
 * (for example `/admin/models/$section` + `{section:'metadata'}`).
 */
export function resolveAdminNavigationUrl(item: AdminNavigationItem): string {
  if (!item.params) return item.to
  return Object.entries(item.params).reduce(
    (url, [key, value]) => url.replace(`$${key}`, value),
    item.to
  )
}

export function isAdminNavigationItemVisible(
  item: AdminNavigationItem,
  user: AuthUser | null | undefined,
  hasCapability: (user: AuthUser | null | undefined, resource: string, action: string) => boolean
): boolean {
  if (!user || (item.requiredRole !== undefined && user.role < item.requiredRole)) {
    return false
  }

  return item.requiredCapability === undefined || hasCapability(
    user,
    item.requiredCapability.resource,
    item.requiredCapability.action
  )
}

export function getVisibleAdminNavigation(
  user: AuthUser | null | undefined,
  hasCapability: (user: AuthUser | null | undefined, resource: string, action: string) => boolean
): AdminNavigationItem[] {
  return ADMIN_NAVIGATION.filter((item) => isAdminNavigationItemVisible(item, user, hasCapability))
}

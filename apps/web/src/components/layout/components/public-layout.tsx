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
import { SidebarProvider } from '@/components/ui/sidebar'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import type { TopNavLink } from '../types'
import { MobileBottomNav } from './mobile-bottom-nav'
import { PublicHeader, type PublicHeaderProps } from './public-header'

type PublicLayoutProps = {
  children: React.ReactNode
  showMainContainer?: boolean
  navContent?: React.ReactNode
  headerProps?: Omit<PublicHeaderProps, 'navContent'>
  navLinks?: TopNavLink[]
  showThemeSwitch?: boolean
  showAuthButtons?: boolean
  showConfigDrawer?: boolean
  showNotifications?: boolean
  logo?: React.ReactNode
  siteName?: string
}

export function PublicLayout(props: PublicLayoutProps) {
  const isAuthenticated = useAuthStore((s) => !!s.auth.user)
  return (
    <div
      data-slot='public-layout'
      className={cn(
        'bg-background text-foreground relative z-10 min-h-svh overflow-x-clip',
        // Clear the fixed mobile bottom bar (3.5rem) when it is mounted.
        isAuthenticated &&
          'pb-[calc(3.5rem+env(safe-area-inset-bottom,0px))] md:pb-0'
      )}
    >
      <PublicHeader
        navContent={props.navContent}
        navLinks={props.navLinks}
        showThemeSwitch={props.showThemeSwitch}
        showAuthButtons={props.showAuthButtons}
        showConfigDrawer={props.showConfigDrawer}
        showNotifications={props.showNotifications}
        logo={props.logo}
        siteName={props.siteName}
        {...props.headerProps}
      />

      {props.showMainContainer !== false ? (
        <main className='container px-4 py-6 pt-20 md:px-4'>
          {props.children}
        </main>
      ) : (
        props.children
      )}

      {/* Keep the app's bottom navigation reachable on public pages
          (model square, rankings, ...) for signed-in users. The bar and
          its drawer portal to the body, so only the sidebar context has
          to be provided here; there is no DOM footprint. */}
      {isAuthenticated ? (
        <SidebarProvider className='contents'>
          <MobileBottomNav />
        </SidebarProvider>
      ) : null}
    </div>
  )
}

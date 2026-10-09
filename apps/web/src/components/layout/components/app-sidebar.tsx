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
import { useLocation } from '@tanstack/react-router'
import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { useEffect, useMemo, useState } from 'react'

import {
  Sidebar,
  SidebarContent,
  SidebarRail,
  useSidebar,
} from '@/components/ui/sidebar'
import { useLayout } from '@/context/layout-provider'
import { useSidebarView } from '@/hooks/use-sidebar-view'
import { MOTION_TRANSITION, MOTION_VARIANTS } from '@/lib/motion'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { checkIsActive } from '../lib/url-utils'
import { IconRail } from './icon-rail'
import { NavGroup } from './nav-group'
import { SecondaryPanel } from './secondary-panel'

/**
 * Application sidebar shell, split by role:
 * - Admins (and above) get the dual-column "icon rail + secondary panel";
 * - regular users get the classic stacked sidebar listing all of their
 *   navigation groups in one full-width column.
 */
export function AppSidebar() {
  const userRole = useAuthStore((s) => s.auth.user?.role)
  const isAdmin = (userRole ?? ROLE.GUEST) >= ROLE.ADMIN

  return isAdmin ? <RailSidebar /> : <StackedSidebar />
}

/**
 * Dual-column sidebar for admins: the rail lists the navigation groups
 * (Chat, General, Personal, promoted settings sections and Admin) as icon
 * buttons; the secondary panel shows the active section's items. The active
 * section follows the current route and can be previewed by clicking a rail
 * icon. Collapsing the sidebar (Ctrl+B / header trigger) keeps the rail and
 * hides the panel.
 */
function RailSidebar() {
  const { collapsible, variant } = useLayout()
  const { state, isMobile } = useSidebar()
  const pathname = useLocation({ select: (location) => location.pathname })
  const { navGroups } = useSidebarView()

  // Manual section picks survive until the route lands in a section;
  // navigating (rail or panel link) hands control back to the route.
  const [sectionOverride, setSectionOverride] = useState<string | null>(null)

  const routeSectionId = useMemo(
    () =>
      navGroups.find((group) =>
        group.items.some((item) => checkIsActive(pathname, item))
      )?.id ?? null,
    [navGroups, pathname]
  )

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setSectionOverride(null)
  }, [routeSectionId, pathname])

  const visibleGroups = useMemo(() =>
    navGroups.filter((group) => group.items.length > 0)
  )

  const activeGroupId =
    sectionOverride ?? routeSectionId ?? visibleGroups[0]?.id
  const activeGroup =
    visibleGroups.find((group) => group.id === activeGroupId) ??
    visibleGroups[0] ??
    null

  return (
    <Sidebar collapsible={collapsible} variant={variant}>
      <div className='flex min-h-0 w-full flex-1'>
        <IconRail
          groups={visibleGroups}
          activeGroupId={activeGroup?.id ?? null}
          onSelect={setSectionOverride}
        />

        <SecondaryPanel
          group={activeGroup}
          hidden={state === 'collapsed' && !isMobile}
        />
      </div>

      <SidebarRail />
    </Sidebar>
  )
}

/**
 * Classic full-width stacked sidebar for regular users: every navigation
 * group renders as its own flat block list (shared NavGroup renderer), with
 * the usual slide animation on navigation-view changes.
 */
function StackedSidebar() {
  const { collapsible, variant } = useLayout()
  const { key, navGroups } = useSidebarView()
  const shouldReduce = useReducedMotion()

  const groups = useMemo(
    () => navGroups.filter((group) => group.items.length > 0),
    [navGroups]
  )

  return (
    <Sidebar collapsible={collapsible} variant={variant}>
      <SidebarContent className='py-2'>
        <AnimatePresence mode='wait' initial={false}>
          <motion.div
            key={key}
            initial={
              shouldReduce ? false : MOTION_VARIANTS.sidebarSlide.initial
            }
            animate={MOTION_VARIANTS.sidebarSlide.animate}
            exit={shouldReduce ? undefined : MOTION_VARIANTS.sidebarSlide.exit}
            transition={MOTION_TRANSITION.fast}
            className='flex flex-col'
          >
            {groups.map((props) => (
              <NavGroup key={props.id || props.title} {...props} />
            ))}
          </motion.div>
        </AnimatePresence>
      </SidebarContent>

      <SidebarRail />
    </Sidebar>
  )
}

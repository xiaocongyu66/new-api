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
import { Link, useLocation } from '@tanstack/react-router'
import type { TFunction } from 'i18next'
import { useMemo, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'

import { Drawer, DrawerContent, DrawerTitle } from '@/components/ui/drawer'
import { SidebarMenu, useSidebar } from '@/components/ui/sidebar'
import { useIsMobile } from '@/hooks/use-mobile'
import { useSidebarView } from '@/hooks/use-sidebar-view'
import { cn } from '@/lib/utils'

import { checkIsActive } from '../lib/url-utils'
import {
  type NavChatPresets,
  type NavLink,
  type NavGroup,
  type NavItem,
} from '../types'
import { ChatPresetsItem } from './chat-presets-item'

// Snap points for the section drawer: half open first, expand via the
// grab handle (or a drag) to nearly full height.
const SNAP_HALF = 0.55
const SNAP_FULL = 0.92

// Compact bottom-bar labels for the promoted settings rail sections;
// the drawer keeps the full section titles.
const SHORT_TAB_TITLES: Record<string, string> = {
  'settings-billing': 'Payments',
  'settings-models': 'Routing',
}

type BottomLinkTab = {
  kind: 'link'
  id: string
  title: string
  icon?: React.ElementType
  url: string
  isActive: boolean
}

type BottomGroupTab = {
  kind: 'group'
  id: string
  title: string
  icon?: React.ElementType
  group: NavGroup
  isActive: boolean
}

type BottomTab = BottomGroupTab | BottomLinkTab

/**
 * Mobile bottom navigation — the phone counterpart of the desktop
 * icon rail.
 *
 * Admins get one tab per sidebar section (tap raises a bottom drawer
 * listing that section's pages as a two-column grid). Regular users
 * get direct tabs for the pages they actually use (overview, keys,
 * model square) plus a chat drawer. The bar floats above the drawer
 * (higher z-index) and stays fully opaque so drawer content scrolling
 * out under it never bleeds through the labels.
 */
export function MobileBottomNav() {
  const { t } = useTranslation()
  const isMobile = useIsMobile()
  const { setOpenMobile } = useSidebar()
  const { navGroups } = useSidebarView()
  const pathname = useLocation({ select: (location) => location.pathname })
  const [openTabId, setOpenTabId] = useState<string | null>(null)

  const tabs = useMemo(
    () => buildBottomTabs(navGroups, pathname, t),
    [navGroups, pathname, t]
  )

  if (!isMobile || tabs.length === 0) return null

  const openTab = tabs.find(
    (tab): tab is BottomGroupTab => tab.kind === 'group' && tab.id === openTabId
  )

  return (
    <>
      {createPortal(
        <nav
          data-slot='mobile-bottom-nav'
          className={cn(
            'bg-background border-border/60 pointer-events-auto fixed inset-x-0 bottom-0 z-[60]',
            'flex items-stretch border-t md:hidden',
            'h-[calc(3.5rem+env(safe-area-inset-bottom,0px))]',
            'pb-[env(safe-area-inset-bottom,0px)]'
          )}
        >
          {tabs.map((tab) => {
            const Icon = tab.icon
            const inner = (
              <span
                className={cn(
                  'flex max-w-full min-w-0 flex-col items-center justify-center gap-1 px-1 py-1.5',
                  (openTabId !== null ? tab.id === openTabId : tab.isActive)
                    ? 'text-primary font-medium'
                    : 'text-muted-foreground'
                )}
              >
                {Icon ? <Icon className='size-5 shrink-0' /> : null}
                <span className='max-w-full truncate text-[10px] leading-none'>
                  {tab.title}
                </span>
              </span>
            )
            const slot = 'flex min-w-0 flex-1 items-center justify-center'
            return tab.kind === 'link' ? (
              <Link
                key={tab.id}
                to={tab.url}
                aria-current={tab.isActive ? 'page' : undefined}
                className={slot}
                onClick={() => setOpenMobile(false)}
              >
                {inner}
              </Link>
            ) : (
              <button
                key={tab.id}
                type='button'
                aria-current={tab.isActive ? 'page' : undefined}
                onClick={() => {
                  setOpenMobile(false)
                  setOpenTabId(tab.id)
                }}
                className={slot}
              >
                {inner}
              </button>
            )
          })}
        </nav>,
        document.body
      )}

      <SectionDrawer
        key={openTab?.id ?? 'closed'}
        group={openTab?.group ?? null}
        onClose={() => setOpenTabId(null)}
      />
    </>
  )
}

/**
 * Resolve bottom tabs from the resolved sidebar groups.
 *
 * Admin view: every visible rail section becomes a drawer tab.
 * Regular view: direct links to overview / keys / model square plus a
 * chat drawer holding the playground entry and chat presets.
 */
function buildBottomTabs(
  navGroups: NavGroup[],
  pathname: string,
  t: TFunction
): BottomTab[] {
  const visible = navGroups.filter((group) => group.items.length > 0)

  if (visible.some((group) => group.id === 'admin')) {
    return visible.map((group) => ({
      kind: 'group',
      id: group.id ?? group.title,
      title: group.id
        ? t(SHORT_TAB_TITLES[group.id] ?? group.title)
        : group.title,
      icon: group.icon,
      group,
      isActive: group.items.some((item) => checkIsActive(pathname, item)),
    }))
  }

  const general = visible.find((group) => group.id === 'general')
  if (!general) return []

  const tabs: BottomTab[] = []

  const presetItem = general.items.find(
    (item) => item.type === 'chat-presets'
  ) as NavChatPresets | undefined
  const playgroundBlock = general.items.find((item) =>
    item.items?.some((sub) => String(sub.url) === '/playground')
  )
  const chatItems = [playgroundBlock, presetItem].filter(Boolean) as NavItem[]
  if (chatItems.length > 0) {
    tabs.push({
      kind: 'group',
      id: 'chat',
      title: t('Chat'),
      icon: presetItem?.icon ?? playgroundBlock?.icon,
      group: { title: t('Chat'), items: chatItems },
      isActive:
        pathname.startsWith('/chat') ||
        chatItems.some((item) => checkIsActive(pathname, item)),
    })
  }

  const flatSubs = general.items.flatMap((item) =>
    'items' in item && item.items ? item.items : []
  )
  const findSub = (url: string) =>
    flatSubs.find((sub) => String(sub.url) === url)

  const overview = findSub('/dashboard/overview')
  if (overview) {
    tabs.push({
      kind: 'link',
      id: 'overview',
      title: t('Overview'),
      icon: overview.icon,
      url: String(overview.url),
      isActive: pathname === '/dashboard/overview',
    })
  }

  const keys = findSub('/keys')
  if (keys) {
    tabs.push({
      kind: 'link',
      id: 'keys',
      title: t('Keys'),
      icon: keys.icon,
      url: String(keys.url),
      isActive: pathname === '/keys',
    })
  }

  const square = findSub('/pricing')
  if (square) {
    tabs.push({
      kind: 'link',
      id: 'square',
      title: t('Square'),
      icon: square.icon,
      url: String(square.url),
      isActive: pathname === '/pricing',
    })
  }

  return tabs
}

/**
 * Bottom sheet listing one navigation section. Opens at half height;
 * tapping the grab handle expands it to near-full height. It slides
 * out behind the bottom bar, which stays tappable at all times.
 */
function SectionDrawer({
  group,
  onClose,
}: {
  group: NavGroup | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [snap, setSnap] = useState<number>(SNAP_HALF)
  const expanded = snap === SNAP_FULL

  return (
    <Drawer
      open={group !== null}
      onOpenChange={(open) => !open && onClose()}
      snapPoints={[SNAP_HALF, SNAP_FULL]}
      activeSnapPoint={snap}
      setActiveSnapPoint={(value) => {
        if (typeof value === 'number') setSnap(value)
      }}
      shouldScaleBackground={false}
    >
      <DrawerContent
        className='pb-[max(env(safe-area-inset-bottom,0px),0.5rem)]'
        style={{
          height: `${SNAP_FULL * 100}%`,
          maxHeight: `${SNAP_FULL * 100}%`,
        }}
      >
        <button
          type='button'
          aria-label={expanded ? t('Collapse') : t('Expand')}
          onClick={() => setSnap(expanded ? SNAP_HALF : SNAP_FULL)}
          className='absolute inset-x-0 top-0 z-10 h-9 w-full cursor-pointer'
        />
        {group ? (
          <>
            <DrawerTitle className='px-4 pt-2 pb-3 text-start text-base font-semibold'>
              {group.title}
            </DrawerTitle>
            {/* Clear the floating bottom bar (3.5rem) so the last rows
                of the grid are reachable. */}
            <div className='min-h-0 flex-1 overflow-y-auto px-4 pb-[calc(4.25rem+env(safe-area-inset-bottom,0px))]'>
              <SectionGrid group={group} onNavigate={onClose} />
            </div>
          </>
        ) : null}
      </DrawerContent>
    </Drawer>
  )
}

/**
 * Two-column card grid for a section, mirroring the flat block split of
 * the sidebar `NavGroup` renderer: plain links, collapsible subsections
 * (label + grid), and the dynamic chat presets block.
 */
function SectionGrid({
  group,
  onNavigate,
}: {
  group: NavGroup
  onNavigate: () => void
}) {
  const href = useLocation({ select: (location) => location.href })

  type Block =
    | { kind: 'grid'; key: string; label?: string; items: NavLink[] }
    | { kind: 'presets'; key: string; item: NavChatPresets }

  const blocks: Block[] = []
  for (const item of group.items) {
    const key = `${item.title}-${item.url || item.type}`
    if (item.type === 'chat-presets') {
      blocks.push({ kind: 'presets', key, item })
      continue
    }
    if (item.items) {
      blocks.push({
        kind: 'grid',
        key,
        label: item.title,
        items: item.items as NavLink[],
      })
      continue
    }
    const last = blocks[blocks.length - 1]
    if (last?.kind === 'grid' && !last.label) last.items.push(item)
    else blocks.push({ kind: 'grid', key, items: [item] })
  }

  return (
    <div className='flex flex-col gap-4'>
      {blocks.map((block) => {
        if (block.kind === 'presets') {
          return (
            <SidebarMenu key={block.key}>
              <ChatPresetsItem item={block.item} />
            </SidebarMenu>
          )
        }
        return (
          <div key={block.key} className='flex flex-col gap-2'>
            {block.label ? (
              <div className='text-muted-foreground/70 text-[11px] font-medium tracking-wider uppercase'>
                {block.label}
              </div>
            ) : null}
            <div className='grid grid-cols-2 gap-2'>
              {block.items.map((item) => (
                <GridCard
                  key={`${item.title}-${item.url}`}
                  item={item}
                  currentHref={href}
                  onNavigate={onNavigate}
                />
              ))}
            </div>
          </div>
        )
      })}
    </div>
  )
}

function GridCard({
  item,
  currentHref,
  onNavigate,
}: {
  item: NavLink
  currentHref: string
  onNavigate: () => void
}) {
  const active = checkIsActive(currentHref, item)
  const Icon = item.icon
  return (
    <Link
      to={item.url}
      onClick={onNavigate}
      className={cn(
        'border-border/60 bg-card flex items-center gap-2 rounded-xl border p-3 text-sm',
        active && 'text-primary font-medium'
      )}
    >
      {Icon ? <Icon className='size-4 shrink-0 opacity-70' /> : null}
      <span className='min-w-0 flex-1 truncate'>{item.title}</span>
      {item.badge ? (
        <span className='bg-primary text-primary-foreground shrink-0 rounded-full px-1.5 py-0.5 text-[10px] leading-none font-medium'>
          {item.badge}
        </span>
      ) : null}
    </Link>
  )
}

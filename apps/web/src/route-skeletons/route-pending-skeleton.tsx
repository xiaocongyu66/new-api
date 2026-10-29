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
import { useRouterState } from '@tanstack/react-router'
import type { ComponentType } from 'react'

import { RouteLoadingFallback } from '@/components/loading-state'

import {
  AboutPageSkeleton,
  AuthPageSkeleton,
  AuthedTablePageSkeleton,
  ChatRouteSkeleton,
  DashboardPageSkeleton,
  HomePageSkeleton,
  InsightsPageSkeleton,
  PricingModelPageSkeleton,
  PricingPageSkeleton,
  ProfilePageSkeleton,
  RankingsPageSkeleton,
  SettingsRouteSkeleton,
  SetupPageSkeleton,
  SystemInfoPageSkeleton,
  WalletPageSkeleton,
} from './page-skeletons'
import {
  resolveRouteSkeletonKey,
  type RouteSkeletonKey,
} from './route-skeleton-map'

const SKELETON_ROUTES: Record<RouteSkeletonKey, ComponentType> = {
  home: HomePageSkeleton,
  pricing: PricingPageSkeleton,
  'pricing-model': PricingModelPageSkeleton,
  rankings: RankingsPageSkeleton,
  about: AboutPageSkeleton,
  dashboard: DashboardPageSkeleton,
  table: AuthedTablePageSkeleton,
  insights: InsightsPageSkeleton,
  settings: SettingsRouteSkeleton,
  profile: ProfilePageSkeleton,
  wallet: WalletPageSkeleton,
  'system-info': SystemInfoPageSkeleton,
  chat: ChatRouteSkeleton,
  wizard: SetupPageSkeleton,
  'auth-card': AuthPageSkeleton,
}

/**
 * Location-aware default pending component.
 *
 * The root route's async `beforeLoad` (session bootstrap/refresh) re-runs
 * on every navigation, and several public routes (`/pricing`, `/rankings`,
 * `/setup`) run async module-access checks — both open the router's
 * pending window. During that window the router sets `state.location` to
 * the *target* location before running `beforeLoad` chains, so reading
 * `state.location.pathname` here resolves the skeleton of the page the
 * user is actually navigating to, not the page they are leaving.
 *
 * Mounted as `defaultPendingComponent` in `apps/web/src/main.tsx`, this
 * covers every pending source (root session refresh, leaf route guards,
 * initial load) with one registry — no per-route `pendingComponent`
 * edits needed, which keeps the diff away from files under concurrent
 * edit in other sessions.
 */
export function RoutePendingSkeleton() {
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const key = resolveRouteSkeletonKey(pathname)

  if (!key) {
    return <RouteLoadingFallback />
  }

  const Skeleton = SKELETON_ROUTES[key]
  return <Skeleton />
}

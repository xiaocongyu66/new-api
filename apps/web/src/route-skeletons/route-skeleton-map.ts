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

const ROUTE_SKELETON_KEYS = [
  'home',
  'pricing',
  'pricing-model',
  'rankings',
  'about',
  'dashboard',
  'table',
  'insights',
  'settings',
  'profile',
  'wallet',
  'system-info',
  'chat',
  'wizard',
  'auth-card',
] as const

export type RouteSkeletonKey = (typeof ROUTE_SKELETON_KEYS)[number]

interface RouteSkeletonRule {
  pattern: RegExp
  key: RouteSkeletonKey
}

/**
 * Ordered pathname → skeleton-key rules. Order matters: parameterized
 * detail routes must precede their collection prefixes, and every
 * `/admin/*` rule must precede the bare `/admin` landing rule.
 *
 * Kept as pure data (no React) so the matcher is unit-testable and so
 * `RoutePendingSkeleton` stays a thin resolver.
 */
const RULES: RouteSkeletonRule[] = [
  { pattern: /^\/pricing\/[^/]+/, key: 'pricing-model' },
  { pattern: /^\/pricing(\/|$)/, key: 'pricing' },
  { pattern: /^\/rankings(\/|$)/, key: 'rankings' },
  { pattern: /^\/about(\/|$)/, key: 'about' },
  {
    pattern: /^\/(sign-in|sign-up|register|forgot-password|otp|reset)/,
    key: 'auth-card',
  },
  { pattern: /^\/(user\/reset|oauth\/)/, key: 'auth-card' },
  { pattern: /^\/setup(\/|$)/, key: 'wizard' },
  { pattern: /^\/chat2link(\/|$)/, key: 'chat' },
  { pattern: /^\/chat\//, key: 'chat' },
  { pattern: /^\/playground(\/|$)/, key: 'chat' },
  { pattern: /^\/(admin\/)?user-insights(\/|$)/, key: 'insights' },
  { pattern: /^\/(admin\/)?users(\/|$)/, key: 'table' },
  { pattern: /^\/(admin\/)?channels(\/|$)/, key: 'table' },
  { pattern: /^\/(admin\/)?models(\/|$)/, key: 'table' },
  { pattern: /^\/(admin\/)?subscriptions(\/|$)/, key: 'table' },
  { pattern: /^\/(admin\/)?redemption-codes(\/|$)/, key: 'table' },
  { pattern: /^\/(admin\/)?proxy(\/|$)/, key: 'table' },
  { pattern: /^\/(admin\/)?system-info(\/|$)/, key: 'system-info' },
  { pattern: /^\/(admin\/)?system-settings(\/|$)/, key: 'settings' },
  { pattern: /^\/usage-logs(\/|$)/, key: 'table' },
  { pattern: /^\/keys(\/|$)/, key: 'table' },
  { pattern: /^\/wallet(\/|$)/, key: 'wallet' },
  { pattern: /^\/profile(\/|$)/, key: 'profile' },
  { pattern: /^\/dashboard(\/|$)/, key: 'dashboard' },
  { pattern: /^\/admin(\/|$)/, key: 'dashboard' },
  { pattern: /^\/$/, key: 'home' },
]

/**
 * Resolve the skeleton key for a pathname. Returns null when no rule
 * matches; the caller then falls back to the generic route fallback.
 */
export function resolveRouteSkeletonKey(
  pathname: string
): RouteSkeletonKey | null {
  for (const rule of RULES) {
    if (rule.pattern.test(pathname)) {
      return rule.key
    }
  }
  return null
}

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
import { describe, expect, it } from 'bun:test'

import { resolveRouteSkeletonKey } from './route-skeleton-map'

describe('route skeleton map', () => {
  it('maps public marketing pages to their own skeletons', () => {
    expect(resolveRouteSkeletonKey('/')).toBe('home')
    expect(resolveRouteSkeletonKey('/pricing')).toBe('pricing')
    expect(resolveRouteSkeletonKey('/pricing/')).toBe('pricing')
    expect(resolveRouteSkeletonKey('/pricing/gpt-4o')).toBe('pricing-model')
    expect(resolveRouteSkeletonKey('/rankings')).toBe('rankings')
    expect(resolveRouteSkeletonKey('/about')).toBe('about')
  })

  it('maps auth and setup flows to bare shells', () => {
    expect(resolveRouteSkeletonKey('/sign-in')).toBe('auth-card')
    expect(resolveRouteSkeletonKey('/sign-up')).toBe('auth-card')
    expect(resolveRouteSkeletonKey('/register')).toBe('auth-card')
    expect(resolveRouteSkeletonKey('/forgot-password')).toBe('auth-card')
    expect(resolveRouteSkeletonKey('/reset')).toBe('auth-card')
    expect(resolveRouteSkeletonKey('/user/reset')).toBe('auth-card')
    expect(resolveRouteSkeletonKey('/otp')).toBe('auth-card')
    expect(resolveRouteSkeletonKey('/oauth/github')).toBe('auth-card')
    expect(resolveRouteSkeletonKey('/setup')).toBe('wizard')
  })

  it('maps chat-style pages to the chat archetype', () => {
    expect(resolveRouteSkeletonKey('/chat/42')).toBe('chat')
    expect(resolveRouteSkeletonKey('/chat2link')).toBe('chat')
    expect(resolveRouteSkeletonKey('/playground')).toBe('chat')
  })

  it('maps console list pages to the table archetype', () => {
    expect(resolveRouteSkeletonKey('/keys')).toBe('table')
    expect(resolveRouteSkeletonKey('/usage-logs/common')).toBe('table')
    expect(resolveRouteSkeletonKey('/usage-logs/task')).toBe('table')
    expect(resolveRouteSkeletonKey('/users')).toBe('table')
    expect(resolveRouteSkeletonKey('/admin/users')).toBe('table')
    expect(resolveRouteSkeletonKey('/channels')).toBe('table')
    expect(resolveRouteSkeletonKey('/admin/channels')).toBe('table')
    expect(resolveRouteSkeletonKey('/models/metadata')).toBe('table')
    expect(resolveRouteSkeletonKey('/admin/models/pricing')).toBe('table')
    expect(resolveRouteSkeletonKey('/subscriptions')).toBe('table')
    expect(resolveRouteSkeletonKey('/admin/subscriptions')).toBe('table')
    expect(resolveRouteSkeletonKey('/redemption-codes')).toBe('table')
    expect(resolveRouteSkeletonKey('/admin/redemption-codes')).toBe('table')
    expect(resolveRouteSkeletonKey('/proxy')).toBe('table')
    expect(resolveRouteSkeletonKey('/admin/proxy')).toBe('table')
  })

  it('keeps insights, settings, and panel pages distinct from plain tables', () => {
    expect(resolveRouteSkeletonKey('/user-insights')).toBe('insights')
    expect(resolveRouteSkeletonKey('/admin/user-insights')).toBe('insights')
    expect(resolveRouteSkeletonKey('/system-settings/site/general')).toBe(
      'settings'
    )
    expect(
      resolveRouteSkeletonKey('/admin/system-settings/security/auth')
    ).toBe('settings')
    expect(resolveRouteSkeletonKey('/system-info')).toBe('system-info')
    expect(resolveRouteSkeletonKey('/admin/system-info')).toBe('system-info')
  })

  it('maps single-purpose pages to their archetypes', () => {
    expect(resolveRouteSkeletonKey('/dashboard/overview')).toBe('dashboard')
    expect(resolveRouteSkeletonKey('/dashboard/models')).toBe('dashboard')
    expect(resolveRouteSkeletonKey('/admin')).toBe('dashboard')
    expect(resolveRouteSkeletonKey('/wallet')).toBe('wallet')
    expect(resolveRouteSkeletonKey('/profile')).toBe('profile')
  })

  it('resolves user-insights before users and admin landing after admin sections', () => {
    // Prefix collision guards: /user-insights must not fall into the users rule.
    expect(resolveRouteSkeletonKey('/user-insights/profiles')).toBe('insights')
    expect(resolveRouteSkeletonKey('/admin/users')).toBe('table')
    expect(resolveRouteSkeletonKey('/admin/user-insights')).toBe('insights')
  })

  it('returns null for unmapped paths so the generic fallback renders', () => {
    expect(resolveRouteSkeletonKey('/404')).toBeNull()
    expect(resolveRouteSkeletonKey('/privacy-policy')).toBeNull()
    expect(resolveRouteSkeletonKey('/user-agreement')).toBeNull()
    expect(resolveRouteSkeletonKey('/unknown-route')).toBeNull()
    // Bare roots that only exist as redirect sources, not real routes
    expect(resolveRouteSkeletonKey('/user')).toBeNull()
    expect(resolveRouteSkeletonKey('/chat')).toBeNull()
    expect(resolveRouteSkeletonKey('')).toBeNull()
  })
})

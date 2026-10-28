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
import type { TFunction } from 'i18next'

/**
 * Navigation descriptors + route-guard data for the Auth domain. Everything in
 * this module is card-free: the sidebar/command-palette nav items, the section
 * id set, and the default section are all lightweight. The render registry
 * (section definitions + card components + getSectionContent/Meta) lives in
 * `./sections.tsx` and is only pulled in by the lazy settings route
 * components, keeping the settings form stack out of the initial chunk.
 */
const AUTH_SECTION_NAV = [
  { id: 'basic-auth', titleKey: 'Basic Authentication' },
  { id: 'oauth', titleKey: 'OAuth Integrations' },
  { id: 'passkey', titleKey: 'Passkey Authentication' },
  { id: 'bot-protection', titleKey: 'Bot Protection' },
  { id: 'custom-oauth', titleKey: 'Custom OAuth' },
] as const

export type AuthSectionId = (typeof AUTH_SECTION_NAV)[number]['id']
export const AUTH_DEFAULT_SECTION: AuthSectionId = 'basic-auth'
export const AUTH_SECTION_IDS = AUTH_SECTION_NAV.map((section) => section.id) as [
  AuthSectionId,
  ...AuthSectionId[],
]

export const getAuthSectionNavItems = (t: TFunction) =>
  AUTH_SECTION_NAV.map((section) => ({
    title: t(section.titleKey),
    url: `/system-settings/auth/${section.id}`,
  }))

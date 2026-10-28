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
 * Navigation descriptors + route-guard data for the Operations domain. Card-free:
 * nav items, the full section id set, and the default section are lightweight.
 * The render registry (cards + getSectionContent/Meta) lives in `./sections.tsx`.
 */
const OPERATIONS_SECTION_NAV = [
  { id: 'behavior', titleKey: 'System Behavior' },
  { id: 'alerts', titleKey: 'Monitoring & Alerts' },
  { id: 'email', titleKey: 'SMTP Email' },
  { id: 'worker', titleKey: 'Worker Proxy' },
  { id: 'logs', titleKey: 'Log Maintenance' },
  { id: 'performance', titleKey: 'Performance' },
  { id: 'update-checker', titleKey: 'System maintenance' },
] as const

export type OperationsSectionId = (typeof OPERATIONS_SECTION_NAV)[number]['id']
export const OPERATIONS_DEFAULT_SECTION: OperationsSectionId = 'behavior'
export const OPERATIONS_SECTION_IDS = OPERATIONS_SECTION_NAV.map(
  (section) => section.id
) as [OperationsSectionId, ...OperationsSectionId[]]

export const getOperationsSectionNavItems = (t: TFunction) =>
  OPERATIONS_SECTION_NAV.map((section) => ({
    title: t(section.titleKey),
    url: `/system-settings/operations/${section.id}`,
  }))

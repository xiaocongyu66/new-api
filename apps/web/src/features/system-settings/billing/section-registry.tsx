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
 * Navigation descriptors + route-guard data for the Billing domain. Card-free:
 * nav items, the full section id set, and the default section are lightweight.
 * The `qqbot` section is a valid URL (present in the id set) but is hidden from
 * the nav sidebar, matching the original filtered `getBillingSectionNavItems`.
 * The render registry (cards + getSectionContent/Meta) lives in `./sections.tsx`.
 */
const BILLING_SECTION_NAV = [
  { id: 'quota', titleKey: 'Quota Settings' },
  { id: 'currency', titleKey: 'Currency & Display' },
  { id: 'group-pricing', titleKey: 'Group Pricing' },
  { id: 'payment', titleKey: 'Payment Gateway' },
  { id: 'checkin', titleKey: 'Check-in Rewards' },
  { id: 'qqbot', titleKey: 'QQ Bot' },
] as const

export type BillingSectionId = (typeof BILLING_SECTION_NAV)[number]['id']
export const BILLING_DEFAULT_SECTION: BillingSectionId = 'quota'
export const BILLING_SECTION_IDS = BILLING_SECTION_NAV.map(
  (section) => section.id
) as [BillingSectionId, ...BillingSectionId[]]

export const getBillingSectionNavItems = (t: TFunction) =>
  BILLING_SECTION_NAV.map((section) => ({
    title: t(section.titleKey),
    url: `/system-settings/billing/${section.id}`,
  })).filter((item) => !item.url.endsWith('/qqbot'))

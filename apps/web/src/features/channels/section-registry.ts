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
/**
 * Channels page sections. The channel list stays the default; model pricing
 * lives here so it sits next to the channels that provide the models.
 */
export const CHANNELS_SECTIONS = [
  {
    id: 'channels',
    titleKey: 'Channels',
  },
  {
    id: 'model-pricing',
    titleKey: 'Model Pricing',
  },
] as const

export type ChannelsSectionId = (typeof CHANNELS_SECTIONS)[number]['id']

export const CHANNELS_SECTION_IDS = CHANNELS_SECTIONS.map(
  (section) => section.id
) as readonly ChannelsSectionId[]

export const CHANNELS_DEFAULT_SECTION: ChannelsSectionId = 'channels'

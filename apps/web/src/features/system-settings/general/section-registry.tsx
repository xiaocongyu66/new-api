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

For commercial licensing, please contact support@quantumnous.com.
*/
import type { BillingSettings } from '../types'
import { createSectionRegistry } from '../utils/section-registry'
import { QQBotSettingsSection } from './qqbot-settings-section'

const GENERAL_SECTIONS = [
  {
    id: 'qqbot' as const,
    titleKey: 'QQ Bot',
    build: (settings: BillingSettings) => (
      <QQBotSettingsSection
        defaultValues={{
          'qq_bot_setting.app_id': settings['qq_bot_setting.app_id'] ?? '',
          'qq_bot_setting.app_secret':
            settings['qq_bot_setting.app_secret'] ?? '',
          'qq_bot_setting.qq_checkin_enabled':
            settings['qq_bot_setting.qq_checkin_enabled'] ?? false,
          'qq_bot_setting.web_checkin_enabled':
            settings['qq_bot_setting.web_checkin_enabled'] ?? true,
          'qq_bot_setting.single_platform_only':
            settings['qq_bot_setting.single_platform_only'] ?? true,
          'qq_bot_setting.notify_template':
            settings['qq_bot_setting.notify_template'] ?? '',
          'qq_bot_setting.auto_approve_enabled':
            settings['qq_bot_setting.auto_approve_enabled'] ?? false,
          'qq_bot_setting.auto_approve_keyword':
            settings['qq_bot_setting.auto_approve_keyword'] ?? '',
          'qq_bot_setting.min_quota':
            settings['qq_bot_setting.min_quota'] ?? 0,
          'qq_bot_setting.max_quota':
            settings['qq_bot_setting.max_quota'] ?? 250000,
          'qq_bot_setting.checkin_disabled_groups':
            settings['qq_bot_setting.checkin_disabled_groups'] ?? '',
          'qq_bot_setting.drop_enabled':
            settings['qq_bot_setting.drop_enabled'] ?? false,
          'qq_bot_setting.drop_groups':
            settings['qq_bot_setting.drop_groups'] ?? '',
          'qq_bot_setting.drop_min_messages':
            settings['qq_bot_setting.drop_min_messages'] ?? 5,
          'qq_bot_setting.drop_max_messages':
            settings['qq_bot_setting.drop_max_messages'] ?? 30,
          'qq_bot_setting.drop_min_quota':
            settings['qq_bot_setting.drop_min_quota'] ?? 150000,
          'qq_bot_setting.drop_max_quota':
            settings['qq_bot_setting.drop_max_quota'] ?? 1500000,
          'qq_bot_setting.drop_daily_limit':
            settings['qq_bot_setting.drop_daily_limit'] ?? 3,
          'qq_bot_setting.drop_template':
            settings['qq_bot_setting.drop_template'] ?? '',
          'qq_bot_setting.transfer_enabled':
            settings['qq_bot_setting.transfer_enabled'] ?? false,
          'qq_bot_setting.transfer_disabled_groups':
            settings['qq_bot_setting.transfer_disabled_groups'] ?? '',
          'qq_bot_setting.transfer_daily_limit':
            settings['qq_bot_setting.transfer_daily_limit'] ?? 2,
          'qq_bot_setting.transfer_min_amount':
            settings['qq_bot_setting.transfer_min_amount'] ?? 50000,
          'qq_bot_setting.transfer_max_amount':
            settings['qq_bot_setting.transfer_max_amount'] ?? 50000000,
          'qq_bot_setting.transfer_fee_brackets':
            settings['qq_bot_setting.transfer_fee_brackets'] ?? '',
          'qq_bot_setting.red_packet_enabled':
            settings['qq_bot_setting.red_packet_enabled'] ?? false,
          'qq_bot_setting.red_packet_disabled_groups':
            settings['qq_bot_setting.red_packet_disabled_groups'] ?? '',
          'qq_bot_setting.red_packet_daily_limit':
            settings['qq_bot_setting.red_packet_daily_limit'] ?? 3,
          'qq_bot_setting.red_packet_min_amount':
            settings['qq_bot_setting.red_packet_min_amount'] ?? 500000,
          'qq_bot_setting.red_packet_max_amount':
            settings['qq_bot_setting.red_packet_max_amount'] ?? 100000000,
          'qq_bot_setting.red_packet_default_count':
            settings['qq_bot_setting.red_packet_default_count'] ?? 5,
          'qq_bot_setting.red_packet_max_count':
            settings['qq_bot_setting.red_packet_max_count'] ?? 50,
          'qq_bot_setting.red_packet_expire_seconds':
            settings['qq_bot_setting.red_packet_expire_seconds'] ?? 86400,
          'qq_bot_setting.red_packet_allow_own_grab':
            settings['qq_bot_setting.red_packet_allow_own_grab'] ?? false,
          'qq_bot_setting.command_cooldown_seconds':
            settings['qq_bot_setting.command_cooldown_seconds'] ?? 0,
          'qq_bot_setting.recall_failed_messages':
            settings['qq_bot_setting.recall_failed_messages'] ?? false,
          'qq_bot_setting.recall_delay_seconds':
            settings['qq_bot_setting.recall_delay_seconds'] ?? 10,
          'qq_bot_setting.admin_open_ids':
            settings['qq_bot_setting.admin_open_ids'] ?? '',
        }}
      />
    ),
  },
]

const generalRegistry = createSectionRegistry({
  sections: GENERAL_SECTIONS,
  defaultSection: 'qqbot',
  basePath: '/system-settings/general',
})

export const getGeneralSectionNavItems = generalRegistry.getSectionNavItems
export const GENERAL_SECTION_IDS = generalRegistry.sectionIds
export const GENERAL_DEFAULT_SECTION = generalRegistry.defaultSection
export const getGeneralSectionContent = generalRegistry.getSectionContent

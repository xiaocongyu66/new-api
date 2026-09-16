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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { SettingsSection } from '../../components/settings-section'
import {
  getOptionValue,
  useSystemOptions,
} from '../../hooks/use-system-options'

import { CleanupPreviewPanel } from './cleanup-preview-panel'
import { CleanupSettingsForm } from './cleanup-settings-form'

const DEFAULT_CLEANUP_VALUES = {
  'qq_bot_setting.cleanup_enabled': false,
  'qq_bot_setting.cleanup_groups': '',
  'qq_bot_setting.napcat_onebot_http_address': '',
  'qq_bot_setting.napcat_onebot_access_token': '',
  'qq_bot_setting.cleanup_inactive_days': 30,
  'qq_bot_setting.cleanup_grace_days': 7,
  'qq_bot_setting.cleanup_warning_template': '',
  'qq_bot_setting.cleanup_kick_min_seconds': 120,
  'qq_bot_setting.cleanup_kick_max_seconds': 300,
  'qq_bot_setting.cleanup_batch_size': 20,
  'qq_bot_setting.cleanup_dry_run': true,
  'qq_bot_setting.cleanup_exempt_bound_users': true,
  'qq_bot_setting.cleanup_warn_hours': 24,
  'qq_bot_setting.cleanup_group_numbers': '',
} as const

export function QQBotCleanupTab() {
  const { t } = useTranslation()

  const systemOptionsQuery = useSystemOptions()
  const cleanupDefaults = useMemo(
    () => getOptionValue(systemOptionsQuery.data?.data, DEFAULT_CLEANUP_VALUES),
    [systemOptionsQuery.data]
  )

  // 保存按钮沿用外层 SettingsPage 的 Provider（与同 section 下其它表单一致），
  // 这里不再嵌套 Provider，否则按钮会 portal 到本 tab 内部而非页面头部。
  return (
    <SettingsSection title={t('Inactive member cleanup')}>
      {/* 名单预览与配置分开两页：踢人是高风险操作，预览页不应让管理员
          在长表单里误触触发按钮。 */}
      <Tabs defaultValue='preview'>
        <TabsList>
          <TabsTrigger value='preview'>{t('Candidate preview')}</TabsTrigger>
          <TabsTrigger value='settings'>{t('Cleanup settings')}</TabsTrigger>
        </TabsList>
        <TabsContent value='preview' className='pt-4'>
          <CleanupPreviewPanel />
        </TabsContent>
        <TabsContent value='settings' className='pt-4'>
          <CleanupSettingsForm defaultValues={cleanupDefaults} />
        </TabsContent>
      </Tabs>
    </SettingsSection>
  )
}

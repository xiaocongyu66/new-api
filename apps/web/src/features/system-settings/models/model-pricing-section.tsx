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

import {
  getOptionValue,
  useSystemOptions,
} from '../hooks/use-system-options'
import { RatioSettingsCard, type RatioTabId } from './ratio-settings-card'

/**
 * Option keys the pricing card reads. `getOptionValue` only copies keys that
 * exist in the defaults, so every option the card needs has to be listed.
 */
const modelPricingDefaults = {
  ModelPrice: '',
  ModelRatio: '',
  CacheRatio: '',
  CreateCacheRatio: '',
  CompletionRatio: '',
  CompletionRatioMeta: '',
  ImageRatio: '',
  AudioRatio: '',
  AudioCompletionRatio: '',
  ExposeRatioEnabled: false,
  'billing_setting.billing_mode': '',
  'billing_setting.billing_expr': '',
  'tool_price_setting.prices': '',
}

/**
 * Model pricing editor, wired to the current system options.
 *
 * `tabs` decides how much of the editor this instance owns: the billing settings
 * page passes all three and keeps the card's own switcher, while the channels
 * page owns the tab row and passes a single tab so the card renders just that
 * view (a one-tab card drops its internal switcher).
 */
export function ModelPricingSection({
  tabs = ['models', 'tool-prices', 'upstream-sync'],
}: {
  tabs?: RatioTabId[]
}) {
  const { data } = useSystemOptions()

  const settings = useMemo(
    () => getOptionValue(data?.data, modelPricingDefaults),
    [data?.data]
  )

  return (
    <RatioSettingsCard
      titleKey='Pricing'
      modelDefaults={{
        ModelPrice: settings.ModelPrice,
        ModelRatio: settings.ModelRatio,
        CacheRatio: settings.CacheRatio,
        CreateCacheRatio: settings.CreateCacheRatio,
        CompletionRatio: settings.CompletionRatio,
        ImageRatio: settings.ImageRatio,
        AudioRatio: settings.AudioRatio,
        AudioCompletionRatio: settings.AudioCompletionRatio,
        ExposeRatioEnabled: settings.ExposeRatioEnabled,
        BillingMode: settings['billing_setting.billing_mode'],
        BillingExpr: settings['billing_setting.billing_expr'],
      }}
      groupDefaults={{
        TopupGroupRatio: '',
        GroupRatio: '',
        UserUsableGroups: '',
        GroupGroupRatio: '',
        AutoGroups: '[]',
        MaxTokenAutoGroups: 1,
        DefaultUseAutoGroup: false,
        GroupSpecialUsableGroup: '',
      }}
      toolPricesDefault={settings['tool_price_setting.prices']}
      completionRatioMeta={settings.CompletionRatioMeta}
      visibleTabs={tabs}
    />
  )
}

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
import { parseCurrencyDisplayType } from '@/lib/currency'

import { CheckinSettingsSection } from '../general/checkin-settings-section'
import { PricingSection } from '../general/pricing-section'
import { QQBotSettingsSection } from '../general/qqbot-settings-section'
import { QuotaSettingsSection } from '../general/quota-settings-section'
import { PaymentSettingsSection } from '../integrations/payment-settings-section'
import { RatioSettingsCard } from '../models/ratio-settings-card'
import type { BillingSettings } from '../types'
import { createSectionRegistry } from '../utils/section-registry'

const getModelDefaults = (settings: BillingSettings) => ({
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
})

const getGroupDefaults = (settings: BillingSettings) => ({
  TopupGroupRatio: settings.TopupGroupRatio,
  GroupRatio: settings.GroupRatio,
  UserUsableGroups: settings.UserUsableGroups,
  GroupGroupRatio: settings.GroupGroupRatio,
  AutoGroups: settings.AutoGroups,
  MaxTokenAutoGroups: settings.MaxTokenAutoGroups,
  DefaultUseAutoGroup: settings.DefaultUseAutoGroup,
  GroupSpecialUsableGroup:
    settings['group_ratio_setting.group_special_usable_group'],
})

const BILLING_SECTIONS = [
  {
    id: 'quota',
    titleKey: 'Quota Settings',
    build: (settings: BillingSettings) => (
      <QuotaSettingsSection
        defaultValues={{
          QuotaForNewUser: settings.QuotaForNewUser,
          PreConsumedQuota: settings.PreConsumedQuota,
          QuotaForInviter: settings.QuotaForInviter,
          QuotaForInvitee: settings.QuotaForInvitee,
          TopUpLink: settings.TopUpLink,
          general_setting: {
            docs_link: settings['general_setting.docs_link'],
          },
          quota_setting: {
            enable_free_model_pre_consume:
              settings['quota_setting.enable_free_model_pre_consume'],
          },
        }}
        complianceConfirmed={
          (settings['payment_setting.compliance_confirmed'] ?? false) &&
          settings['payment_setting.compliance_terms_version'] === 'v1'
        }
      />
    ),
  },
  {
    id: 'currency',
    titleKey: 'Currency & Display',
    build: (settings: BillingSettings) => (
      <PricingSection
        defaultValues={{
          QuotaPerUnit: settings.QuotaPerUnit,
          USDExchangeRate: settings.USDExchangeRate,
          DisplayInCurrencyEnabled: settings.DisplayInCurrencyEnabled,
          DisplayTokenStatEnabled: settings.DisplayTokenStatEnabled,
          general_setting: {
            quota_display_type: parseCurrencyDisplayType(
              settings['general_setting.quota_display_type']
            ),
            custom_currency_symbol:
              settings['general_setting.custom_currency_symbol'] ?? '¤',
            custom_currency_exchange_rate:
              settings['general_setting.custom_currency_exchange_rate'] ?? 1,
          },
        }}
      />
    ),
  },
  {
    id: 'model-pricing',
    titleKey: 'Model Pricing',
    build: (settings: BillingSettings) => (
      <RatioSettingsCard
        titleKey='Model Pricing'
        modelDefaults={getModelDefaults(settings)}
        groupDefaults={getGroupDefaults(settings)}
        toolPricesDefault={settings['tool_price_setting.prices']}
        visibleTabs={['models', 'unset-models', 'tool-prices', 'upstream-sync']}
      />
    ),
  },
  {
    id: 'group-pricing',
    titleKey: 'Group Pricing',
    build: (settings: BillingSettings) => (
      <RatioSettingsCard
        titleKey='Group Pricing'
        modelDefaults={getModelDefaults(settings)}
        groupDefaults={getGroupDefaults(settings)}
        toolPricesDefault={settings['tool_price_setting.prices']}
        visibleTabs={['groups']}
      />
    ),
  },
  {
    id: 'payment',
    titleKey: 'Payment Gateway',
    build: (settings: BillingSettings) => (
      <PaymentSettingsSection
        defaultValues={{
          PayAddress: settings.PayAddress,
          EpayId: settings.EpayId,
          EpayKey: settings.EpayKey,
          Price: settings.Price,
          MinTopUp: settings.MinTopUp,
          CustomCallbackAddress: settings.CustomCallbackAddress,
          PayMethods: settings.PayMethods,
          AmountOptions: settings['payment_setting.amount_options'],
          AmountDiscount: settings['payment_setting.amount_discount'],
          StripeApiSecret: settings.StripeApiSecret,
          StripeWebhookSecret: settings.StripeWebhookSecret,
          StripePriceId: settings.StripePriceId,
          StripeUnitPrice: settings.StripeUnitPrice,
          StripeMinTopUp: settings.StripeMinTopUp,
          StripePromotionCodesEnabled: settings.StripePromotionCodesEnabled,
          CreemApiKey: settings.CreemApiKey,
          CreemWebhookSecret: settings.CreemWebhookSecret,
          CreemTestMode: settings.CreemTestMode,
          CreemProducts: settings.CreemProducts,
        }}
        waffoDefaultValues={{
          WaffoEnabled: settings.WaffoEnabled ?? false,
          WaffoApiKey: settings.WaffoApiKey ?? '',
          WaffoPrivateKey: settings.WaffoPrivateKey ?? '',
          WaffoPublicCert: settings.WaffoPublicCert ?? '',
          WaffoSandboxPublicCert: settings.WaffoSandboxPublicCert ?? '',
          WaffoSandboxApiKey: settings.WaffoSandboxApiKey ?? '',
          WaffoSandboxPrivateKey: settings.WaffoSandboxPrivateKey ?? '',
          WaffoSandbox: settings.WaffoSandbox ?? false,
          WaffoMerchantId: settings.WaffoMerchantId ?? '',
          WaffoCurrency: settings.WaffoCurrency ?? 'USD',
          WaffoUnitPrice: settings.WaffoUnitPrice ?? 1,
          WaffoMinTopUp: settings.WaffoMinTopUp ?? 1,
          WaffoNotifyUrl: settings.WaffoNotifyUrl ?? '',
          WaffoReturnUrl: settings.WaffoReturnUrl ?? '',
          WaffoPayMethods: settings.WaffoPayMethods ?? '[]',
        }}
        waffoPancakeDefaultValues={{
          WaffoPancakeMerchantID: settings.WaffoPancakeMerchantID ?? '',
          WaffoPancakePrivateKey: settings.WaffoPancakePrivateKey ?? '',
          WaffoPancakeReturnURL: settings.WaffoPancakeReturnURL ?? '',
        }}
        waffoPancakeProvisionedStoreID={settings.WaffoPancakeStoreID ?? ''}
        waffoPancakeProvisionedProductID={settings.WaffoPancakeProductID ?? ''}
        complianceDefaults={{
          confirmed: settings['payment_setting.compliance_confirmed'] ?? false,
          termsVersion:
            settings['payment_setting.compliance_terms_version'] ?? '',
          confirmedAt: settings['payment_setting.compliance_confirmed_at'] ?? 0,
          confirmedBy: settings['payment_setting.compliance_confirmed_by'] ?? 0,
        }}
      />
    ),
  },
  {
    id: 'checkin',
    titleKey: 'Check-in Rewards',
    build: (settings: BillingSettings) => (
      <CheckinSettingsSection
        defaultValues={{
          enabled: settings['checkin_setting.enabled'],
          minQuota: settings['checkin_setting.min_quota'],
          maxQuota: settings['checkin_setting.max_quota'],
        }}
      />
    ),
  },
  {
    id: 'qqbot',
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
          'qq_bot_setting.min_quota':
            settings['qq_bot_setting.min_quota'] ?? 1000,
          'qq_bot_setting.max_quota':
            settings['qq_bot_setting.max_quota'] ?? 10000,
          'qq_bot_setting.checkin_disabled_groups':
            settings['qq_bot_setting.checkin_disabled_groups'] ?? '',
          'qq_bot_setting.notify_template':
            settings['qq_bot_setting.notify_template'] ?? '',
          'qq_bot_setting.auto_approve_enabled':
            settings['qq_bot_setting.auto_approve_enabled'] ?? false,
          'qq_bot_setting.auto_approve_keyword':
            settings['qq_bot_setting.auto_approve_keyword'] ?? '',
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
        }}
      />
    ),
  },
] as const

export type BillingSectionId = (typeof BILLING_SECTIONS)[number]['id']

const billingRegistry = createSectionRegistry<
  BillingSectionId,
  BillingSettings
>({
  sections: BILLING_SECTIONS,
  defaultSection: 'quota',
  basePath: '/system-settings/billing',
  urlStyle: 'path',
})

export const BILLING_SECTION_IDS = billingRegistry.sectionIds
export const BILLING_DEFAULT_SECTION = billingRegistry.defaultSection
export const getBillingSectionNavItems = billingRegistry.getSectionNavItems
export const getBillingSectionContent = billingRegistry.getSectionContent
export const getBillingSectionMeta = billingRegistry.getSectionMeta

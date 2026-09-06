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
import { SettingsPage } from '../components/settings-page'
import type { BillingSettings } from '../types'
import {
  BILLING_DEFAULT_SECTION,
  getBillingSectionContent,
  getBillingSectionMeta,
} from './section-registry.tsx'

const defaultBillingSettings: BillingSettings = {
  QuotaForNewUser: 0,
  PreConsumedQuota: 0,
  QuotaForInviter: 0,
  QuotaForInvitee: 0,
  TopUpLink: '',
  'general_setting.docs_link': '',
  'quota_setting.enable_free_model_pre_consume': true,
  QuotaPerUnit: 500000,
  USDExchangeRate: 7,
  'general_setting.quota_display_type': 'USD',
  'general_setting.custom_currency_symbol': '¤',
  'general_setting.custom_currency_exchange_rate': 1,
  DisplayInCurrencyEnabled: true,
  DisplayTokenStatEnabled: true,
  ModelPrice: '',
  ModelRatio: '',
  CacheRatio: '',
  CreateCacheRatio: '',
  CompletionRatio: '',
  ImageRatio: '',
  AudioRatio: '',
  AudioCompletionRatio: '',
  ExposeRatioEnabled: false,
  'billing_setting.billing_mode': '{}',
  'billing_setting.billing_expr': '{}',
  'tool_price_setting.prices': '{}',
  TopupGroupRatio: '',
  GroupRatio: '',
  UserUsableGroups: '',
  GroupGroupRatio: '',
  AutoGroups: '',
  MaxTokenAutoGroups: 5,
  DefaultUseAutoGroup: false,
  'group_ratio_setting.group_special_usable_group': '{}',
  PayAddress: '',
  EpayId: '',
  EpayKey: '',
  Price: 7.3,
  MinTopUp: 1,
  CustomCallbackAddress: '',
  PayMethods: '',
  'payment_setting.amount_options': '',
  'payment_setting.amount_discount': '',
  'payment_setting.compliance_confirmed': false,
  'payment_setting.compliance_terms_version': '',
  'payment_setting.compliance_confirmed_at': 0,
  'payment_setting.compliance_confirmed_by': 0,
  'payment_setting.compliance_confirmed_ip': '',
  StripeApiSecret: '',
  StripeWebhookSecret: '',
  StripePriceId: '',
  StripeUnitPrice: 8.0,
  StripeMinTopUp: 1,
  StripePromotionCodesEnabled: false,
  CreemApiKey: '',
  CreemWebhookSecret: '',
  CreemTestMode: false,
  CreemProducts: '[]',
  WaffoEnabled: false,
  WaffoApiKey: '',
  WaffoPrivateKey: '',
  WaffoPublicCert: '',
  WaffoSandboxPublicCert: '',
  WaffoSandboxApiKey: '',
  WaffoSandboxPrivateKey: '',
  WaffoSandbox: false,
  WaffoMerchantId: '',
  WaffoCurrency: 'USD',
  WaffoUnitPrice: 1,
  WaffoMinTopUp: 1,
  WaffoNotifyUrl: '',
  WaffoReturnUrl: '',
  WaffoPayMethods: '[]',
  WaffoPancakeMerchantID: '',
  WaffoPancakePrivateKey: '',
  WaffoPancakeReturnURL: '',
  WaffoPancakeStoreID: '',
  WaffoPancakeProductID: '',
  'checkin_setting.enabled': false,
  'checkin_setting.min_quota': 1000,
  'checkin_setting.max_quota': 10000,
  'qq_bot_setting.app_id': '',
  'qq_bot_setting.app_secret': '',
  'qq_bot_setting.qq_checkin_enabled': false,
  'qq_bot_setting.web_checkin_enabled': true,
  'qq_bot_setting.single_platform_only': true,
  'qq_bot_setting.min_quota': 1000,
  'qq_bot_setting.max_quota': 10000,
  'qq_bot_setting.checkin_disabled_groups': '',
  'qq_bot_setting.notify_template': '',
  'qq_bot_setting.auto_approve_enabled': false,
  'qq_bot_setting.auto_approve_keyword': '',
  'qq_bot_setting.drop_enabled': false,
  'qq_bot_setting.drop_groups': '',
  'qq_bot_setting.drop_min_messages': 5,
  'qq_bot_setting.drop_max_messages': 30,
  'qq_bot_setting.drop_min_quota': 150000,
  'qq_bot_setting.drop_max_quota': 1500000,
  'qq_bot_setting.drop_daily_limit': 3,
  'qq_bot_setting.drop_template': '',
  'qq_bot_setting.transfer_enabled': false,
  'qq_bot_setting.transfer_disabled_groups': '',
  'qq_bot_setting.transfer_daily_limit': 2,
  'qq_bot_setting.transfer_min_amount': 50000,
  'qq_bot_setting.transfer_max_amount': 50000000,
  'qq_bot_setting.transfer_fee_brackets': '',
  'qq_bot_setting.red_packet_enabled': false,
  'qq_bot_setting.red_packet_disabled_groups': '',
  'qq_bot_setting.red_packet_daily_limit': 3,
  'qq_bot_setting.red_packet_min_amount': 500000,
  'qq_bot_setting.red_packet_max_amount': 100000000,
  'qq_bot_setting.red_packet_default_count': 5,
  'qq_bot_setting.red_packet_max_count': 50,
  'qq_bot_setting.red_packet_expire_seconds': 86400,
  'qq_bot_setting.red_packet_allow_own_grab': false,
}

export function BillingSettings() {
  return (
    <SettingsPage
      routePath='/_authenticated/system-settings/billing/$section'
      defaultSettings={defaultBillingSettings}
      defaultSection={BILLING_DEFAULT_SECTION}
      getSectionContent={getBillingSectionContent}
      getSectionMeta={getBillingSectionMeta}
    />
  )
}

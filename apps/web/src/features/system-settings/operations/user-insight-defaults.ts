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
import * as z from 'zod'

// Keys mirror UserInsightSetting in apps/api/internal/usage/user_insight_setting.go.
//
// react-hook-form interprets dotted names as nested paths. Declaring the
// schema with literal flat keys like `'user_insight_setting.enabled'` makes
// the form state diverge from what zod validates, so saves silently turn
// into no-ops. We model the form internally as a nested object and flatten
// back to the server-side dotted key format only right before persisting
// (same treatment as the performance settings form).
export const insightSchema = z.object({
  user_insight_setting: z.object({
    enabled: z.boolean(),
    record_in_log: z.boolean(),
    gender_inference_enabled: z.boolean(),
    jailbreak_alert_score: z.coerce.number().int().min(1).max(100),
    sample_enabled: z.boolean(),
    sample_rate_percent: z.coerce.number().int().min(0).max(100),
    sample_keep_body: z.boolean(),
    sample_quota_mb: z.coerce.number().int().min(1),
    sample_retention_days: z.coerce.number().int().min(0),
    auto_ban_enabled: z.boolean(),
    auto_ban_min_risk: z.enum(['suspect', 'likely', 'confirmed']),
    auto_ban_code_ratio_enabled: z.boolean(),
    auto_ban_code_ratio_percent: z.coerce.number().int().min(1).max(100),
    auto_ban_code_min_requests: z.coerce.number().int().min(1),
  }),
})

export type InsightFormValues = z.infer<typeof insightSchema>

// Flat server-shaped option record (dotted keys), as served by GET /api/option.
export type InsightFlatDefaults = {
  'user_insight_setting.enabled': boolean
  'user_insight_setting.record_in_log': boolean
  'user_insight_setting.gender_inference_enabled': boolean
  'user_insight_setting.jailbreak_alert_score': number
  'user_insight_setting.sample_enabled': boolean
  'user_insight_setting.sample_rate_percent': number
  'user_insight_setting.sample_keep_body': boolean
  'user_insight_setting.sample_quota_mb': number
  'user_insight_setting.sample_retention_days': number
  'user_insight_setting.auto_ban_enabled': boolean
  'user_insight_setting.auto_ban_min_risk': 'suspect' | 'likely' | 'confirmed'
  'user_insight_setting.auto_ban_code_ratio_enabled': boolean
  'user_insight_setting.auto_ban_code_ratio_percent': number
  'user_insight_setting.auto_ban_code_min_requests': number
}

export const DEFAULT_INSIGHT_VALUES: InsightFlatDefaults = {
  'user_insight_setting.enabled': true,
  'user_insight_setting.record_in_log': true,
  'user_insight_setting.gender_inference_enabled': true,
  'user_insight_setting.jailbreak_alert_score': 70,
  'user_insight_setting.sample_enabled': true,
  'user_insight_setting.sample_rate_percent': 5,
  'user_insight_setting.sample_keep_body': false,
  'user_insight_setting.sample_quota_mb': 1024,
  'user_insight_setting.sample_retention_days': 30,
  'user_insight_setting.auto_ban_enabled': false,
  'user_insight_setting.auto_ban_min_risk': 'confirmed',
  'user_insight_setting.auto_ban_code_ratio_enabled': false,
  'user_insight_setting.auto_ban_code_ratio_percent': 80,
  'user_insight_setting.auto_ban_code_min_requests': 10,
}

// Flat (server) shape -> nested (form) shape.
export const buildInsightFormDefaults = (
  defaults: InsightFlatDefaults
): InsightFormValues => ({
  user_insight_setting: {
    enabled: defaults['user_insight_setting.enabled'],
    record_in_log: defaults['user_insight_setting.record_in_log'],
    gender_inference_enabled:
      defaults['user_insight_setting.gender_inference_enabled'],
    jailbreak_alert_score:
      defaults['user_insight_setting.jailbreak_alert_score'],
    sample_enabled: defaults['user_insight_setting.sample_enabled'],
    sample_rate_percent: defaults['user_insight_setting.sample_rate_percent'],
    sample_keep_body: defaults['user_insight_setting.sample_keep_body'],
    sample_quota_mb: defaults['user_insight_setting.sample_quota_mb'],
    sample_retention_days:
      defaults['user_insight_setting.sample_retention_days'],
    auto_ban_enabled: defaults['user_insight_setting.auto_ban_enabled'],
    auto_ban_min_risk: defaults['user_insight_setting.auto_ban_min_risk'],
    auto_ban_code_ratio_enabled:
      defaults['user_insight_setting.auto_ban_code_ratio_enabled'],
    auto_ban_code_ratio_percent:
      defaults['user_insight_setting.auto_ban_code_ratio_percent'],
    auto_ban_code_min_requests:
      defaults['user_insight_setting.auto_ban_code_min_requests'],
  },
})

// Nested (form) shape -> flat (server) shape, for persisting.
export const normalizeInsightFormValues = (
  values: InsightFormValues
): InsightFlatDefaults => ({
  'user_insight_setting.enabled': values.user_insight_setting.enabled,
  'user_insight_setting.record_in_log':
    values.user_insight_setting.record_in_log,
  'user_insight_setting.gender_inference_enabled':
    values.user_insight_setting.gender_inference_enabled,
  'user_insight_setting.jailbreak_alert_score':
    values.user_insight_setting.jailbreak_alert_score,
  'user_insight_setting.sample_enabled':
    values.user_insight_setting.sample_enabled,
  'user_insight_setting.sample_rate_percent':
    values.user_insight_setting.sample_rate_percent,
  'user_insight_setting.sample_keep_body':
    values.user_insight_setting.sample_keep_body,
  'user_insight_setting.sample_quota_mb':
    values.user_insight_setting.sample_quota_mb,
  'user_insight_setting.sample_retention_days':
    values.user_insight_setting.sample_retention_days,
  'user_insight_setting.auto_ban_enabled':
    values.user_insight_setting.auto_ban_enabled,
  'user_insight_setting.auto_ban_min_risk':
    values.user_insight_setting.auto_ban_min_risk,
  'user_insight_setting.auto_ban_code_ratio_enabled':
    values.user_insight_setting.auto_ban_code_ratio_enabled,
  'user_insight_setting.auto_ban_code_ratio_percent':
    values.user_insight_setting.auto_ban_code_ratio_percent,
  'user_insight_setting.auto_ban_code_min_requests':
    values.user_insight_setting.auto_ban_code_min_requests,
})

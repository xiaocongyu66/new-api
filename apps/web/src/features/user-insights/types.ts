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

/** 用途类别，与后端 service/insight 常量一致。 */
export type InsightCategory =
  | 'code'
  | 'roleplay'
  | 'qa'
  | 'translate'
  | 'embedding'
  | 'other'

/** 代码方向。 */
export type InsightStack =
  | 'frontend'
  | 'backend'
  | 'fullstack'
  | 'infra'
  | 'mobile'
  | 'data'
  | 'unknown'

/** 破甲风险等级。 */
export type InsightRisk = 'none' | 'suspect' | 'likely' | 'confirmed'

/** 性别推断结果。 */
export type InsightGender = 'male' | 'female' | 'unknown'

export type InsightRatios = {
  code_ratio: number
  roleplay_ratio: number
  qa_ratio: number
  translate_ratio: number
  other_ratio: number
  frontend_ratio: number
  backend_ratio: number
  male_ratio: number
  female_ratio: number
}

export type InsightClientUsage = {
  client: string
  count: number
  version?: string
}

export type UserInsight = {
  user_id: number
  username: string
  display_name?: string
  status: number
  role: number
  group?: string

  first_seen_at: number
  last_seen_at: number
  total_requests: number

  code_requests: number
  roleplay_requests: number
  qa_requests: number
  translate_requests: number
  embedding_requests: number
  other_requests: number

  primary_category: InsightCategory
  primary_stack: InsightStack
  ratios: InsightRatios

  guessed_gender: InsightGender
  gender_confidence: number
  // 性别结论的依据强度：self_report / preference / inverse。
  // 前端据此标注可信度——inverse 只是群体先验，不能当处置依据。
  gender_basis?: string
  ai_female_requests: number
  ai_male_requests: number

  risk_level: InsightRisk
  jailbreak_suspect: number
  jailbreak_likely: number
  jailbreak_confirmed: number
  jailbreak_max_score: number
  last_jailbreak_at: number
  jailbreak_tags?: Record<string, number>

  relay_requests: number
  relay_vendors?: Record<string, number>

  clients?: InsightClientUsage[]
  languages?: Record<string, number>
}

export type UserInsightListData = {
  items: UserInsight[]
  total: number
  page: number
  size: number
}

export type UserInsightSummary = {
  total_users: number
  coders: number
  roleplayers: number
  qa_users: number
  frontend_leaning: number
  backend_leaning: number
  male_leaning: number
  female_leaning: number
  relay_users: number
  risky_users: number
  clients: Record<string, number>
}

export type UserInsightFilters = {
  category?: InsightCategory | ''
  risk?: InsightRisk | ''
  relayOnly?: boolean
  sort?: 'last_seen' | 'requests' | 'risk' | 'roleplay' | 'code'
  /** 按用户名或用户 ID 子串搜索。后端只匹配这两项，不含显示名。 */
  keyword?: string
}

/** 清除画像的结果，用于在提示里回显实际删掉了什么。 */
export type PurgeInsightResult = {
  profile_deleted: boolean
  samples_deleted: number
}

/** 证据类别，与后端 service/insight 的 Evidence* 常量一致。 */
export type InsightEvidenceKind =
  | 'jailbreak'
  | 'code'
  | 'roleplay'
  | 'qa'
  | 'translate'
  | 'stack'
  | 'gender'
  | 'client'

/** 一条关键词命中记录：命中了什么词、在哪一段、原句是什么。 */
export type InsightEvidence = {
  kind: InsightEvidenceKind
  tag?: string
  keyword: string
  snippet: string
  offset?: number
  /** system = 系统提示词段，conversation = 用户对话段。 */
  section?: 'system' | 'conversation'
}

/** 单条复核样本。列表接口不返回 body，详情接口才带。 */
export type InsightSample = {
  id: number
  user_id: number
  username: string
  created_at: number
  request_id?: string
  model_name?: string
  path?: string

  category?: InsightCategory
  client?: string
  client_version?: string
  is_relay: boolean
  relay_vendor?: string

  risk_level?: InsightRisk
  jailbreak_score: number
  guess_gender?: InsightGender

  evidence?: InsightEvidence[]
  evidence_count: number

  /** 同一命中模式累计出现的请求次数（去重后一行代表一种模式）。 */
  hit_count: number
  /** 该模式最近一次出现的时间。 */
  last_seen_at: number

  /** 请求体原文，仅详情接口且开启完整留存时才有值。 */
  body?: string
  /** 原始请求体大小，可能大于 body 长度（只留前缀）。 */
  body_size: number
  byte_size: number
}

/** 按用户聚合的样本概览：一个用户一行，展开才拉具体证据。 */
export type InsightSampleGroup = {
  user_id: number
  username: string
  /** 该用户的去重样本行数（命中模式数）。 */
  samples: number
  /** 这些模式累计对应的请求次数。 */
  hit_total: number
  max_risk: InsightRisk
  max_score: number
  last_seen_at: number
  byte_size: number
}

export type InsightSampleGroupListData = {
  items: InsightSampleGroup[]
  total: number
  page: number
  size: number
  quota: InsightSampleQuota
}

/** 样本缓存库的容量水位与当前策略。 */
export type InsightSampleQuota = {
  used_bytes: number
  limit_bytes: number
  keep_body: boolean
  sample_rate: number
  enabled: boolean
}

export type InsightSampleListData = {
  items: InsightSample[]
  total: number
  page: number
  size: number
  quota: InsightSampleQuota
}

export type InsightSampleFilters = {
  userId?: number
  risk?: InsightRisk | ''
  relayOnly?: boolean
}

export type ApiResponse<T> = {
  success: boolean
  message?: string
  data: T
}
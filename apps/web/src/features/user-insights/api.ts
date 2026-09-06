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

import { api } from '@/lib/api'
import type {
  ApiResponse,
  InsightSample,
  InsightSampleFilters,
  InsightSampleGroupListData,
  InsightSampleListData,
  PurgeInsightResult,
  UserInsight,
  UserInsightFilters,
  UserInsightListData,
  UserInsightSummary,
} from './types'

/** 拉取用户画像列表。 */
export async function getUserInsights(
  page: number,
  pageSize: number,
  filters: UserInsightFilters = {}
): Promise<ApiResponse<UserInsightListData>> {
  const res = await api.get('/api/user-insight', {
    params: {
      p: page,
      page_size: pageSize,
      category: filters.category || undefined,
      risk: filters.risk || undefined,
      relay_only: filters.relayOnly ? 'true' : undefined,
      sort: filters.sort || undefined,
      keyword: filters.keyword || undefined,
    },
  })
  return res.data
}

/** 拉取站点级画像汇总。 */
export async function getUserInsightSummary(): Promise<
  ApiResponse<UserInsightSummary>
> {
  const res = await api.get('/api/user-insight/summary')
  return res.data
}

/** 拉取单个用户的画像详情。 */
export async function getUserInsightDetail(
  userId: number
): Promise<ApiResponse<UserInsight | null>> {
  const res = await api.get(`/api/user-insight/${userId}`)
  return res.data
}

/**
 * 清除某用户的全部画像。
 *
 * 一次清干净：聚合画像行、证据样本、尚未落库的内存增量与自动封禁标记。
 * 不可逆，调用方必须先做二次确认。
 */
export async function purgeUserInsight(
  userId: number
): Promise<ApiResponse<PurgeInsightResult>> {
  const res = await api.delete(`/api/user-insight/${userId}`)
  return res.data
}

/**
 * 封禁 / 解封用户。
 *
 * 复用既有的用户管理接口，避免为看板单独开一条权限口子：
 * /api/user/manage 已有 AdminAuth 与角色校验，并会写入操作审计日志。
 */
export async function setUserBanned(
  userId: number,
  banned: boolean
): Promise<ApiResponse<unknown>> {
  const res = await api.post('/api/user/manage', {
    id: userId,
    action: banned ? 'disable' : 'enable',
  })
  return res.data
}

/**
 * 拉取复核样本列表。
 *
 * 列表不含请求体原文（后端 Omit body），因此可以放心分页浏览。
 */
export async function getInsightSamples(
  page: number,
  pageSize: number,
  filters: InsightSampleFilters = {}
): Promise<ApiResponse<InsightSampleListData>> {
  const res = await api.get('/api/insight-sample', {
    params: {
      p: page,
      page_size: pageSize,
      user_id: filters.userId || undefined,
      risk: filters.risk || undefined,
      relay_only: filters.relayOnly ? 'true' : undefined,
    },
  })
  return res.data
}

/**
 * 拉取按用户聚合的样本概览。
 *
 * 独立的路由前缀（insight-sample-group）而不是 /insight-sample/groups：
 * gin 不允许同一层路由同时存在静态段与 :id 通配段。
 */
export async function getInsightSampleGroups(
  page: number,
  pageSize: number,
  filters: Omit<InsightSampleFilters, 'userId'> = {}
): Promise<ApiResponse<InsightSampleGroupListData>> {
  const res = await api.get('/api/insight-sample-group', {
    params: {
      p: page,
      page_size: pageSize,
      risk: filters.risk || undefined,
      relay_only: filters.relayOnly ? 'true' : undefined,
    },
  })
  return res.data
}

/** 拉取单条样本详情，含请求体原文（若已留存）。 */
export async function getInsightSampleDetail(
  id: number
): Promise<ApiResponse<InsightSample>> {
  const res = await api.get(`/api/insight-sample/${id}`)
  return res.data
}

/**
 * 清除某用户的全部样本。
 *
 * 样本里含提示词原文，用户提出异议时需要能立即删除。
 */
export async function deleteUserInsightSamples(
  userId: number
): Promise<ApiResponse<{ deleted: number }>> {
  const res = await api.delete(`/api/insight-sample/user/${userId}`)
  return res.data
}
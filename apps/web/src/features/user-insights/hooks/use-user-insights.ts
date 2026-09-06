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

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { useTranslation } from 'react-i18next'
import {
  deleteUserInsightSamples,
  getInsightSampleDetail,
  getInsightSampleGroups,
  getInsightSamples,
  getUserInsightSummary,
  getUserInsights,
  purgeUserInsight,
  setUserBanned,
} from '../api'
import type { InsightSampleFilters, UserInsightFilters } from '../types'

const INSIGHTS_KEY = 'user-insights'
const SAMPLES_KEY = 'insight-samples'

/** 画像列表查询。筛选条件变化时自动重新拉取。 */
export function useUserInsights(
  page: number,
  pageSize: number,
  filters: UserInsightFilters
) {
  return useQuery({
    queryKey: [INSIGHTS_KEY, 'list', page, pageSize, filters],
    queryFn: () => getUserInsights(page, pageSize, filters),
    // 画像每分钟落库一次，前端无需更频繁地轮询。
    staleTime: 30_000,
    // 页面开着不动时也定时拉取，让新落库的画像自动出现，不用手动刷新。
    refetchInterval: 30_000,
    // 后台标签页停止轮询，切回来时立即补拉一次，避免看到过期数据。
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: true,
  })
}

/** 站点级画像汇总查询。 */
export function useUserInsightSummary() {
  return useQuery({
    queryKey: [INSIGHTS_KEY, 'summary'],
    queryFn: getUserInsightSummary,
    staleTime: 60_000,
    refetchInterval: 60_000,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: true,
  })
}

/**
 * 封禁 / 解封用户。
 *
 * 成功后同时失效画像列表与汇总缓存，让状态徽标立即反映最新结果。
 */
export function useToggleUserBan() {
  const queryClient = useQueryClient()
  const { t } = useTranslation()

  return useMutation({
    mutationFn: ({
      userId,
      banned,
    }: {
      userId: number
      banned: boolean
    }) => setUserBanned(userId, banned),
    onSuccess: (response, variables) => {
      if (!response.success) {
        toast.error(response.message || t('Operation failed'))
        return
      }
      toast.success(
        variables.banned ? t('User has been banned') : t('User has been unbanned')
      )
      queryClient.invalidateQueries({ queryKey: [INSIGHTS_KEY] })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t('Operation failed'))
    },
  })
}

/** 复核样本列表查询。 */
export function useInsightSamples(
  page: number,
  pageSize: number,
  filters: InsightSampleFilters
) {
  return useQuery({
    queryKey: [SAMPLES_KEY, 'list', page, pageSize, filters],
    queryFn: () => getInsightSamples(page, pageSize, filters),
    staleTime: 15_000,
  })
}

/**
 * 按用户聚合的样本概览查询。
 *
 * 这是样本面板的默认视图：一个用户一行。单个 agent 用户能产生几百条
 * 命中模式几乎相同的记录，平铺展示会把界面刷满。
 */
export function useInsightSampleGroups(
  page: number,
  pageSize: number,
  filters: Omit<InsightSampleFilters, 'userId'>
) {
  return useQuery({
    queryKey: [SAMPLES_KEY, 'groups', page, pageSize, filters],
    queryFn: () => getInsightSampleGroups(page, pageSize, filters),
    staleTime: 15_000,
    refetchInterval: 30_000,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: true,
  })
}

/**
 * 某个用户的样本明细查询。
 *
 * userId 为 null 时不发请求：分组行展开时才拉该用户的证据，
 * 避免一次把所有用户的明细都取回来。
 */
export function useUserInsightSamples(
  userId: number | null,
  filters: Omit<InsightSampleFilters, 'userId'>
) {
  return useQuery({
    queryKey: [SAMPLES_KEY, 'user', userId, filters],
    // 单个用户去重后的模式数很少（线上最多几十条），一页足够。
    queryFn: () =>
      getInsightSamples(1, 100, { ...filters, userId: userId as number }),
    enabled: userId !== null,
    staleTime: 15_000,
  })
}

/**
 * 单条样本详情查询（含请求体原文）。
 *
 * id 为 null 时不发请求，用于"点开抽屉才加载原文"的场景，
 * 避免列表渲染时把大段原文全部拉下来。
 */
export function useInsightSampleDetail(id: number | null) {
  return useQuery({
    queryKey: [SAMPLES_KEY, 'detail', id],
    queryFn: () => getInsightSampleDetail(id as number),
    enabled: id !== null,
    staleTime: 60_000,
  })
}

/** 清除指定用户的全部样本。 */
export function useDeleteUserSamples() {
  const queryClient = useQueryClient()
  const { t } = useTranslation()

  return useMutation({
    mutationFn: (userId: number) => deleteUserInsightSamples(userId),
    onSuccess: (response) => {
      if (!response.success) {
        toast.error(response.message || t('Operation failed'))
        return
      }
      toast.success(
        t('{{count}} samples deleted', { count: response.data?.deleted ?? 0 })
      )
      queryClient.invalidateQueries({ queryKey: [SAMPLES_KEY] })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t('Operation failed'))
    },
  })
}

/**
 * 清除某用户的全部画像。
 *
 * 成功后同时失效画像与样本两组缓存：这个操作会把两边的数据一起删掉，
 * 只失效一边会让另一个页签继续显示已经不存在的记录。
 */
export function usePurgeUserInsight() {
  const queryClient = useQueryClient()
  const { t } = useTranslation()

  return useMutation({
    mutationFn: (userId: number) => purgeUserInsight(userId),
    onSuccess: (response) => {
      if (!response.success) {
        toast.error(response.message || t('Operation failed'))
        return
      }
      toast.success(
        t('Profile cleared, {{count}} evidence samples removed', {
          count: response.data?.samples_deleted ?? 0,
        })
      )
      queryClient.invalidateQueries({ queryKey: [INSIGHTS_KEY] })
      queryClient.invalidateQueries({ queryKey: [SAMPLES_KEY] })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t('Operation failed'))
    },
  })
}
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

// 与 apps/api/internal/billing/qq_group_member.go 的成员状态机一一对应
export const MEMBER_STATUS = {
  active: 'active',
  warned: 'warned',
  removed: 'removed',
  exempt: 'exempt',
} as const

export type MemberStatus = (typeof MEMBER_STATUS)[keyof typeof MEMBER_STATUS]

export interface GroupMember {
  id: number
  group_open_id: string
  member_open_id: string
  username: string
  qq_number: number
  last_active_at: number
  warned_at: number
  status: MemberStatus
  created_at: number
  updated_at: number
  // 预览接口附加的豁免说明（不在表结构里）
  exempt?: boolean
  exempt_reason?: string
}

export interface CleanupPreview {
  members: GroupMember[]
  stats: Record<string, number>
  dry_run: boolean
}

export interface ApiResponse<T> {
  success: boolean
  message: string
  data: T
}

/** 只读预览某群的潜水候选名单，不执行任何操作。 */
export async function getCleanupPreview(
  groupOpenId: string
): Promise<ApiResponse<CleanupPreview>> {
  const res = await api.get('/api/user/qq/cleanup/preview', {
    params: { group_open_id: groupOpenId },
  })
  return res.data
}

/** 手动触发一次清理扫描，后端异步执行。 */
export async function runCleanup(
  groupOpenId: string
): Promise<ApiResponse<{ success: boolean; message: string }>> {
  const res = await api.post('/api/user/qq/cleanup/run', {
    group_open_id: groupOpenId,
  })
  return res.data
}

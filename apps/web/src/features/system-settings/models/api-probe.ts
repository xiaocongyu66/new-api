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
import { api, type ApiRequestConfig } from '@/lib/api'

/** 路由健康监控设置（后端 probe_setting.* option 域）。 */
export type ProbeSettings = {
  auto_probe_enabled: boolean
  probe_interval_minutes: number
  warning_threshold: number
  critical_threshold: number
  emergency_threshold: number
  webhook_enabled: boolean
  webhook_url: string
  /** 恒为空串：后端不回传密钥，空值提交表示保持已保存的值不变。 */
  webhook_secret: string
  webhook_secret_configured: boolean
}

/** 渠道级探测策略（存在渠道 settings JSON 里）。 */
export type ProbePolicy = {
  auto_control_enabled: boolean
  disable_below: number
  enable_above: number
}

/** 单个（渠道, 模型）探测单元的 24h 聚合。 */
export type ProbeModelStat = {
  model: string
  success_count: number
  total_count: number
  /** 百分比 0-100；null 表示 24h 内没有探测数据。 */
  availability: number | null
  /** Unix 秒，0 = 未探测。 */
  last_probe_at: number
  last_error_code: string
  /** 该模型当前是否参与路由。 */
  enabled: boolean
  /** 是否被自动控制（阈值）禁用；操作员手动禁用时为 false。 */
  auto_disabled: boolean
}

/** 单个启用渠道的探测概览行。禁用渠道不出现在列表里。 */
export type ProbeChannelStat = {
  channel_id: number
  name: string
  status: number
  availability: number | null
  success_count: number
  total_count: number
  /** 该渠道的策略；null 表示未配置（不自动控制）。 */
  policy: ProbePolicy | null
  models: ProbeModelStat[]
}

export type ProbeOverview = {
  settings: ProbeSettings
  channels: ProbeChannelStat[]
}

export type ProbeOverviewResponse = {
  success: boolean
  message?: string
  data?: ProbeOverview
}

export async function getProbeOverview(): Promise<ProbeOverviewResponse> {
  const res = await api.get('/api/channel/probe/overview', {
    skipErrorHandler: true,
    skipBusinessError: true,
  } as ApiRequestConfig)
  return res.data
}

export type UpdateProbeSettingsRequest = ProbeSettings

export async function updateProbeSettings(
  settings: UpdateProbeSettingsRequest
): Promise<{ success: boolean; message?: string }> {
  const res = await api.put('/api/channel/probe/settings', settings, {
    skipErrorHandler: true,
    skipBusinessError: true,
  } as ApiRequestConfig)
  return res.data
}

/** 触发一轮探测；channelIds 为空表示全部启用渠道。 */
export async function runProbeNow(
  channelIds: number[] = []
): Promise<{ success: boolean; message?: string }> {
  const res = await api.post(
    '/api/channel/probe/run',
    { channel_ids: channelIds },
    { skipErrorHandler: true, skipBusinessError: true } as ApiRequestConfig
  )
  return res.data
}

/** 发送 webhook 测试通知；url/secret 留空时使用已保存的配置。 */
export async function testProbeWebhook(
  webhookUrl = '',
  webhookSecret = ''
): Promise<{ success: boolean; message?: string }> {
  const res = await api.post(
    '/api/channel/probe/webhook_test',
    { webhook_url: webhookUrl, webhook_secret: webhookSecret },
    { skipErrorHandler: true, skipBusinessError: true } as ApiRequestConfig
  )
  return res.data
}

/** 保存渠道探测策略；clear=true 时删除策略。 */
export async function updateProbePolicy(
  channelId: number,
  policy: ProbePolicy | null
): Promise<{ success: boolean; message?: string }> {
  const body = policy
    ? {
        channel_id: channelId,
        auto_control_enabled: policy.auto_control_enabled,
        disable_below: policy.disable_below,
        enable_above: policy.enable_above,
      }
    : { channel_id: channelId, clear: true }
  const res = await api.put('/api/channel/probe/policy', body, {
    skipErrorHandler: true,
    skipBusinessError: true,
  } as ApiRequestConfig)
  return res.data
}

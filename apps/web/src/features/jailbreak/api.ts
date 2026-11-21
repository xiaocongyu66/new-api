import { api } from '@/lib/api'

import type {
  JailbreakPreviewInput,
  JailbreakPreviewResult,
  JailbreakProfile,
  JailbreakProfileInput,
} from './types'

type RawJailbreakProfile = Omit<JailbreakProfile, 'channel_models'> & {
  channel_models?: string
}

function parseChannelModels(value?: string): Record<string, string[]> {
  if (!value) return {}
  try {
    const parsed = JSON.parse(value) as Record<string, string[]>
    return parsed && typeof parsed === 'object' ? parsed : {}
  } catch {
    return {}
  }
}

function fromRawProfile(profile: RawJailbreakProfile): JailbreakProfile {
  return {
    ...profile,
    channel_models: parseChannelModels(profile.channel_models),
  }
}

function toWirePayload(payload: JailbreakProfileInput) {
  return {
    ...payload,
    channel_models: JSON.stringify(payload.channel_models ?? {}),
  }
}

export async function getJailbreakProfiles(): Promise<JailbreakProfile[]> {
  const res = await api.get('/api/jailbreak/')
  return ((res.data?.data ?? []) as RawJailbreakProfile[]).map(fromRawProfile)
}

export async function createJailbreakProfile(
  payload: JailbreakProfileInput
): Promise<JailbreakProfile> {
  const res = await api.post('/api/jailbreak/', toWirePayload(payload))
  return fromRawProfile(res.data?.data as RawJailbreakProfile)
}

export async function updateJailbreakProfile(
  id: number,
  payload: JailbreakProfileInput
): Promise<JailbreakProfile> {
  const res = await api.put(`/api/jailbreak/${id}`, toWirePayload(payload))
  return fromRawProfile(res.data?.data as RawJailbreakProfile)
}

export async function deleteJailbreakProfile(id: number): Promise<void> {
  await api.delete(`/api/jailbreak/${id}`)
}
export async function previewJailbreakPrompt(
  payload: JailbreakPreviewInput
): Promise<JailbreakPreviewResult> {
  const res = await api.post('/api/jailbreak/preview', payload, {
    skipBusinessError: true,
    skipErrorHandler: true,
  })
  const body = res?.data as
    | { success?: boolean; message?: string; data?: JailbreakPreviewResult }
    | undefined
  if (!body?.success || !body?.data) {
    throw new Error(body?.message || '预览接口未返回数据')
  }
  return body.data
}

export interface JailbreakTestResult {
  content: string
  reasoning: string
  finish_reason: string
  prompt_tokens: number
  completion_tokens: number
  response: string
}

export async function testJailbreakRequest(payload: {
  channel_id: number
  model: string
  base_prompt?: string
  base_source?: 'channel' | 'token'
  user_message: string
  extra_kind?: string
  extra_mode?: string
  extra_prompt?: string
  extra_force?: boolean
  extra_name?: string
  exclude_rule_id?: number
}): Promise<JailbreakTestResult> {
  const res = await api.post('/api/jailbreak/test', payload, {
    skipBusinessError: true,
    skipErrorHandler: true,
  })
  if (!res.data?.success || !res.data?.data) {
    throw new Error(res.data?.message || '测试请求失败')
  }
  return res.data.data as JailbreakTestResult
}

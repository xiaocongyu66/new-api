export type JailbreakMode = 'prepend' | 'append' | 'replace'

export type JailbreakKind = 'prompt' | 'header'

export interface JailbreakProfile {
  id: number
  name: string
  description: string
  kind: JailbreakKind
  prompt: string
  headers: string
  channel_models: Record<string, string[]>
  mode: JailbreakMode
  priority: number
  enabled: boolean
  channels: number[]
  models: string[]
  created_time: number
  updated_time: number
}

export interface JailbreakProfileInput {
  name: string
  description: string
  kind: JailbreakKind
  prompt: string
  headers: string
  channel_models: Record<string, string[]>
  mode: JailbreakMode
  priority?: number
  enabled: boolean
  channels: number[]
  models: string[]
}

export const JAILBREAK_MODES: JailbreakMode[] = ['prepend', 'append', 'replace']
export interface JailbreakPreviewInput {
  kind: JailbreakKind
  extra_kind?: JailbreakKind
  extra_mode?: JailbreakMode
  extra_prompt?: string
  extra_headers?: string
  extra_name?: string
  channel_id: number
  model: string
  base_prompt: string
  base_source: 'channel' | 'token'
  base_override: boolean
  user_message: string
}

export interface JailbreakPreviewResult {
  kind: JailbreakKind
  finish_reason?: string
  completion_tokens?: number
  rules: string[] | null
  prompt: string
  headers: Record<string, unknown> | null
  override: boolean
  source: string
  prompt_tokens: number
  messages: { role: string; content: string }[]
}

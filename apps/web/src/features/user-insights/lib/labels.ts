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

import type { TFunction } from 'i18next'
import type {
  InsightCategory,
  InsightEvidenceKind,
  InsightGender,
  InsightRisk,
  InsightStack,
} from '../types'

/** 用途类别的本地化名称。 */
export function categoryLabel(
  category: InsightCategory,
  t: TFunction
): string {
  switch (category) {
    case 'code':
      return t('Coding')
    case 'roleplay':
      return t('Roleplay')
    case 'qa':
      return t('Q&A')
    case 'translate':
      return t('Translation')
    case 'embedding':
      return t('Embedding')
    default:
      return t('Other')
  }
}

/** 代码方向的本地化名称。 */
export function stackLabel(stack: InsightStack, t: TFunction): string {
  switch (stack) {
    case 'frontend':
      return t('Frontend')
    case 'backend':
      return t('Backend')
    case 'fullstack':
      return t('Full Stack')
    case 'infra':
      return t('Infrastructure')
    case 'mobile':
      return t('Mobile')
    case 'data':
      return t('Data & ML')
    default:
      return t('Unknown')
  }
}

/** 风险等级的本地化名称。 */
export function riskLabel(risk: InsightRisk, t: TFunction): string {
  switch (risk) {
    case 'confirmed':
      return t('Confirmed jailbreak')
    case 'likely':
      return t('Likely jailbreak')
    case 'suspect':
      return t('Suspected jailbreak')
    default:
      return t('Clean')
  }
}

/** 性别倾向的本地化名称。 */
export function genderLabel(gender: InsightGender, t: TFunction): string {
  switch (gender) {
    case 'male':
      return t('Likely male')
    case 'female':
      return t('Likely female')
    default:
      return t('Unknown')
  }
}

/**
 * 性别推断依据的本地化名称。
 *
 * 这不是可有可无的说明——它是判断该结论可信度的关键：
 * self_report 是用户自述（可信），preference 是题材偏好推断（中等），
 * inverse 是仅按 AI 角色性别反推的群体先验（很弱，别拿它做处置依据）。
 */
export function genderBasisLabel(
  basis: string | undefined,
  t: TFunction
): string {
  switch (basis) {
    case 'self_report':
      return t('self-reported')
    case 'preference':
      return t('inferred from content preference')
    case 'inverse':
      return t('inferred from character gender')
    default:
      return ''
  }
}

/** 破甲手法标签的本地化名称。 */
export function jailbreakTagLabel(tag: string, t: TFunction): string {
  const labels: Record<string, string> = {
    instruction_override: t('Instruction override'),
    persona_hijack: t('Persona hijack'),
    restriction_removal: t('Restriction removal'),
    refusal_suppression: t('Refusal suppression'),
    prefill_attack: t('Prefill attack'),
    fiction_wrapper: t('Fiction wrapper'),
    encoding_obfuscation: t('Encoding obfuscation'),
    token_smuggling: t('Token smuggling'),
    nsfw_unlock: t('NSFW unlock'),
    system_prompt_extraction: t('System prompt extraction'),
    authority_spoof: t('Authority spoofing'),
    multi_turn_priming: t('Multi-turn priming'),
    known_preset: t('Known jailbreak preset'),
    rule_stacking: t('Rule stacking'),
    hidden_characters: t('Hidden characters'),
  }
  return labels[tag] ?? tag
}

/** 风险等级对应的徽标配色。 */
export function riskBadgeVariant(
  risk: InsightRisk
): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (risk) {
    case 'confirmed':
      return 'destructive'
    case 'likely':
      return 'destructive'
    case 'suspect':
      return 'secondary'
    default:
      return 'outline'
  }
}

/** 把 0-1 的比例格式化为百分比文本。 */
export function formatRatio(ratio: number): string {
  if (!Number.isFinite(ratio) || ratio <= 0) return '0%'
  return `${Math.round(ratio * 100)}%`
}

/** 秒级时间戳格式化为本地时间；0 表示从未发生。 */
export function formatTimestamp(seconds: number, t: TFunction): string {
  if (!seconds) return t('Never')
  return new Date(seconds * 1000).toLocaleString()
}

/** 客户端标识的展示名。未知标识直接原样显示，便于发现新工具。 */
export function clientLabel(client: string): string {
  const names: Record<string, string> = {
    claude_code: 'Claude Code',
    codex_cli: 'Codex CLI',
    opencode: 'OpenCode',
    zcode: 'ZCode',
    gemini_cli: 'Gemini CLI',
    cline: 'Cline',
    roo_code: 'Roo Code',
    kilo_code: 'Kilo Code',
    cursor: 'Cursor',
    windsurf: 'Windsurf',
    copilot: 'GitHub Copilot',
    continue: 'Continue',
    aider: 'Aider',
    crush: 'Crush',
    droid: 'Factory Droid',
    sillytavern: 'SillyTavern',
    cherry_studio: 'Cherry Studio',
    chatbox: 'Chatbox',
    lobechat: 'LobeChat',
    openwebui: 'Open WebUI',
    nextchat: 'NextChat',
    immersive_translate: 'Immersive Translate',
    openai_sdk: 'OpenAI SDK',
    anthropic_sdk: 'Anthropic SDK',
    langchain: 'LangChain',
    generic_http: 'HTTP Client',
  }
  return names[client] ?? client
}

/** 证据类别的本地化名称。 */
export function evidenceKindLabel(
  kind: InsightEvidenceKind,
  t: TFunction
): string {
  switch (kind) {
    case 'jailbreak':
      return t('Jailbreak signals')
    case 'code':
      return t('Coding signals')
    case 'roleplay':
      return t('Roleplay signals')
    case 'qa':
      return t('Q&A signals')
    case 'translate':
      return t('Translation signals')
    case 'stack':
      return t('Stack signals')
    case 'gender':
      return t('Gender signals')
    case 'client':
      return t('Client fingerprint')
    default:
      return kind
  }
}

/**
 * 证据细分标签的本地化名称。
 *
 * 破甲手法标签复用 jailbreakTagLabel；技术栈与性别标签在这里补充。
 * 未知标签原样显示，便于发现后端新增但前端未跟进的标签。
 */
export function evidenceTagLabel(tag: string, t: TFunction): string {
  const extra: Record<string, string> = {
    frontend: t('Frontend'),
    backend: t('Backend'),
    infra: t('Infrastructure'),
    mobile: t('Mobile'),
    data: t('Data & ML'),
    ai_female: t('AI plays female'),
    ai_male: t('AI plays male'),
    user_female: t('User plays female'),
    user_male: t('User plays male'),
  }
  return extra[tag] ?? jailbreakTagLabel(tag, t)
}

/** 字节数格式化为可读体积。 */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unitIndex = 0
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024
    unitIndex += 1
  }
  // 小于 10 时保留一位小数，读数更有意义。
  const digits = value < 10 && unitIndex > 0 ? 1 : 0
  return `${value.toFixed(digits)} ${units[unitIndex]}`
}
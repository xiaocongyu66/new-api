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

import { getAmountSymbol } from '@/lib/currency'
import { getSporeName, getSporeSymbol, sporeUnitsToValue } from '@/lib/spore'


import dayjs from '@/lib/dayjs'

import type { SubscriptionPlan } from '../types'

export function formatDuration(
  plan: Partial<SubscriptionPlan>,
  t: TFunction
): string {
  const unit = plan?.duration_unit || 'month'
  const value = plan?.duration_value || 1
  const unitLabels: Record<string, string> = {
    year: t('years'),
    month: t('months'),
    day: t('days'),
    hour: t('hours'),
    custom: t('Custom (seconds)'),
  }
  if (unit === 'custom') {
    const seconds = plan?.custom_seconds || 0
    if (seconds >= 86400) return `${Math.floor(seconds / 86400)} ${t('days')}`
    if (seconds >= 3600) return `${Math.floor(seconds / 3600)} ${t('hours')}`
    return `${seconds} ${t('seconds')}`
  }
  return `${value} ${unitLabels[unit] || unit}`
}

export function formatResetPeriod(
  plan: Partial<SubscriptionPlan>,
  t: TFunction
): string {
  const period = plan?.quota_reset_period || 'never'
  if (period === 'daily') return t('Daily')
  if (period === 'weekly') return t('Weekly')
  if (period === 'monthly') return t('Monthly')
  if (period === 'custom') {
    const seconds = Number(plan?.quota_reset_custom_seconds || 0)
    if (seconds >= 86400) return `${Math.floor(seconds / 86400)} ${t('days')}`
    if (seconds >= 3600) return `${Math.floor(seconds / 3600)} ${t('hours')}`
    if (seconds >= 60) return `${Math.floor(seconds / 60)} ${t('minutes')}`
    return `${seconds} ${t('seconds')}`
  }
  return t('No Reset')
}

export function formatTimestamp(ts: number): string {
  if (!ts) return '-'
  return dayjs(ts * 1000).format('YYYY-MM-DD HH:mm:ss')
}

/**
 * 套餐标价展示：按支付方式组合金额（充值货币）与菌种（凭证货币）两个单位。
 *
 * - balance 套餐 → 应付金额（$ / ¥，由管理员的金额单位设置决定前缀）
 * - spore 套餐 → 菌种标价（十分之一整数换算 + 菌种名称）
 * - both/either → 两个单位并列（either 用「或」连接）
 * - 未配置支付方式的历史行回落到金额单位展示，保持旧行为。
 */
export function formatPlanPrice(
  plan: Partial<SubscriptionPlan>,
  t: TFunction
): string {
  const mode =
    plan.pay_mode ?? (plan.allow_balance_pay === false ? 'none' : 'balance')
  const money = `${getAmountSymbol()}${Number(plan.price_amount || 0).toFixed(2)}`
  const sporeSymbol = getSporeSymbol()
  const sporeUnit = sporeSymbol || getSporeName()
  // 与余额（🧀 0）一致：单位图标在前，数值在后。
  const spore = `${sporeUnit} ${sporeUnitsToValue(plan.spore_amount ?? 0)}`

  if (mode === 'spore') return spore
  if (mode === 'both') return `${money} + ${spore}`
  if (mode === 'either') return `${money} ${t('or')} ${spore}`
  return money
}

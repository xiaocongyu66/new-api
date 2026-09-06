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

/**
 * 菌种（spore）——由管理员发放的特殊货币，可在套餐页消费。
 *
 * 后端以整数存储，1 单位 = 0.1 菌种（见 internal/identity/store_user_spore.go）。
 * 前端沿用同一约定：所有 API 传输的都是整数单位，只在展示与输入
 * 的边界上做转换。
 */

/** 1 菌种对应的内部整数单位数。最小可操作单位是 0.1 菌种。 */
export const SPORE_UNITS_PER_SPORE = 10

/** 菌种的展示名，集中在此以便将来改名时只动一处。 */
export const SPORE_LABEL = '菌种'

/**
 * 把内部整数单位格式化为展示文本，固定一位小数。
 */
export function formatSpore(units: number | undefined | null): string {
  const value = Number(units ?? 0)
  if (!Number.isFinite(value)) return '0.0'
  return (value / SPORE_UNITS_PER_SPORE).toFixed(1)
}

/**
 * 把用户输入的菌种数量（如 "2.5"）转换为内部整数单位。
 */
export function parseSporeToUnits(input: string | number): number {
  const value = typeof input === 'number' ? input : parseFloat(input)
  if (!Number.isFinite(value)) return 0
  return Math.round(value * SPORE_UNITS_PER_SPORE)
}

/** 内部单位转为菌种数值，供输入框回填。 */
export function sporeUnitsToValue(units: number | undefined | null): number {
  const value = Number(units ?? 0)
  if (!Number.isFinite(value)) return 0
  return value / SPORE_UNITS_PER_SPORE
}

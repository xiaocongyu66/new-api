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
import { useCountUp } from './use-count-up'

// Formatted price / ratio / percentage strings: a non-numeric prefix such as
// "$" or "×", the numeric core (commas allowed), then any suffix.
const NUMERIC_TEXT = /^([^\d-]*)(-?[\d,]*\.?\d+)(.*)$/

/**
 * Counts the numeric core of an already-formatted string up to its final
 * value (e.g. "$75", "×0.5", "37.5%"), keeping the prefix and suffix in
 * place. Non-numeric strings ("-") and reduced-motion users get the final
 * text immediately.
 *
 * The animation is delegated to `useCountUp`, so retargeting starts from the
 * value currently on screen. `active` gates cards that are still below the
 * fold: while gated the text shows its zero state (same prefix/suffix), so
 * the count-up begins as soon as the card becomes visible.
 */
export function useCountUpText(
  finalText: string,
  active = true,
  durationMs = 1500
): string {
  const match = NUMERIC_TEXT.exec(finalText)
  const target = match ? Number(match[2].replaceAll(',', '')) : Number.NaN
  const enabled = active && Number.isFinite(target)
  const value = useCountUp(Number.isFinite(target) ? target : 0, durationMs, enabled)

  if (!match || !Number.isFinite(target)) {
    return finalText
  }

  const [, prefix, numberText, suffix] = match
  const decimals = numberText.includes('.')
    ? numberText.split('.')[1].length
    : 0
  if (Math.abs(value - target) < 1e-9) {
    // Return the caller's exact string once settled: its separators and
    // trailing zeros come from the app formatter, not from this hook.
    return finalText
  }
  return (
    prefix +
    value.toLocaleString('en-US', {
      minimumFractionDigits: decimals,
      maximumFractionDigits: decimals,
    }) +
    suffix
  )
}

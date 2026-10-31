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
import { useEffect, useRef, useState } from 'react'

/**
 * Animates a number up to `target` with an ease-out curve when the value
 * first appears (the "count-up" effect). Reduced-motion users and
 * non-finite targets get the final value immediately.
 *
 * When the target CHANGES while the number is already shown (hovering a
 * different chart hour, re-sorted lists), the animation starts from whatever
 * is on screen instead of collapsing back to zero, so a retarget reads as a
 * count to the new value rather than a reset.
 *
 * The settle ref is only written when an animation finishes, never when it
 * starts: React StrictMode double-invokes mount effects, and a start-time
 * ref would make the second invocation skip the animation and leave the
 * display stuck at zero for data that was already cached (e.g. paging a
 * list whose query is warm).
 */
export function useCountUp(
  target: number,
  durationMs = 1500,
  enabled = true
): number {
  const [display, setDisplay] = useState(0)
  // The value currently painted, mirrored for the effect closure: the next
  // animation must start from what the user sees, not from a stale state
  // snapshot captured at render time.
  const displayRef = useRef(0)
  const frameRef = useRef<number | null>(null)
  const settledRef = useRef<number>(Number.NaN)
  const wasEnabledRef = useRef(enabled)

  useEffect(() => {
    const wasEnabled = wasEnabledRef.current
    wasEnabledRef.current = enabled

    // Gated values (cards still below the fold) hold at zero instead of
    // painting the final number first: when the card scrolls in, the count-up
    // starts from the value already on screen, so there is no one-frame jump
    // from the finished number back to zero.
    if (!enabled) {
      // Off-screen/gated cards: don't register rAF or run the animation body.
      // Only reset painted state when transitioning away from enabled.
      if (wasEnabled) {
        settledRef.current = Number.NaN
        displayRef.current = 0
        setDisplay(0)
      }
      return
    }
    if (!Number.isFinite(target)) {
      setDisplay(0)
      displayRef.current = 0
      return
    }
    if (wasEnabled && target === settledRef.current) {
      return
    }

    const reducedMotion =
      typeof window !== 'undefined' &&
      window.matchMedia('(prefers-reduced-motion: reduce)').matches
    if (reducedMotion || durationMs <= 0) {
      settledRef.current = target
      displayRef.current = target
      setDisplay(target)
      return
    }

    const from = wasEnabled ? displayRef.current : 0
    const startedAt = performance.now()
    const tick = (now: number) => {
      // rAF timestamps can predate the effect that scheduled them; clamp so
      // the eased value never overshoots on the first frame.
      const progress = Math.max(0, Math.min(1, (now - startedAt) / durationMs))
      const eased = 1 - Math.pow(1 - progress, 3)
      const value = from + (target - from) * eased
      displayRef.current = value
      setDisplay(value)
      if (progress < 1) {
        frameRef.current = requestAnimationFrame(tick)
      } else {
        settledRef.current = target
      }
    }
    frameRef.current = requestAnimationFrame(tick)

    return () => {
      if (frameRef.current !== null) {
        cancelAnimationFrame(frameRef.current)
      }
    }
  }, [target, durationMs, enabled])

  return display
}

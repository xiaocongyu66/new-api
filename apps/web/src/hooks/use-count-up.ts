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
 * Animates a number from 0 to `target` with an ease-out curve when the value
 * first appears (the "count-up" effect). Reduced-motion users and
 * non-finite targets get the final value immediately.
 *
 * The settle ref is only written when an animation finishes, never when it
 * starts: React StrictMode double-invokes mount effects, and a start-time
 * ref would make the second invocation skip the animation and leave the
 * display stuck at zero for data that was already cached (e.g. paging a
 * list whose query is warm).
 */
export function useCountUp(target: number, durationMs = 900): number {
  const [display, setDisplay] = useState(0)
  const frameRef = useRef<number | null>(null)
  const settledRef = useRef<number>(Number.NaN)

  useEffect(() => {
    if (!Number.isFinite(target)) {
      setDisplay(0)
      return
    }
    if (target === settledRef.current) {
      return
    }

    const reducedMotion =
      typeof window !== 'undefined' &&
      window.matchMedia('(prefers-reduced-motion: reduce)').matches
    if (reducedMotion || durationMs <= 0) {
      settledRef.current = target
      setDisplay(target)
      return
    }

    const startedAt = performance.now()
    const tick = (now: number) => {
      // rAF timestamps can predate the effect that scheduled them; clamp so
      // the eased value never dips below zero on the first frame.
      const progress = Math.max(0, Math.min(1, (now - startedAt) / durationMs))
      const eased = 1 - Math.pow(1 - progress, 3)
      setDisplay(target * eased)
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
  }, [target, durationMs])

  return display
}

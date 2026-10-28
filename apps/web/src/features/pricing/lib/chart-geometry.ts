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

// Chart geometry in viewBox units. Kept outside the component module so the
// spline builder and constants can be unit-tested without pulling React in
// (and without breaking the component file's fast-refresh contract).

export const CHART_W = 400
export const CHART_H = 96
export const PAD_L = 40
export const PAD_R = 38
export const PAD_T = 8
export const PAD_B = 14
export const PLOT_W = CHART_W - PAD_L - PAD_R
export const PLOT_H = CHART_H - PAD_T - PAD_B
export const BASE_Y = CHART_H - PAD_B

export type ChartPoint = { x: number; y: number }

/**
 * Cardinal-spline polyline → cubic bezier path. Noisy hourly data would
 * make straight segments look jagged at this size; control points are
 * clamped into the plot band so the curve cannot overshoot the axes.
 */
export function smoothLinePath(points: ChartPoint[]): string {
  if (points.length === 0) return ''
  if (points.length < 3) {
    return points
      .map((point, index) => `${index === 0 ? 'M' : 'L'}${point.x},${point.y}`)
      .join(' ')
  }
  const clampY = (y: number) => Math.min(BASE_Y, Math.max(PAD_T, y))
  const tension = 0.18
  let path = `M${points[0].x.toFixed(1)},${points[0].y.toFixed(1)}`
  for (let index = 0; index < points.length - 1; index++) {
    const previous = points[index - 1] ?? points[index]
    const current = points[index]
    const next = points[index + 1]
    const after = points[index + 2] ?? next
    const firstControl = {
      x: current.x + (next.x - previous.x) * tension,
      y: clampY(current.y + (next.y - previous.y) * tension),
    }
    const secondControl = {
      x: next.x - (after.x - current.x) * tension,
      y: clampY(next.y - (after.y - current.y) * tension),
    }
    path += ` C${firstControl.x.toFixed(1)},${firstControl.y.toFixed(1)} ${secondControl.x.toFixed(1)},${secondControl.y.toFixed(1)} ${next.x.toFixed(1)},${next.y.toFixed(1)}`
  }
  return path
}

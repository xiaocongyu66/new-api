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
import { describe, test } from 'bun:test'
import assert from 'node:assert/strict'

import { smoothLinePath } from '../model-card-metrics'

// The chart band the spline clamps into (mirrors the component's constants:
// CHART_H 96, PAD_T 8, PAD_B 14).
const BAND_TOP = 8
const BAND_BOTTOM = 82

function allYCoordinates(path: string): number[] {
  return [...path.matchAll(/[MLC]([^MLCZ]+)/g)].flatMap(([, segment]) =>
    segment
      .trim()
      .split(/[\s,]+/)
      .filter((token) => token.length > 0)
      .map(Number)
      .filter((_, index) => index % 2 === 1)
  )
}

describe('smoothLinePath', () => {
  test('degenerate inputs produce no curve', () => {
    assert.equal(smoothLinePath([]), '')
    assert.equal(
      smoothLinePath([{ x: 10, y: 20 }]),
      'M10,20',
      'a single point is a moveto, not a curve'
    )
  })

  test('two points stay a straight segment', () => {
    assert.equal(
      smoothLinePath([
        { x: 0, y: 80 },
        { x: 100, y: 10 },
      ]),
      'M0,80 L100,10'
    )
  })

  test('three or more points emit one bezier per segment', () => {
    const points = [
      { x: 0, y: 80 },
      { x: 50, y: 40 },
      { x: 100, y: 20 },
      { x: 150, y: 60 },
    ]
    const path = smoothLinePath(points)
    assert.ok(path.startsWith('M0.0,80.0'))
    assert.equal((path.match(/C/g) ?? []).length, points.length - 1)
    assert.ok(path.endsWith('150.0,60.0'))
  })

  // Control points derived from neighbours can overshoot the axes on noisy
  // data; the clamp keeps every coordinate inside the plot band.
  test('control points stay inside the plot band on noisy data', () => {
    const points = [
      { x: 0, y: 80 },
      { x: 20, y: 8 },
      { x: 40, y: 82 },
      { x: 60, y: 8 },
      { x: 80, y: 82 },
      { x: 100, y: 10 },
    ]
    for (const y of allYCoordinates(smoothLinePath(points))) {
      assert.ok(
        y >= BAND_TOP && y <= BAND_BOTTOM,
        `y ${y} escaped the plot band`
      )
    }
  })
})

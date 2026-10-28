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
import { memo, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { getSuccessRateColor } from '@/features/performance-metrics/lib/format'
import { useCountUp } from '@/hooks/use-count-up'
import { cn } from '@/lib/utils'

import type { ModelPerfBadgeData } from '../types'

// Fixed viewBox with uniform scaling (preserveAspectRatio defaults to meet):
// axis text must not stretch, and card widths only vary by ~10%.
const CHART_W = 400
const CHART_H = 96
const PAD_L = 40
const PAD_R = 38
const PAD_T = 8
const PAD_B = 14
const PLOT_W = CHART_W - PAD_L - PAD_R
const PLOT_H = CHART_H - PAD_T - PAD_B
const BASE_Y = CHART_H - PAD_B

const LATENCY_COLOR = '#3b82f6' // blue-500, latency bars
const GRID_COLOR = 'currentColor'

type TrendPoint = { ts: number; latency_ms: number; success_rate: number }

function formatHour(ts: number): string {
  const date = new Date(ts * 1000)
  return `${String(date.getHours()).padStart(2, '0')}:00`
}

function formatLatency(ms: number): string {
  return `${Math.round(ms).toLocaleString()}ms`
}

type Point = { x: number; y: number }

/**
 * Cardinal-spline polyline → cubic bezier path. Noisy hourly data would
 * make straight segments look jagged at this size; control points are
 * clamped into the plot band so the curve cannot overshoot the axes.
 */
function smoothLinePath(points: Point[]): string {
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

// Mantissa steps for axis rounding, in ascending order.
const NICE_STEPS = [1, 2, 2.5, 5, 10]

/** Round a raw maximum up to the next 1/2/2.5/5/10 × 10^n so axis ticks read cleanly. */
function niceCeil(value: number): number {
  if (value <= 0) return 1
  const magnitude = Math.pow(10, Math.floor(Math.log10(value)))
  const normalized = value / magnitude
  const step = NICE_STEPS.find((candidate) => normalized <= candidate) ?? 10
  return step * magnitude
}
/**
 * Mini combo chart for a model-square card: blue latency bars against the
 * left axis (ms), green success-rate line against the right axis (%), hour
 * labels on the x axis. The headline numbers sit above the chart and swap to
 * the hovered/clicked hour; clicking pins that hour.
 *
 * Renders nothing without metrics so unsampled models stay visually clean.
 */
export const ModelCardMetrics = memo(function ModelCardMetrics(props: {
  perf: ModelPerfBadgeData | undefined
  className?: string
}) {
  const { t } = useTranslation()
  const { perf } = props
  const [hovered, setHovered] = useState<number | null>(null)
  const [pinned, setPinned] = useState<number | null>(null)

  const series = useMemo<TrendPoint[]>(
    () =>
      (perf?.recent_series ?? [])
        .filter(
          (point) =>
            Number.isFinite(point.latency_ms) &&
            Number.isFinite(point.success_rate)
        )
        .slice(-24),
    [perf]
  )

  const geometry = useMemo(() => {
    if (series.length < 2) return null

    const latencies = series.map((point) => point.latency_ms)
    const latencyMax = niceCeil(Math.max(...latencies))
    const latencyTicks = [0, latencyMax / 2, latencyMax]

    const successValues = series.map((point) => point.success_rate)
    let successMin = Math.max(0, Math.floor((Math.min(...successValues) - 1) / 5) * 5)
    let successMax = Math.min(100, Math.ceil((Math.max(...successValues) + 1) / 5) * 5)
    if (successMax - successMin < 10) {
      successMax = Math.min(100, successMin + 10)
      successMin = Math.max(0, successMax - 10)
    }
    const successTicks = [successMin, (successMin + successMax) / 2, successMax]

    const xAt = (index: number) =>
      PAD_L + (PLOT_W * index) / (series.length - 1)
    const yLatency = (value: number) =>
      BASE_Y - (value / latencyMax) * PLOT_H
    const ySuccess = (value: number) =>
      BASE_Y - ((value - successMin) / (successMax - successMin)) * PLOT_H

    const band = PLOT_W / series.length
    // 24 hourly points leave ~14 units per band; keep bars readable with a
    // floor instead of letting them vanish.
    const barWidth = Math.max(4, Math.min(16, band * 0.6))
    const bars = series.map((point, index) => ({
      index,
      x: xAt(index) - barWidth / 2,
      y: yLatency(point.latency_ms),
      width: barWidth,
      height: Math.max(1, BASE_Y - yLatency(point.latency_ms)),
    }))

    const linePath = smoothLinePath(
      series.map((point, index) => ({
        x: xAt(index),
        y: ySuccess(point.success_rate),
      }))
    )

    // Label at most four hours; always keep the newest one when it clears the
    // previous label by a full step.
    const labelEvery = Math.max(1, Math.ceil(series.length / 4))
    const xLabels = series
      .map((point, index) => ({ index, label: formatHour(point.ts) }))
      .filter(
        (item, _, all) =>
          item.index % labelEvery === 0 ||
          (item.index === all.length - 1 &&
            item.index % labelEvery >= labelEvery - 1)
      )

    return {
      bars,
      linePath,
      xAt,
      ySuccess,
      latencyTicks,
      successTicks,
      xLabels,
      band,
    }
  }, [series])

  const latency = useCountUp(perf?.avg_latency_ms ?? 0)
  const successRate = useCountUp(perf?.success_rate ?? 0)
  const throughput = useCountUp(perf?.avg_tps ?? 0)

  if (!perf || !geometry) {
    return null
  }

  const active = pinned ?? hovered
  const activePoint = active != null ? series[active] : null
  const successColor = getSuccessRateColor(perf.success_rate)

  return (
    <div className={cn('border-t pt-3', props.className)}>
      {/* Headline numbers: window averages, or the hovered/clicked hour */}
      <div className='flex items-end justify-between gap-2'>
        <div className='flex min-w-0 items-baseline gap-1.5'>
          <span className='text-muted-foreground/55 text-[10px] leading-4'>
            {t('Latency short')}
          </span>
          <span className='text-foreground font-mono text-base leading-none font-semibold tabular-nums'>
            {activePoint
              ? formatLatency(activePoint.latency_ms)
              : formatLatency(latency)}
          </span>
          {activePoint && (
            <span className='bg-foreground/10 text-foreground/80 rounded px-1 text-[10px] leading-4 tabular-nums'>
              {formatHour(activePoint.ts)}
            </span>
          )}
        </div>
        <div className='flex shrink-0 flex-col items-end gap-0.5 text-[11px] leading-4 tabular-nums'>
          <span className='flex items-baseline gap-1'>
            <span className='text-muted-foreground/55'>{t('Status short')}</span>
            <span
              className='font-mono font-semibold'
              style={{ color: successColor }}
            >
              {activePoint
                ? activePoint.success_rate.toFixed(1)
                : successRate.toFixed(1)}
              %
            </span>
          </span>
          <span className='flex items-baseline gap-1'>
            <span className='text-muted-foreground/55'>
              {t('Throughput short')}
            </span>
            <span className='text-foreground/80 font-mono'>
              {Math.round(throughput).toLocaleString()}t/s
            </span>
          </span>
        </div>
      </div>

      <svg
        viewBox={`0 0 ${CHART_W} ${CHART_H}`}
        className='mt-1.5 h-24 w-full'
        aria-hidden='true'
        onMouseLeave={() => setHovered(null)}
      >
        {/* Horizontal gridlines double as both axes' tick references */}
        {geometry.latencyTicks.map((tick) => {
          const y = BASE_Y - (tick / geometry.latencyTicks[2]) * PLOT_H
          return (
            <line
              key={`grid-${tick}`}
              x1={PAD_L}
              x2={CHART_W - PAD_R}
              y1={y}
              y2={y}
              className={GRID_COLOR}
              strokeOpacity={0.12}
              strokeWidth={1}
              vectorEffect='non-scaling-stroke'
            />
          )
        })}

        {/* Left axis: latency (ms) */}
        {geometry.latencyTicks.map((tick, index) => {
          const y = BASE_Y - (tick / geometry.latencyTicks[2]) * PLOT_H
          return (
            <text
              key={`lat-${tick}`}
              x={PAD_L - 6}
              y={y + 3}
              textAnchor='end'
              className='fill-muted-foreground/55'
              fontSize={9}
            >
              {index === geometry.latencyTicks.length - 1
                ? `${tick.toLocaleString()}ms`
                : Math.round(tick).toLocaleString()}
            </text>
          )
        })}

        {/* Right axis: success rate (%) */}
        {geometry.successTicks.map((tick, index) => {
          const y =
            BASE_Y -
            ((tick - geometry.successTicks[0]) /
              (geometry.successTicks[2] - geometry.successTicks[0])) *
              PLOT_H
          return (
            <text
              key={`succ-${tick}`}
              x={CHART_W - PAD_R + 6}
              y={y + 3}
              textAnchor='start'
              className='fill-muted-foreground/55'
              fontSize={9}
            >
              {index === geometry.successTicks.length - 1 ? `${tick}%` : tick}
            </text>
          )
        })}

        {/* Latency bars; non-active bars dim while a point is active */}
        {geometry.bars.map((bar) => (
          <rect
            key={`bar-${series[bar.index].ts}`}
            className='card-chart-bar'
            x={bar.x}
            y={bar.y}
            width={bar.width}
            height={bar.height}
            rx={1.5}
            fill={LATENCY_COLOR}
            fillOpacity={active == null || active === bar.index ? 0.85 : 0.22}
            style={{
              animationDelay: `${bar.index * 18}ms`,
              transition: 'fill-opacity 140ms ease',
            }}
          />
        ))}

        {/* Success-rate line + points */}
        <path
          className='card-chart-line'
          d={geometry.linePath}
          pathLength={1}
          fill='none'
          stroke={successColor}
          strokeWidth={2}
          strokeLinecap='round'
          strokeLinejoin='round'
          vectorEffect='non-scaling-stroke'
        />
        {series.map((point, index) => (
          <circle
            key={`dot-${point.ts}`}
            className='card-chart-dot'
            cx={geometry.xAt(index)}
            cy={geometry.ySuccess(point.success_rate)}
            r={active === index ? 3.2 : 2}
            fill={successColor}
            stroke='white'
            strokeWidth={1}
            vectorEffect='non-scaling-stroke'
            style={{
              animationDelay: `${420 + index * 12}ms`,
              transition: 'r 140ms ease',
            }}
          />
        ))}

        {/* Active-hour guide */}
        {active != null && (
          <line
            x1={geometry.xAt(active)}
            x2={geometry.xAt(active)}
            y1={PAD_T}
            y2={BASE_Y}
            stroke='currentColor'
            className='fill-foreground'
            strokeOpacity={0.35}
            strokeWidth={1}
            vectorEffect='non-scaling-stroke'
            style={{ transition: 'stroke-opacity 140ms ease' }}
          />
        )}

        {/* X axis: hour labels */}
        {geometry.xLabels.map((item) => (
          <text
            key={`x-${item.index}`}
            x={geometry.xAt(item.index)}
            y={CHART_H - 3}
            textAnchor='middle'
            className='fill-muted-foreground/55'
            fontSize={9}
          >
            {item.label}
          </text>
        ))}

        {/* Hit areas: hover previews a hour, click pins it. Clicks must not
            reach the card, which opens the details drawer. */}
        {series.map((point, index) => (
          <rect
            key={`hit-${point.ts}`}
            x={geometry.xAt(index) - geometry.band / 2}
            y={0}
            width={geometry.band}
            height={CHART_H}
            fill='transparent'
            style={{ pointerEvents: 'all', cursor: 'pointer' }}
            onMouseEnter={() => setHovered(index)}
            onClick={(event) => {
              event.stopPropagation()
              setPinned((current) => (current === index ? null : index))
            }}
          />
        ))}
      </svg>
    </div>
  )
})

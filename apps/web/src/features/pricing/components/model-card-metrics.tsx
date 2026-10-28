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

import {
  BASE_Y,
  CHART_H,
  CHART_W,
  PAD_L,
  PAD_R,
  PAD_T,
  PLOT_H,
  PLOT_W,
  smoothLinePath,
} from '../lib/chart-geometry'
import type { ModelPerfBadgeData } from '../types'

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

  if (!perf || !geometry) {
    return null
  }

  const active = pinned ?? hovered
  const activePoint = active != null ? series[active] : null
  const successColor = getSuccessRateColor(perf.success_rate)

  return (
    <div className={cn('border-t pt-3', props.className)}>
      {/* Headline numbers: window averages, or the hovered/clicked hour.
          One flat row of three — latency, success rate, then the hour. */}
      <div className='flex items-baseline gap-x-3 gap-y-1 text-[11px] leading-4 tabular-nums'>
        <span className='flex min-w-0 items-baseline gap-1'>
          <span className='text-muted-foreground/55'>{t('Latency short')}</span>
          <span className='text-foreground font-mono text-sm font-semibold'>
            {activePoint
              ? formatLatency(activePoint.latency_ms)
              : formatLatency(latency)}
          </span>
        </span>
        <span className='flex items-baseline gap-1'>
          <span className='text-muted-foreground/55'>{t('Status short')}</span>
          <span className='font-mono font-semibold' style={{ color: successColor }}>
            {activePoint
              ? activePoint.success_rate.toFixed(1)
              : successRate.toFixed(1)}
            %
          </span>
        </span>
        {activePoint && (
          <span className='bg-foreground/10 text-foreground/80 rounded px-1 font-mono'>
            {formatHour(activePoint.ts)}
          </span>
        )}
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

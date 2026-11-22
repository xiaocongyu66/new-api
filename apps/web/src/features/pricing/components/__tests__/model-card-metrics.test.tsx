import { afterAll, describe, expect, test } from 'bun:test'

import type { ModelPerfBadgeData } from '../model-perf-badge'
import type { Root } from 'react-dom/client'

// The card chart is plain SVG, so it renders under happy-dom without the
// rspack CSS/app entry pipeline the dev server needs. Dynamic imports follow
// the repo's existing render-test convention: bun evaluates all test files in
// one process, so React/i18next singletons must load after the happy-dom
// environment preload (see bunfig.toml).
const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { initReactI18next, I18nextProvider } = await import('react-i18next')
const { ModelCardMetrics } = await import('../model-card-metrics')

// An empty resource bundle makes i18next echo the key, so assertions read the
// English source string the component passes to `t()`.
const i18n = createInstance()
await i18n.use(initReactI18next).init({ lng: 'en' })

const mounted: { root: Root | null } = { root: null }

afterAll(() => {
  act(() => mounted.root?.unmount())
})

function renderCard(perf: ModelPerfBadgeData) {
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  act(() => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <ModelCardMetrics perf={perf} />
      </I18nextProvider>
    )
  })
  mounted.root = root
  return host
}

// A seconds-suffixed axis tick ("2.50s") versus a milliseconds one ("500ms"):
// "ms" also ends in "s", so the check needs the regex, not endsWith.
const SECONDS_LABEL = /(?<!m)s$/

// A series of hourly latency points; latency_ms is the bucket mean the backend
// reports in milliseconds.
function series(latencyMs: number) {
  return Array.from({ length: 24 }, (_, index) => ({
    ts: 1_700_000_000 + index * 3_600,
    latency_ms: latencyMs,
    success_rate: 99,
  }))
}

function badge(recent: ReturnType<typeof series>, avgLatencyMs: number) {
  return {
    avg_latency_ms: avgLatencyMs,
    success_rate: 99,
    avg_tps: 42,
    recent_series: recent,
  } satisfies ModelPerfBadgeData
}

function latencyTexts(host: HTMLElement) {
  return Array.from(host.querySelectorAll('text'))
    .map((node) => node.textContent ?? '')
    .filter((text) => text.length > 0)
}

// The success-rate axis is the only one labelled with textAnchor="start"
// (latency anchors "end", hour labels "middle"), so it isolates the right
// axis labels from the rest of the chart.
function rightAxisLabels(host: HTMLElement) {
  return Array.from(host.querySelectorAll('text'))
    .filter((node) => node.getAttribute('text-anchor') === 'start')
    .map((node) => node.textContent ?? '')
}

describe('model-square card latency unit', () => {
  // The whole scale (axis ticks, headline, bars) must share one unit: a chart
  // that mixes "2.50" with "1200ms" is unreadable and the ticks no longer
  // measure the bars.
  test('a peak above one second renders the whole chart in seconds', () => {
    const host = renderCard(badge(series(2_500), 2_500))
    const labels = latencyTexts(host)

    expect(labels).toContain('2.50s')
    expect(labels.some((label) => label.endsWith('ms'))).toBe(false)
  })

  // Real hourly data rarely has every bucket equal to the peak: a single slow
  // hour must flip the whole chart's unit without the average moving. This is
  // the case the uniform-series tests above cannot cover.
  test('a single slow hour sets the unit for the whole chart', () => {
    const slowHour = { ts: 1_700_000_000, latency_ms: 2_500, success_rate: 99 }
    const host = renderCard(
      badge([...series(200), slowHour], 200)
    )
    const labels = latencyTexts(host)

    expect(labels).toContain('2.50s')
    expect(labels.some((label) => label.endsWith('ms'))).toBe(false)
  })

  // A faster model keeps milliseconds so their numbers stay whole rather than
  // collapsing to "0.00"-style fractions of a second. The top tick carries the
  // unit and shows the rounded scale (niceCeil(820) is 1000), never the raw
  // measured value.
  test('a peak below one second keeps milliseconds', () => {
    const host = renderCard(badge(series(820), 820))
    const labels = latencyTexts(host)

    expect(labels).toContain('1,000ms')
    expect(labels.some((label) => SECONDS_LABEL.test(label))).toBe(false)
  })

  // The boundary itself: a measured peak of exactly one second is the smallest
  // value that flips the scale, so 999ms must stay in milliseconds.
  test('a peak just under one second stays in milliseconds', () => {
    const host = renderCard(badge(series(999), 999))
    const labels = latencyTexts(host)

    expect(labels).toContain('1,000ms')
    expect(labels.some((label) => SECONDS_LABEL.test(label))).toBe(false)
  })

  // The headline value and the axis must agree, so a reader can check the
  // average against the scale. The headline reserves the axis max's width and
  // animates the live number, so it settles on the axis shape rather than a
  // standalone one.
  test('the headline number matches the axis unit', async () => {
    const host = renderCard(badge(series(1_200), 1_200))
    // The count-up runs on a real timer; let it reach the final value.
    const { promise, resolve } = Promise.withResolvers<void>()
    setTimeout(resolve, 2_000)
    await promise
    await act(async () => {})

    const headline = Array.from(host.querySelectorAll('span'))
      .map((node) => node.textContent ?? '')
      .filter((text) => /^2\.00$/.test(text))

    expect(headline).toContain('2.00')
    expect(latencyTexts(host)).toContain('2.00s')
  })
})
describe('model-square card success-rate axis', () => {
  // The health axis must stay fixed at 0–100 rather than tracking the data
  // range: a dynamic axis would stretch a healthy 60–100% model across the
  // whole chart, making a 60% hour hug the baseline and read like a failure.
  test('the right axis stays fixed at 0, 50, 100 regardless of the data', () => {
    // A narrow high band: the old dynamic axis would have produced a
    // 60–100 range, but the fixed axis must read 0/50/100.
    const narrow = Array.from({ length: 24 }, (_, index) => ({
      ts: 1_700_000_000 + index * 3_600,
      latency_ms: 200,
      success_rate: 60 + Math.round((40 * index) / 23),
    }))
    const host = renderCard(badge(narrow, 200))

    // The top tick carries the % suffix; the rest are bare numbers.
    expect(rightAxisLabels(host)).toEqual(['0', '50', '100%'])
  })

  // A uniformly high success rate still anchors the baseline at 0 so the
  // reader can gauge the headroom, not collapse onto a single line.
  test('a high success rate still shows the 0 baseline', () => {
    const host = renderCard(badge(series(200), 200))

    expect(rightAxisLabels(host)).toEqual(['0', '50', '100%'])
  })

  // The green dots' cy must follow the fixed mapping y = BASE_Y - (value/100)
  // * PLOT_H, not stretch to the data. With BASE_Y=82 and PLOT_H=74:
  // 0% → 82 (baseline), 50% → 45, 100% → 8 (top). Asserting the rendered dot
  // position locks the line to the axis even if successTicks and ySuccess are
  // ever edited independently.
  test('the success line maps 0/50/100 to the fixed heights', () => {
    const points = [0, 50, 100].map((rate, index) => ({
      ts: 1_700_000_000 + index * 3_600,
      latency_ms: 200,
      success_rate: rate,
    }))
    const host = renderCard(badge(points, 200))
    const cys = Array.from(host.querySelectorAll('svg circle')).map((circle) =>
      Number(circle.getAttribute('cy'))
    )
    expect(cys).toEqual([82, 45, 8])
  })
})

describe('model-square card upstream fallback', () => {
  // A quiet hour must not become a fabricated 100% / 0 ms sample. Weighted
  // upstream health fills it, and hours with neither source stay empty so the
  // line breaks instead of interpolating a made-up rate.
  test('upstream hours are drawn and unsampled hours are gaps', async () => {
    const perf: ModelPerfBadgeData = {
      avg_latency_ms: 400,
      success_rate: 90,
      avg_tps: 42,
      recent_series: [
        { ts: 1_700_000_000, latency_ms: 250, success_rate: 60, source: 'upstream' },
        { ts: 1_700_003_600, latency_ms: null, success_rate: null, source: 'none' },
        { ts: 1_700_007_200, latency_ms: 400, success_rate: 90, source: 'request' },
      ],
    }
    const host = renderCard(perf)

    expect(host.querySelectorAll('svg circle')).toHaveLength(2)
    expect(host.querySelectorAll('rect.card-chart-bar')).toHaveLength(2)
    const path = host.querySelector('path.card-chart-line')?.getAttribute('d') ?? ''
    expect((path.match(/M/g) ?? []).length).toBe(2)

    const chart = host.querySelector<SVGSVGElement>('svg[viewBox="0 0 400 96"]')
    expect(chart).not.toBeNull()
    chart!.getBoundingClientRect = () =>
      ({
        x: 0,
        y: 0,
        left: 0,
        top: 0,
        right: 400,
        bottom: 96,
        width: 400,
        height: 96,
        toJSON: () => ({}),
      }) as DOMRect

    const clickAt = async (clientX: number) => {
      await act(async () => {
        chart!.dispatchEvent(
          new MouseEvent('click', { bubbles: true, clientX, clientY: 48 })
        )
      })
    }

    await clickAt(40)
    expect(host.textContent).not.toContain('Probe')
    expect(host.textContent).not.toContain('Upstream')

    await clickAt(201)
    expect(host.textContent).toContain('—')
  })
})

describe('model-square card upstream rate without latency', () => {
  // The old gap guard required latency and rate together, so an upstream
  // health estimate with no latency sample silently lost its rate. The rate
  // must still be plotted; only the latency bar stays a gap.
  test('an upstream rate is plotted while its latency is a gap', async () => {
    const perf: ModelPerfBadgeData = {
      avg_latency_ms: null,
      success_rate: 80,
      avg_tps: 42,
      source: 'upstream',
      recent_series: [
        { ts: 1_700_000_000, latency_ms: 250, success_rate: 60, source: 'request' },
        { ts: 1_700_003_600, latency_ms: null, success_rate: 85, source: 'upstream' },
        { ts: 1_700_007_200, latency_ms: null, success_rate: null, source: 'none' },
        { ts: 1_700_010_800, latency_ms: 400, success_rate: 90, source: 'request' },
      ],
    }
    const host = renderCard(perf)

    // Three measured rates, but only two latencies: the upstream hour draws a
    // dot and no bar. The path breaks at the empty hour instead of joining
    // 85% to 90% across a sample nobody measured.
    expect(host.querySelectorAll('svg circle')).toHaveLength(3)
    expect(host.querySelectorAll('rect.card-chart-bar')).toHaveLength(2)
    const path = host.querySelector('path.card-chart-line')?.getAttribute('d') ?? ''
    expect((path.match(/M/g) ?? []).length).toBe(2)

    const headline = host.querySelector<HTMLElement>('div.grid-cols-3')!
    // Idle summary: no latency to show — it must read as a gap, not 0 ms.
    expect(headline.textContent).toContain('—')
    expect(headline.textContent).not.toContain('ms')

    const chart = host.querySelector<SVGSVGElement>('svg[viewBox="0 0 400 96"]')!
    chart.getBoundingClientRect = () =>
      ({
        x: 0,
        y: 0,
        left: 0,
        top: 0,
        right: 400,
        bottom: 96,
        width: 400,
        height: 96,
        toJSON: () => ({}),
      }) as DOMRect

    const pinAt = async (clientX: number) => {
      await act(async () => {
        chart.dispatchEvent(
          new MouseEvent('click', { bubbles: true, clientX, clientY: 48 })
        )
      })
    }

    await pinAt(147)
    // Source labels are gone from the headline; only the metrics remain.

    await pinAt(255)
    // A gap shows '—' for both metrics and prints no unit beside either.
    expect(headline.textContent).toContain('—')
    expect(headline.textContent).not.toContain('ms')
    expect(headline.textContent).not.toContain('%')
  })
})

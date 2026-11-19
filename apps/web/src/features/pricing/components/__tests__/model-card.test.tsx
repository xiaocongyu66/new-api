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
import { afterAll, describe, mock, test } from 'bun:test'
import assert from 'node:assert/strict'

import { resetSharedDomWindow } from '@/test-utils/happy-dom-env'

import type { PricingModel } from '../../types'
import type { ModelPerfBadgeData } from '../model-perf-badge'
// 卡片图标走 @lobehub/icons，其模块图在 bun 下会因 @lobehub/ui 缺少 Tag 导出而加载失败；
// 本测试不覆盖图标渲染，换成空实现避开该依赖图。
mock.module('@/lib/lobe-icon', () => ({
  getLobeIcon: () => null,
  LOBE_ICON_KEYS: [],
}))

// happy-dom ships a clipboard object without writeText, and no execCommand, so
// the copy button's real write path needs a boundary fake. The model name the
// fake records is asserted below, which keeps the test on the user-visible
// contract ("copying does not also open details").
const clipboardWrites: string[] = []
const originalClipboard = Object.getOwnPropertyDescriptor(
  globalThis.navigator,
  'clipboard'
)
Object.defineProperty(globalThis.navigator, 'clipboard', {
  configurable: true,
  value: {
    writeText: (text: string) => {
      clipboardWrites.push(text)
      return Promise.resolve()
    },
  },
})

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { ModelCard } = await import('../model-card')

// An empty resource bundle makes i18next echo the key, so assertions read the
// English source string the component passes to `t()`.
const i18n = createInstance()
await i18n.use(initReactI18next).init({ lng: 'en' })

const MODEL_NAME = 'gpt-4o'

function buildModel(): PricingModel {
  return {
    id: 1,
    model_name: MODEL_NAME,
    vendor_name: 'OpenAI',
    description: 'A test model description',
    quota_type: 0,
    model_ratio: 2,
    completion_ratio: 2,
    cache_ratio: 0.5,
    enable_groups: ['default'],
    tags: 'vision,audio',
    supported_endpoint_types: ['openai', 'anthropic'],
  }
}

type MountedCard = {
  container: HTMLDivElement
  clicks: string[]
  card: HTMLElement
}

async function mountCard(
  perf?: ModelPerfBadgeData,
  visible = true
): Promise<MountedCard> {
  const clicks: string[] = []
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)

  await act(async () =>
    root.render(
      <I18nextProvider i18n={i18n}>
        <ModelCard
          perf={perf}
          visible={visible}
          model={buildModel()}
          onClick={() => {
            clicks.push(MODEL_NAME)
          }}
        />
      </I18nextProvider>
    )
  )

  const card = getCard(container)
  return { container, clicks, card }
}

function getCard(container: HTMLDivElement): HTMLElement {
  const card = container.querySelector<HTMLElement>(
    '[data-slot="card"][role="button"]'
  )
  assert.ok(card, 'card is rendered')
  return card
}

async function unmountCard(mounted: MountedCard) {
  await act(async () => {
    mounted.container.replaceChildren()
  })
  mounted.container.remove()
}

describe('Model square card as a single activation surface', () => {
  afterAll(async () => {
    if (originalClipboard) {
      Object.defineProperty(
        globalThis.navigator,
        'clipboard',
        originalClipboard
      )
    }
    await resetSharedDomWindow()
  })

  test('exposes the whole card as the details control with an accessible name', async () => {
    const mounted = await mountCard()
    const card = getCard(mounted.container)

    assert.equal(card.getAttribute('role'), 'button')
    assert.equal(card.getAttribute('tabindex'), '0')
    assert.match(
      card.getAttribute('aria-label') ?? '',
      /^View details · gpt-4o$/
    )

    // The card is the details trigger; the copy button and the table
    // collapse button are independent focusable controls inside it.
    const cardButtons = [
      ...mounted.container.querySelectorAll<HTMLElement>(
        'button'
      ),
    ]
    assert.equal(
      cardButtons.filter((button) => button.textContent?.trim() === 'Details')
        .length,
      0
    )
    assert.equal(cardButtons.length, 2, 'copy button + table collapse button')

    assert.equal(card.textContent?.includes(MODEL_NAME), true)

    await unmountCard(mounted)
  })

  test('opens details when the card body is clicked but not when copying the model name', async () => {
    const mounted = await mountCard()
    const card = getCard(mounted.container)

    const priceBlock = card.querySelector<HTMLElement>(
      '[data-slot="card-content"] > div'
    )
    assert.ok(priceBlock)
    await act(async () => {
      priceBlock.click()
    })
    assert.deepEqual(mounted.clicks, [MODEL_NAME])

    const description = card.querySelector<HTMLElement>(
      '[data-slot="card-content"] p'
    )
    assert.ok(description)
    await act(async () => {
      description.click()
    })
    assert.deepEqual(mounted.clicks, [MODEL_NAME, MODEL_NAME])

    clipboardWrites.length = 0
    const copyButton = mounted.container.querySelector<HTMLElement>(
      'button[aria-label^="Copy model name"]'
    )
    assert.ok(copyButton)
    await act(async () => {
      copyButton.click()
    })
    assert.deepEqual(mounted.clicks, [MODEL_NAME, MODEL_NAME])
    assert.deepEqual(clipboardWrites, [MODEL_NAME])

    await unmountCard(mounted)
  })

  test('chart taps and touch drags select points without opening card details', async () => {
    const perf: ModelPerfBadgeData = {
      avg_latency_ms: 300,
      success_rate: 90,
      avg_tps: 30,
      recent_series: [
        { ts: 1_700_000_000, latency_ms: 200, success_rate: 80 },
        { ts: 1_700_003_600, latency_ms: 300, success_rate: 90 },
        { ts: 1_700_007_200, latency_ms: 400, success_rate: 100 },
      ],
    }
    const mounted = await mountCard(perf, false)
    const chart = mounted.card.querySelector<SVGSVGElement>(
      'svg[viewBox="0 0 400 96"]'
    )
    assert.ok(chart)
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

    const activeX = () =>
      Number(chart.querySelector('line.fill-foreground')?.getAttribute('x1'))
    const clickAt = (clientX: number) => {
      chart.dispatchEvent(
        new MouseEvent('click', { bubbles: true, clientX, clientY: 48 })
      )
    }

    await act(async () => clickAt(150))
    assert.equal(activeX(), 201)
    assert.deepEqual(mounted.clicks, [])

    await act(async () => {
      chart.dispatchEvent(
        new PointerEvent('pointerdown', {
          bubbles: true,
          clientX: 40,
          clientY: 48,
          pointerType: 'touch',
        })
      )
      chart.dispatchEvent(
        new PointerEvent('pointermove', {
          bubbles: true,
          clientX: 362,
          clientY: 48,
          pointerType: 'touch',
        })
      )
    })
    assert.equal(activeX(), 362)

    await act(async () => clickAt(362))
    assert.equal(activeX(), 362)
    assert.deepEqual(mounted.clicks, [])

    await act(async () => chart.dispatchEvent(new PointerEvent('pointerleave')))
    assert.equal(activeX(), 362)

    await act(async () => {
      chart.dispatchEvent(
        new PointerEvent('pointerdown', {
          bubbles: true,
          clientX: 40,
          clientY: 48,
          pointerType: 'touch',
        })
      )
    })
    assert.equal(activeX(), 40)

    await act(async () => {
      chart.dispatchEvent(
        new PointerEvent('pointermove', {
          bubbles: true,
          clientX: 362,
          clientY: 48,
          pointerType: 'touch',
        })
      )
    })
    assert.equal(activeX(), 362)
    await act(async () => {
      chart.dispatchEvent(
        new PointerEvent('pointercancel', {
          bubbles: true,
          pointerType: 'touch',
        })
      )
    })
    assert.equal(activeX(), 201)
    assert.deepEqual(mounted.clicks, [])

    await unmountCard(mounted)
  })


  test('swaps the title copy icon to a check after a successful copy', async () => {
    const mounted = await mountCard()
    const copyButton = mounted.container.querySelector<HTMLElement>(
      'button[aria-label^="Copy model name"]'
    )
    assert.ok(copyButton)
    // The test clipboard stub succeeds, so the button should show the check
    // icon right after the click.
    await act(async () => {
      copyButton.click()
    })
    assert.ok(copyButton.querySelector('svg.lucide-check'))
    assert.equal(copyButton.querySelectorAll('svg.lucide-copy').length, 0)
    // The check is temporary: the button reverts to the copy icon.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 1600))
    })
    assert.ok(copyButton.querySelector('svg.lucide-copy'))
    assert.equal(copyButton.querySelectorAll('svg.lucide-check').length, 0)

    await unmountCard(mounted)
  })

  test('activates the card from the keyboard with Enter and Space', async () => {
    const mounted = await mountCard()
    const card = getCard(mounted.container)

    for (const key of ['Enter', ' ']) {
      const event = new KeyboardEvent('keydown', {
        key,
        bubbles: true,
        cancelable: true,
      })
      await act(async () => {
        card.dispatchEvent(event)
      })

      // Space would otherwise scroll the page instead of activating the card.
      assert.equal(
        event.defaultPrevented,
        true,
        `${key} suppresses its default`
      )
      assert.equal(
        mounted.clicks.at(-1),
        MODEL_NAME,
        `key ${key} opens details`
      )
    }

    const callsBeforeOtherKeys = mounted.clicks.length
    for (const key of ['a', 'Escape', 'Tab']) {
      await act(async () => {
        card.dispatchEvent(
          new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })
        )
      })
    }
    assert.equal(mounted.clicks.length, callsBeforeOtherKeys)

    await unmountCard(mounted)
  })

  test('retains the copy button as an independent focusable control', async () => {
    const mounted = await mountCard()

    const copyButton = mounted.container.querySelector<HTMLElement>(
      'button[aria-label^="Copy model name"]'
    )
    assert.ok(copyButton)
    // The label names the action and identifies which model it copies.
    const copyLabel = copyButton.getAttribute('aria-label') ?? ''
    assert.ok(copyLabel.startsWith('Copy model name'))
    assert.ok(copyLabel.includes(MODEL_NAME))
    assert.equal(copyButton.tabIndex, 0)
    // The button is the card title: its text carries the model name.
    assert.ok(copyButton.textContent?.includes(MODEL_NAME))

    // Copying through the keyboard must not reach the card handler either.
    await act(async () => {
      copyButton.focus()
      copyButton.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })
      )
    })
    assert.deepEqual(mounted.clicks, [])

    await unmountCard(mounted)
  })
})

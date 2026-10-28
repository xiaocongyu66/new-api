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

// 卡片图标走 @lobehub/icons，其模块图在 bun 下会因 @lobehub/ui 缺少 Tag 导出而加载失败；
// 本测试不覆盖图标渲染，换成空实现避开该依赖图。
mock.module('@/lib/lobe-icon', () => ({
  getLobeIcon: () => null,
  LOBE_ICON_KEYS: [],
}))

// happy-dom ships a clipboard object without writeText, and no execCommand, so
// the copy button's real write path needs a boundary stub. The model name the
// stub records is asserted below, which keeps the test on the user-visible
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
}

async function mountCard(): Promise<MountedCard> {
  const clicks: string[] = []
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)

  await act(async () =>
    root.render(
      <I18nextProvider i18n={i18n}>
        <ModelCard
          model={buildModel()}
          onClick={() => {
            clicks.push(MODEL_NAME)
          }}
        />
      </I18nextProvider>
    )
  )

  return { container, clicks }
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

    // The old header action is gone: the card itself is the only trigger.
    const cardButtons = [
      ...mounted.container.querySelectorAll<HTMLElement>(
        '[data-slot="button"]'
      ),
    ]
    assert.equal(
      cardButtons.filter((button) => button.textContent?.trim() === 'Details')
        .length,
      0
    )
    assert.equal(cardButtons.length, 1, 'only the copy action remains')

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
      'button[aria-label="Copy model name"]'
    )
    assert.ok(copyButton)
    await act(async () => {
      copyButton.click()
    })
    assert.deepEqual(mounted.clicks, [MODEL_NAME, MODEL_NAME])
    assert.deepEqual(clipboardWrites, [MODEL_NAME])

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
      'button[aria-label="Copy model name"]'
    )
    assert.ok(copyButton)
    assert.equal(copyButton.getAttribute('aria-label'), 'Copy model name')
    assert.equal(copyButton.tabIndex, 0)
    assert.equal(copyButton.textContent?.trim(), '')

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

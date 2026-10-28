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
import { afterAll, describe, test } from 'bun:test'
import assert from 'node:assert/strict'
import { StrictMode, act, useState } from 'react'
import { createRoot } from 'react-dom/client'

import { resetSharedDomWindow } from '@/test-utils/happy-dom-env'

import { useCountUp } from '../use-count-up'

// Short animation: fast enough for a test, long enough that the rAF path
// (not the duration<=0 jump) is the one under test.
const DURATION = 40

function Probe(props: { target: number }) {
  const value = useCountUp(props.target, DURATION)
  return <span data-testid='value'>{value}</span>
}

function RetargetProbe(props: { duration: number }) {
  const [target, setTarget] = useState(100)
  const value = useCountUp(target, props.duration)
  return (
    <>
      <span data-testid='value'>{value}</span>
      <button
        type='button'
        data-testid='retarget'
        onClick={() => setTarget(500)}
      />
    </>
  )
}

async function mount(element: React.ReactElement) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  await act(async () => {
    root.render(element)
  })
  return {
    container,
    unmount: async () => {
      await act(async () => {
        root.unmount()
      })
      container.remove()
    },
  }
}

function readValue(container: HTMLElement): string {
  return container.querySelector('[data-testid="value"]')?.textContent ?? ''
}

async function settleFrames() {
  await act(async () => {
    const { promise, resolve } = Promise.withResolvers<void>()
    setTimeout(resolve, 25)
    await promise
  })
}

async function waitForSettle(container: HTMLElement, expected: string) {
  const deadline = Date.now() + 2000
  let last = readValue(container)
  while (Date.now() < deadline) {
    if (last === expected) return
    await settleFrames()
    last = readValue(container)
  }
  throw new Error(`count-up never settled at ${expected}; last value ${last}`)
}

describe('useCountUp', () => {
  test('settles at the target after the animation', async () => {
    const mounted = await mount(<Probe target={1686} />)
    await waitForSettle(mounted.container, '1686')
    await mounted.unmount()
  })

  // Regression: the first implementation recorded the target when the
  // animation STARTED. React StrictMode double-invokes mount effects, so the
  // second invocation saw "already animated" and skipped while the first
  // invocation's frame had been cancelled — the number stayed stuck at zero
  // for data that was already cached (e.g. paging a warm list). Settling
  // must be recorded on completion, not on start.
  test('settles under StrictMode with a warm target', async () => {
    const mounted = await mount(
      <StrictMode>
        <Probe target={1686} />
      </StrictMode>
    )
    await waitForSettle(mounted.container, '1686')
    await mounted.unmount()
  })

  test('settles at the new target when the target changes', async () => {
    const mounted = await mount(<RetargetProbe duration={40} />)
    await waitForSettle(mounted.container, '100')

    const retarget = mounted.container.querySelector<HTMLButtonElement>(
      '[data-testid="retarget"]'
    )
    assert.ok(retarget)
    await act(async () => {
      retarget.click()
    })
    await waitForSettle(mounted.container, '500')
    await mounted.unmount()
  })

  test('non-finite targets render zero', async () => {
    const mounted = await mount(<Probe target={Number.NaN} />)
    await settleFrames()
    await settleFrames()
    assert.equal(readValue(mounted.container), '0')
    await mounted.unmount()
  })
})

afterAll(() => {
  resetSharedDomWindow()
})

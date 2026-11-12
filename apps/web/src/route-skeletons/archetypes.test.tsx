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
import { afterAll, afterEach, describe, expect, it } from 'bun:test'

import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'

import {
  installDomGlobals,
  resetSharedDomWindow,
} from '@/test-utils/happy-dom-env'

import { AuthedShellSkeleton } from './archetypes'
import { RouteShellMountedProvider } from './shell-mounted-provider'

installDomGlobals()

/** The secondary panel's width is the cheapest marker of the drawn chrome. */
const SECONDARY_PANEL_CLASS = 'w-[17.5rem]'

async function renderSkeleton(node: ReactNode) {
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  await act(async () => {
    root.render(node)
  })
  return { host, root }
}

afterEach(() => {
  document.body.replaceChildren()
})

afterAll(() => {
  resetSharedDomWindow()
})

describe('AuthedShellSkeleton', () => {
  it('draws header, rail and panel when no real layout is mounted', async () => {
    const { host } = await renderSkeleton(
      <AuthedShellSkeleton>
        <div data-testid='skeleton-content' />
      </AuthedShellSkeleton>
    )

    expect(host.querySelector('header')).not.toBeNull()
    expect(host.querySelector('main')).not.toBeNull()
    const hasSecondaryPanel = [...host.querySelectorAll('div')].some((node) =>
      node.className.toString().includes(SECONDARY_PANEL_CLASS)
    )
    expect(hasSecondaryPanel).toBe(true)
  })

  it('renders only the content area when the real layout is already mounted', async () => {
    const { host } = await renderSkeleton(
      <RouteShellMountedProvider>
        <AuthedShellSkeleton>
          <div data-testid='skeleton-content' />
        </AuthedShellSkeleton>
      </RouteShellMountedProvider>
    )

    // A second header/sidebar inside the mounted layout is the regression this
    // guards: only the content block may be replaced.
    expect(host.querySelector('header')).toBeNull()
    const hasSecondaryPanel = [...host.querySelectorAll('div')].some((node) =>
      node.className.toString().includes(SECONDARY_PANEL_CLASS)
    )
    expect(hasSecondaryPanel).toBe(false)

    const status = host.querySelector('[role=status]')
    expect(status?.tagName).toBe('DIV')
    // The mounted layout already provides the main landmark.
    expect(host.querySelector('main')).toBeNull()
    expect(host.querySelector('[data-testid=skeleton-content]')).not.toBeNull()
  })

  it('keeps the content-only branch through intermediate elements', async () => {
    const { host } = await renderSkeleton(
      <RouteShellMountedProvider>
        <div>
          <AuthedShellSkeleton>
            <div data-testid='skeleton-content' />
          </AuthedShellSkeleton>
        </div>
      </RouteShellMountedProvider>
    )

    expect(host.querySelector('header')).toBeNull()
    expect(host.querySelector('[data-testid=skeleton-content]')).not.toBeNull()
  })
})

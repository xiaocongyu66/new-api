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
import { Window } from 'happy-dom'

/**
 * One happy-dom window shared by every frontend DOM test file.
 *
 * `bun test` evaluates ALL test files in a single process with a single module
 * registry. UI library singletons (Base UI portals through @floating-ui,
 * react-dom internals, sonner, ...) resolve `window`/`document` at the moment
 * their modules are FIRST evaluated. When every test file built its own
 * `new Window()` and rebound the globals at module load, whichever file
 * happened to load first pinned all shared library modules to ITS window.
 * Later files then queried their own live document while the cached libraries
 * mounted portals and rendered content into the earlier file's window, which
 * their `afterAll` had already closed. That is the cross-file pollution that
 * made the full suite fail order-dependently even though each file passed in
 * isolation.
 *
 * A single process-wide window removes the race: globals are installed once,
 * here, before any DOM test imports React or component code (apps/web
 * bunfig.toml preloads this module so installation happens before ANY test
 * module), so module capture order no longer matters. Test files must NOT
 * `close()` this window; instead call `resetSharedDomWindow()` from
 * `afterAll` to drop per-file DOM residue and cancel queued async work.
 */
const domWindow = new Window()

// Union of every DOM global any test file has needed; installing the superset
// keeps every file on the same window without per-file lists.
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLInputElement',
  'HTMLFormElement',
  'HTMLLabelElement',
  'HTMLFieldSetElement',
  'HTMLTextAreaElement',
  'HTMLDivElement',
  'HTMLSpanElement',
  'HTMLPreElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'KeyboardEvent',
  'PointerEvent',
  'MouseEvent',
  'FocusEvent',
  'CustomEvent',
  'MutationObserver',
  'ResizeObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const

/**
 * Bind the shared window's DOM globals onto `globalThis` (idempotent).
 *
 * Files that interleave DOM work with other suites may re-pin in
 * `beforeEach`; the installation itself happens at import time below.
 */
export function installDomGlobals(): void {
  for (const key of domGlobals) {
    Object.defineProperty(globalThis, key, {
      configurable: true,
      value: domWindow[key],
    })
  }

  const reactTestGlobals = globalThis as typeof globalThis & {
    IS_REACT_ACT_ENVIRONMENT?: boolean
  }
  reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true
}

installDomGlobals()

/**
 * Discard per-file DOM state without closing the shared window.
 *
 * Removes leftover body nodes, clears localStorage (zustand persist and
 * component caches read it), and cancels timers/animation frames queued by
 * mounted components so they cannot bleed into — or hang — later files.
 */
export async function resetSharedDomWindow(): Promise<void> {
  domWindow.document.body.replaceChildren()
  domWindow.localStorage.clear()
  await domWindow.happyDOM.cancelAsync()
}

export { domWindow }

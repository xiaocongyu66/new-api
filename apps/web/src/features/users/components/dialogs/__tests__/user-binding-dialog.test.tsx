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
import assert from 'node:assert/strict'
import { describe, test } from 'bun:test'

/**
 * The admin binding dialog sends `binding_type` straight into
 * DELETE /api/user/:id/bindings/:binding_type, and the Go handler matches it
 * against provider names. It used to send users-table column names instead
 * (github_id vs github), so every built-in provider except email answered
 * "invalid binding type" with a 200 + success:false — a silent no-op in the UI.
 *
 * This asserts the wire contract without rendering: the source of truth on the
 * Go side is the bindingColumnMap in identity/store_users.go plus the "qq"
 * special case, and those keys are duplicated here on purpose so a rename on
 * either side breaks a test rather than the feature.
 */
const BACKEND_BINDING_TYPES = new Set([
  'email',
  'github',
  'discord',
  'oidc',
  'wechat',
  'telegram',
  'linuxdo',
  'qq',
])

const { BUILTIN_BINDINGS } = await import('../builtin-bindings')

describe('admin binding dialog wire contract', () => {
  test('every builtin binding key is a binding_type the backend accepts', () => {
    for (const binding of BUILTIN_BINDINGS) {
      assert.ok(
        BACKEND_BINDING_TYPES.has(binding.key),
        `binding_type "${binding.key}" (${binding.label}) 后端 ClearBinding 不认，解绑会静默失败`
      )
    }
  })

  test('covers every provider the backend can clear', () => {
    const dialogKeys = new Set(BUILTIN_BINDINGS.map((binding) => binding.key))
    for (const bindingType of BACKEND_BINDING_TYPES) {
      assert.ok(
        dialogKeys.has(bindingType),
        `后端支持解绑 "${bindingType}"，但绑定管理弹窗没有这一项`
      )
    }
  })

  test('reads QQ from qq_open_id, which only the admin single-user endpoint returns', () => {
    const qq = BUILTIN_BINDINGS.find((binding) => binding.key === 'qq')
    assert.ok(qq)
    assert.equal(qq.field, 'qq_open_id')
  })
})

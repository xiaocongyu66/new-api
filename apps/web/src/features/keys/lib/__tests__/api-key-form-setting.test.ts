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

import { transformApiKeyToFormDefaults, transformFormDataToPayload } from '../api-key-form'
import type { ApiKey } from '../../types'

const baseApiKey: ApiKey = {
  id: 1,
  name: 'test',
  key: 'sk-test',
  status: 1,
  remain_quota: 0,
  used_quota: 0,
  setting: '',
  unlimited_quota: true,
  expired_time: -1,
  created_time: 1,
  accessed_time: 0,
  group: 'default',
  auto_groups: null,
  cross_group_retry: true,
  model_limits_enabled: false,
  model_limits: '',
  allow_ips: '',
}

describe('API key form mapping', () => {
  test('loads stored model limits without restoring token prompt settings', () => {
    const defaults = transformApiKeyToFormDefaults({
      ...baseApiKey,
      model_limits: 'model-a,model-b',
      setting: JSON.stringify({
        system_prompt_preset: 'silly_tavern',
        system_prompt: 'keep me',
        user_system_prompt_override: true,
      }),
    })

    assert.deepEqual(defaults.model_limits, ['model-a', 'model-b'])
    assert.equal('system_prompt_preset' in defaults, false)
    assert.equal('system_prompt' in defaults, false)
    assert.equal('user_system_prompt_override' in defaults, false)
  })

  test('clears token prompt settings when saving', () => {
    const defaults = transformApiKeyToFormDefaults(baseApiKey)

    assert.equal(transformFormDataToPayload(defaults).setting, '')
  })
})
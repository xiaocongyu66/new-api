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

export type UpstreamAccountKind = 'sub2api' | 'newapi'

export interface UpstreamAccount {
  id: number
  host: string
  base_url: string
  kind: UpstreamAccountKind
  username: string
  jk_group_id: string
  // Secrets are write-only: the API returns has_* flags and never the values.
  has_password: boolean
  has_token: boolean
  token_expires_at: number
  enabled: boolean
  last_test_at: number
  last_test_ok: boolean
  last_test_message: string
  created_at: number
  updated_at: number
}

export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface UpstreamAccountTestResult {
  ok: boolean
  kind: UpstreamAccountKind
  message: string
  model_count: number
  group_ids: string[]
}

export interface UpstreamAccountPayload {
  base_url: string
  kind: UpstreamAccountKind
  username: string
  password: string
  jk_group_id: string
  access_token: string
  refresh_token: string
  enabled: boolean
}

export type UpstreamAccountUpdatePayload = UpstreamAccountPayload & {
  id: number
}
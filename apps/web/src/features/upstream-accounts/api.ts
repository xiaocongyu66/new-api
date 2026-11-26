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
import { api } from '@/lib/api'

import type {
  ApiResponse,
  UpstreamAccount,
  UpstreamAccountPayload,
  UpstreamAccountTestResult,
  UpstreamAccountUpdatePayload,
} from './types'

const UPSTREAM_ACCOUNT_API = '/api/upstream-account/'

export async function getUpstreamAccounts(): Promise<
  ApiResponse<UpstreamAccount[]>
> {
  const res = await api.get<ApiResponse<UpstreamAccount[]>>(
    UPSTREAM_ACCOUNT_API
  )
  return res.data
}

export async function createUpstreamAccount(
  payload: UpstreamAccountPayload
): Promise<ApiResponse<UpstreamAccount>> {
  const res = await api.post<ApiResponse<UpstreamAccount>>(
    UPSTREAM_ACCOUNT_API,
    payload
  )
  return res.data
}

export async function updateUpstreamAccount(
  payload: UpstreamAccountUpdatePayload
): Promise<ApiResponse<UpstreamAccount>> {
  const res = await api.put<ApiResponse<UpstreamAccount>>(
    UPSTREAM_ACCOUNT_API,
    payload
  )
  return res.data
}

export async function deleteUpstreamAccount(
  id: number
): Promise<ApiResponse<null>> {
  const res = await api.delete<ApiResponse<null>>(
    `${UPSTREAM_ACCOUNT_API}${id}`
  )
  return res.data
}

export async function testUpstreamAccount(
  id: number
): Promise<ApiResponse<UpstreamAccountTestResult>> {
  const res = await api.post<ApiResponse<UpstreamAccountTestResult>>(
    `${UPSTREAM_ACCOUNT_API}${id}/test`
  )
  return res.data
}
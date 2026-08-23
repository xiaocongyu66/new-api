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

export interface RouteUnitAliasSummary {
  alias: string
  route_count: number
  total_weight: number
}

export interface RouteUnitView {
  id: number
  group: string
  public_model_alias: string
  channel_id: number
  channel_name: string
  channel_status: number
  base_url: string
  key_index: number
  upstream_model: string
  static_weight: number
  enabled: boolean
  expected_share: number
  health_score: number
}

export interface GetRouteUnitsResponse {
  items: RouteUnitView[]
  total_weight: number
}

export interface UpdateRouteUnitRequest {
  static_weight?: number
  enabled?: boolean
}

export async function getRouteUnitAliases(): Promise<RouteUnitAliasSummary[]> {
  const res = await api.get<RouteUnitAliasSummary[]>('/channel/route_unit/aliases')
  return res.data
}

export async function getRouteUnits(alias: string): Promise<GetRouteUnitsResponse> {
  const res = await api.get<GetRouteUnitsResponse>('/channel/route_unit/', {
    params: { alias },
  })
  return res.data
}

export async function updateRouteUnit(id: number, data: UpdateRouteUnitRequest): Promise<void> {
  await api.put(`/channel/route_unit/${id}`, data)
}
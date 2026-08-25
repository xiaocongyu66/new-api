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
  ewma_quality: number
  success_ewma: number
  ttft_ewma_ms: number
  tps_ewma: number
  sample_count: number
  // W5.1: six-factor score breakdown
  base_weight: number
  health_multiplier: number
  share_correction: number
  actual_share: number
  final_score: number
  share_opportunities: number
  share_selections: number
}

export interface GetRouteUnitsResponse {
  items: RouteUnitView[]
}

export interface UpdateRouteUnitRequest {
  static_weight?: number
  enabled?: boolean
}

export async function getRouteUnitAliases(): Promise<RouteUnitAliasSummary[]> {
  const res = await api.get('/api/channel/route_unit/aliases')
  return res.data.data
}

export async function getRouteUnits(alias: string): Promise<GetRouteUnitsResponse> {
  const res = await api.get('/api/channel/route_unit/', {
    params: { alias },
  })
  return res.data.data
}

export async function updateRouteUnit(id: number, data: UpdateRouteUnitRequest): Promise<void> {
  await api.put(`/api/channel/route_unit/${id}`, data)
}
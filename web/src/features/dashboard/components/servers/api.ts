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
  FleetAppRow,
  FleetHostDetail,
  FleetHostRow,
  FleetSeries,
  FleetSummary,
} from './types'

interface Envelope<T> {
  success: boolean
  message: string
  data: T
}

export async function getFleetSummary(rangeDays = 1): Promise<FleetSummary> {
  const res = await api.get<Envelope<FleetSummary>>('/api/fleet/summary', {
    params: { range: `${rangeDays}d` },
  })
  return res.data.data
}

export async function getFleetHosts(): Promise<FleetHostRow[]> {
  const res = await api.get<Envelope<FleetHostRow[]>>('/api/fleet/hosts')
  return res.data.data ?? []
}

export async function getFleetHostDetail(id: number): Promise<FleetHostDetail> {
  const res = await api.get<Envelope<FleetHostDetail>>(`/api/fleet/hosts/${id}`)
  return res.data.data
}

export async function getFleetSeries(
  rangeDays: number,
  groupBy: 'app' | 'host' = 'app'
): Promise<FleetSeries> {
  const res = await api.get<Envelope<FleetSeries>>('/api/fleet/series', {
    params: { range: `${rangeDays}d`, group_by: groupBy },
  })
  return res.data.data
}

export async function getFleetApps(
  top = 10,
  rangeDays = 1,
  hostId = 0
): Promise<FleetAppRow[]> {
  const res = await api.get<Envelope<FleetAppRow[]>>('/api/fleet/apps', {
    params: { top, range: `${rangeDays}d`, host_id: hostId },
  })
  return res.data.data ?? []
}

// ---------------------------------------------------------------------------
// 人工台账
// ---------------------------------------------------------------------------

export type FleetMetaPatch = Partial<FleetMetaFields>

export interface FleetMetaFields {
  display_name: string
  room: string
  rack: string
  device_model: string
  form_factor: string
  asset_no: string
  serial_no: string
  bmc_ip: string
  owner: string
  usage: string
  env: string
  purchased_at: string
  warranty_until: string
}

export interface FleetMetaHistoryRow {
  id: number
  host_id: number
  changed_at: number
  changed_by: string
  source: string
  diff_json: string
  snapshot_json: string
}

export async function updateFleetMeta(
  hostId: number,
  patch: FleetMetaPatch
): Promise<void> {
  await api.put(`/api/fleet/hosts/${hostId}/meta`, patch)
}

export async function getFleetMetaHistory(
  hostId: number
): Promise<FleetMetaHistoryRow[]> {
  const res = await api.get<Envelope<FleetMetaHistoryRow[]>>(
    `/api/fleet/hosts/${hostId}/meta/history`
  )
  return res.data.data ?? []
}

export async function rollbackFleetMeta(
  hostId: number,
  historyId: number
): Promise<void> {
  await api.post(`/api/fleet/hosts/${hostId}/meta/rollback`, {
    history_id: historyId,
  })
}

export interface FleetImportResult {
  dry_run: boolean
  matched: Array<{ host_id: number; hostname: string; ip: string }>
  unmatched: string[]
  applied: number
}

export async function importFleetMeta(
  items: Array<Record<string, unknown>>,
  dryRun: boolean
): Promise<FleetImportResult> {
  const res = await api.post<Envelope<FleetImportResult>>(
    '/api/fleet/meta/import',
    { dry_run: dryRun, items }
  )
  return res.data.data
}

// ---------------------------------------------------------------------------
// 应用别名（把容器/镜像映射成能读懂的业务名）
// ---------------------------------------------------------------------------

export interface FleetAppAlias {
  id: number
  match_type: 'service' | 'image' | 'name'
  match_value: string
  display_name: string
  owner: string
  updated_by: string
  updated_at: number
}

export async function getFleetAppAliases(): Promise<FleetAppAlias[]> {
  const res = await api.get<Envelope<FleetAppAlias[]>>('/api/fleet/app-aliases')
  return res.data.data ?? []
}

export async function upsertFleetAppAlias(body: {
  match_type: string
  match_value: string
  display_name: string
  owner?: string
}): Promise<void> {
  await api.put('/api/fleet/app-aliases', body)
}

export async function deleteFleetAppAlias(
  id: number,
  matchType: string,
  matchValue: string
): Promise<void> {
  await api.delete('/api/fleet/app-aliases', {
    data: { id, match_type: matchType, match_value: matchValue },
  })
}

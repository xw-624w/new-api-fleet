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

// 算力服务（fleet）：接口 DTO

export interface FleetSummary {
  hosts: { total: number; online: number }
  gpus: { total: number; nvidia: number; intel: number; in_use: number }
  avg_util: number
  vram_used: number
  vram_total: number
  gpu_hours_today: number
  /** 今日样本实际覆盖的小时数（服务刚启动时会远小于 24） */
  covered_hours_today: number
  last_ingest_at: number
  degraded: boolean
}

export interface FleetHostRow {
  id: number
  hostname: string
  display_name: string
  ip: string
  room: string
  owner: string
  usage: string
  status: 'online' | 'stale' | 'offline'
  last_seen: number
  cpu: number
  mem_used: number
  mem_total: number
  gpus: number
  gpu_summary: string
  util: number
  vram_used: number
  vram_total: number
  temp_max: number
  apps: number
  idle_cards: number
}

export interface FleetGpu {
  id: number
  host_id: number
  vendor: string
  gpu_index: number
  uuid: string
  model: string
  mem_total: number
  util: number
  mem_used: number
  temp: number
  power: number
}

export interface FleetApp {
  id: number
  host_id: number
  name: string
  display_name: string
  detail: string
  owner: string
  kind: string
  image: string
  compose_service: string
  container_id: string
  gpu_util: number
  /** 物理占用卡数 */
  gpu_count: number
  vram_used: number
  estimated: boolean
}

export interface FleetHostDetail {
  host: {
    id: number
    hostname: string
    ip: string
    os: string
    cpu_model: string
    cpu_cores: number
    mem_total: number
    status: string
  }
  meta: {
    host_id: number
    display_name: string
    room: string
    owner: string
    usage: string
    device_model: string
    bmc_ip: string
  }
  gpus: FleetGpu[]
  apps: FleetApp[]
}

export interface FleetSeries {
  step: number
  keys: string[]
  points: Array<Record<string, number>>
}

export interface FleetAppRow {
  name: string
  /** 别名解析后的业务名（没人标记时等于 name） */
  display_name: string
  /** 这个应用在服务什么：探测到的模型名或服务名 */
  detail: string
  owner: string
  kind: string
  host: string
  gpu_util: number
  /** 物理占用卡数（在哪几张卡上有显存分配） */
  gpu_count: number
  gpu_hours: number
  /** 统计窗口内该应用实际被采样的小时数；GPU·h 只按这段时间折算 */
  covered_hours: number
  vram_used: number
  estimated: boolean
}

export interface FleetSeriesPoint extends Record<string, number> {
  ts: number
}

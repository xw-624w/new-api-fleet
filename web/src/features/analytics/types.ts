export type ServerDimension = 'node' | 'channel'

export interface ServerUsageRow {
  key: string
  name: string
  requests: number
  token_used: number
  quota: number
}

export interface ServerUsageTotals {
  requests: number
  token_used: number
  quota: number
}

export interface ServerUsageData {
  dimension: ServerDimension
  start_timestamp: number
  end_timestamp: number
  rows: ServerUsageRow[]
  totals: ServerUsageTotals
}

export interface ConcurrencyPoint {
  timestamp: number
  active: number
}

export interface ConcurrencyData {
  bucket_seconds: number
  points: ConcurrencyPoint[]
  peak: number
  average: number
  calls: number
}

export interface CallLogRow {
  id: number
  created_at: number
  username: string
  token_name: string
  model_name: string
  channel: number
  use_time: number
  quota: number
  prompt_tokens: number
  completion_tokens: number
}

export interface CallLogPage {
  page: number
  page_size: number
  total: number
  items: CallLogRow[]
}

export interface ApiEnvelope<T> {
  success: boolean
  message?: string
  data?: T
}

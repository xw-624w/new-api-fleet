import { api } from '@/lib/api'

import type {
  ApiEnvelope,
  CallLogPage,
  ConcurrencyData,
  ServerDimension,
  ServerUsageData,
} from './types'

// ----------------------------------------------------------------------------
// Compute analytics APIs (admin only)
// ----------------------------------------------------------------------------

// 按"服务器"汇总算力使用：dimension=node（部署节点）或 channel（上游渠道）
export async function getServerUsage(
  dimension: ServerDimension,
  start: number,
  end: number
): Promise<ApiEnvelope<ServerUsageData>> {
  const res = await api.get('/api/data/analytics/servers', {
    params: {
      dimension,
      start_timestamp: start,
      end_timestamp: end,
    },
  })
  return res.data
}

// 并发曲线（由日志区间反推）
export async function getConcurrency(
  start: number,
  end: number,
  bucket = 60
): Promise<ApiEnvelope<ConcurrencyData>> {
  const res = await api.get('/api/data/analytics/concurrency', {
    params: {
      start_timestamp: start,
      end_timestamp: end,
      bucket,
    },
  })
  return res.data
}

// 调用明细：直接复用现成的 /api/log 接口（支持时间段、渠道、节点筛选与分页）
export async function getCallLogs(params: {
  start: number
  end: number
  page: number
  pageSize: number
  channel?: string
  nodeName?: string
}): Promise<ApiEnvelope<CallLogPage>> {
  const res = await api.get('/api/log', {
    params: {
      p: params.page,
      page_size: params.pageSize,
      start_timestamp: params.start,
      end_timestamp: params.end,
      ...(params.channel ? { channel: params.channel } : {}),
      ...(params.nodeName ? { node_name: params.nodeName } : {}),
    },
  })
  return res.data
}

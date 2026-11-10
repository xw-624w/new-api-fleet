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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import type { FleetSeries } from './types'

// 调色板：8 色循环，配合 Top-N 后最多 7 条（含“其他”），不会撞色
const PALETTE = [
  '#6366f1',
  '#22d3ee',
  '#f59e0b',
  '#a855f7',
  '#34d399',
  '#f472b6',
  '#94a3b8',
  '#facc15',
]

const TOP_N = 6
const TOOLTIP_MAX = 8

type Row = Record<string, number | null>

interface Gap {
  from: number
  to: number
}

interface TrendChartProps {
  series: FleetSeries | undefined
  groupBy: 'app' | 'host'
  onGroupByChange: (value: 'app' | 'host') => void
  /** 当前区间天数，决定 X 轴刻度粒度 */
  rangeDays: number
}

// X 轴刻度：按天聚合时只显示日期；1 天看时分；预计未来若放开小时粒度则带小时
function formatTick(ts: number, daily: boolean, rangeDays: number): string {
  const d = new Date(ts * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  if (daily) {
    return `${d.getMonth() + 1}/${d.getDate()}`
  }
  if (rangeDays <= 1) {
    return `${pad(d.getHours())}:${pad(d.getMinutes())}`
  }
  return `${d.getMonth() + 1}/${d.getDate()} ${pad(d.getHours())}时`
}

export function TrendChart({
  series,
  groupBy,
  onGroupByChange,
  rangeDays,
}: TrendChartProps) {
  const { t } = useTranslation()
  const [hidden, setHidden] = useState<Set<string>>(new Set())
  const [hovered, setHovered] = useState<string | null>(null)

  // 1. 只画 Top N，其余合并成“其他”
  const { keys, rows, gaps } = useMemo(() => {
    if (!series || series.points.length === 0) {
      return { keys: [] as string[], rows: [] as Row[], gaps: [] as Gap[] }
    }

    const totals = new Map<string, number>()
    for (const key of series.keys) {
      let sum = 0
      for (const p of series.points) sum += p[key] ?? 0
      totals.set(key, sum)
    }
    const sorted = [...totals.entries()].sort((a, b) => b[1] - a[1]).map((e) => e[0])
    const top = sorted.slice(0, TOP_N)
    const rest = sorted.slice(TOP_N)
    const otherLabel = t('Other')

    const outKeys = rest.length > 0 ? [...top, otherLabel] : top
    const step = series.step || 60

    // 2. 转成时间轴数据；中间断档处插入空行，让面积断开而不是连成直线
    const points = series.points
    const built: Row[] = []
    const gaps: Gap[] = []
    let prevTs = 0

    for (const p of points) {
      const ts = Number(p.ts)
      if (prevTs > 0 && ts - prevTs > step * 3) {
        built.push({ ts: prevTs + step })
        built.push({ ts: ts - step })
        // 超过 2 小时的空档单独记下来，图表下方会提示
        if (ts - prevTs >= 7200) {
          gaps.push({ from: prevTs, to: ts })
        }
      }
      const row: Row = { ts }
      for (const key of top) row[key] = p[key] ?? 0
      if (rest.length > 0) {
        let sum = 0
        for (const key of rest) sum += p[key] ?? 0
        row[otherLabel] = Math.round(sum * 10) / 10
      }
      built.push(row)
      prevTs = ts
    }

    return { keys: outKeys, rows: built, gaps }
  }, [series, t])

  const visibleKeys = keys.filter((k) => !hidden.has(k))

  if (rows.length === 0) {
    return (
      <p className='text-muted-foreground py-10 text-center text-sm'>
        {t('No samples in this range yet')}
      </p>
    )
  }

  const toggleKey = (key: string) => {
    setHidden((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  return (
    <div className='space-y-2'>
      <div className='flex justify-end'>
        <div className='bg-muted/60 inline-flex overflow-hidden rounded-lg border p-0.5'>
          <Button
            size='sm'
            variant={groupBy === 'app' ? 'default' : 'ghost'}
            className='h-6 rounded-md px-2 text-[11px]'
            onClick={() => onGroupByChange('app')}
          >
            {t('By app')}
          </Button>
          <Button
            size='sm'
            variant={groupBy === 'host' ? 'default' : 'ghost'}
            className='h-6 rounded-md px-2 text-[11px]'
            onClick={() => onGroupByChange('host')}
          >
            {t('By machine')}
          </Button>
        </div>
      </div>

      <div className='h-64 w-full'>
        <ResponsiveContainer width='100%' height='100%'>
          <AreaChart
            data={rows}
            margin={{ top: 8, right: 12, left: 0, bottom: 0 }}
          >
            <CartesianGrid
              strokeDasharray='3 3'
              vertical={false}
              stroke='rgba(127,127,127,0.22)'
            />
            <XAxis
              dataKey='ts'
              type='number'
              scale='time'
              domain={['dataMin', 'dataMax']}
              tick={{ fontSize: 11 }}
              tickLine={false}
              minTickGap={44}
              stroke='#9ca3af'
              tickFormatter={(v: number) =>
                formatTick(v, (series?.step ?? 0) >= 86400, rangeDays)
              }
            />
            <YAxis
              width={44}
              tick={{ fontSize: 11 }}
              tickLine={false}
              stroke='#9ca3af'
            />
            <Tooltip content={<FleetTooltip />} />
            {visibleKeys.map((key) => {
              const idx = keys.indexOf(key)
              const color = PALETTE[idx % PALETTE.length]
              const dim = hovered !== null && hovered !== key
              return (
                <Area
                  key={key}
                  type='monotone'
                  dataKey={key}
                  stackId='1'
                  stroke={color}
                  fill={color}
                  fillOpacity={dim ? 0.06 : 0.55}
                  strokeOpacity={dim ? 0.15 : 1}
                  strokeWidth={1.5}
                  isAnimationActive={false}
                  connectNulls={false}
                  onMouseEnter={() => setHovered(key)}
                  onMouseLeave={() => setHovered(null)}
                />
              )
            })}
          </AreaChart>
        </ResponsiveContainer>
      </div>

      {/* 大段无数据的提示 */}
      {gaps.length > 0 && (
        <div className='text-muted-foreground text-[11px]'>
          {t('No data during:')}{' '}
          {gaps
            .slice(0, 2)
            .map(
              (g) =>
                `${new Date(g.from * 1000).toLocaleString([], {
                  month: '2-digit',
                  day: '2-digit',
                  hour: '2-digit',
                  minute: '2-digit',
                })} → ${new Date(g.to * 1000).toLocaleString([], {
                  month: '2-digit',
                  day: '2-digit',
                  hour: '2-digit',
                  minute: '2-digit',
                })}`
            )
            .join('；')}
          {gaps.length > 2 ? ` ${t('and {{count}} more', { count: gaps.length - 2 })}` : ''}
        </div>
      )}

      {/* 自定义图例：单行、可横滚、点击隐藏 */}
      <div className='flex flex-nowrap gap-3 overflow-x-auto px-0.5 pb-1 pt-0.5'>
        {keys.map((key) => {
          const idx = keys.indexOf(key)
          const color = PALETTE[idx % PALETTE.length]
          const isHidden = hidden.has(key)
          return (
            <button
              key={key}
              type='button'
              onClick={() => toggleKey(key)}
              onMouseEnter={() => setHovered(key)}
              onMouseLeave={() => setHovered(null)}
              className={cn(
                'flex shrink-0 items-center gap-1.5 text-[11px] transition-opacity',
                isHidden ? 'opacity-35' : 'opacity-100'
              )}
              title={isHidden ? t('Click to show') : t('Click to hide')}
            >
              <span
                className='inline-block size-2 rounded-full'
                style={{ backgroundColor: color }}
              />
              <span
                className={cn(
                  isHidden && 'line-through',
                  'text-muted-foreground'
                )}
              >
                {key}
              </span>
            </button>
          )
        })}
      </div>
    </div>
  )
}

function FleetTooltip(props: {
  active?: boolean
  payload?: Array<{ name?: string; value?: number; color?: string }>
  label?: number
}) {
  const { t } = useTranslation()
  const { active, payload, label } = props
  if (!active || !payload || payload.length === 0) return null

  const items = payload
    .filter((p) => (p.value ?? 0) > 0)
    .sort((a, b) => (b.value ?? 0) - (a.value ?? 0))
  const shown = items.slice(0, TOOLTIP_MAX)
  const rest = items.length - shown.length

  return (
    <div className='bg-popover rounded-lg border px-2.5 py-2 text-xs shadow-md'>
      <div className='text-muted-foreground mb-1.5 font-medium'>
        {typeof label === 'number'
          ? new Date(label * 1000).toLocaleString()
          : String(label ?? '')}
      </div>
      <div className='space-y-0.5'>
        {shown.map((p) => (
          <div key={p.name} className='flex items-center gap-2'>
            <span
              className='inline-block size-2 shrink-0 rounded-full'
              style={{ backgroundColor: p.color }}
            />
            <span className='max-w-52 truncate'>{p.name}</span>
            <span className='ml-auto tabular-nums'>
              {Math.round((p.value ?? 0) * 10) / 10}
            </span>
          </div>
        ))}
      </div>
      {rest > 0 && (
        <div className='text-muted-foreground mt-1'>
          {t('+{{count}} more', { count: rest })}
        </div>
      )}
    </div>
  )
}

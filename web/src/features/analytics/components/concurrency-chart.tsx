import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { useTranslation } from 'react-i18next'

import dayjs from '@/lib/dayjs'

import type { ConcurrencyPoint } from '../types'

interface ConcurrencyChartProps {
  points: ConcurrencyPoint[]
  bucketSeconds: number
}

export function ConcurrencyChart({
  points,
  bucketSeconds,
}: ConcurrencyChartProps) {
  const { t } = useTranslation()

  const data = points.map((point) => ({
    label: dayjs(point.timestamp * 1000).format(
      bucketSeconds >= 86400 ? 'MM-DD' : 'MM-DD HH:mm'
    ),
    active: point.active,
  }))

  if (data.length === 0) {
    return (
      <p className='text-muted-foreground py-8 text-center text-sm'>
        {t('No data')}
      </p>
    )
  }

  return (
    <div className='h-64 w-full'>
      <ResponsiveContainer width='100%' height='100%'>
        <AreaChart
          data={data}
          margin={{ top: 8, right: 12, left: 0, bottom: 0 }}
        >
          <defs>
            <linearGradient id='computeConcurrencyFill' x1='0' y1='0' x2='0' y2='1'>
              <stop offset='0%' stopColor='#6366f1' stopOpacity={0.45} />
              <stop offset='100%' stopColor='#6366f1' stopOpacity={0.04} />
            </linearGradient>
          </defs>
          <CartesianGrid
            strokeDasharray='3 3'
            vertical={false}
            stroke='rgba(127,127,127,0.22)'
          />
          <XAxis
            dataKey='label'
            tick={{ fontSize: 11 }}
            minTickGap={28}
            tickLine={false}
            stroke='#9ca3af'
          />
          <YAxis
            width={44}
            allowDecimals={false}
            tick={{ fontSize: 11 }}
            tickLine={false}
            stroke='#9ca3af'
          />
          <Tooltip
            formatter={(value) => [value as number, t('Concurrency')]}
            labelFormatter={(label) => String(label)}
            contentStyle={{ fontSize: 12, borderRadius: 8 }}
          />
          <Area
            type='monotone'
            dataKey='active'
            stroke='#6366f1'
            strokeWidth={2}
            fill='url(#computeConcurrencyFill)'
            isAnimationActive={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}

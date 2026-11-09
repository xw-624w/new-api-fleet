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
import { describe, expect, it } from 'vitest'

import type { QuotaDataItem } from '@/features/dashboard/types'
import dayjs from '@/lib/dayjs'
import { formatChartTime } from '@/lib/time'

import { processChartData } from './charts'

// A fixed mid-week moment; the exact date does not matter, only the offsets.
const anchor = dayjs('2026-03-04T12:00:00')
const monday = anchor
  .subtract((anchor.day() + 6) % 7, 'day')
  .startOf('day')
const label = (offsetDays: number) =>
  `${monday.add(offsetDays, 'day').format('MM-DD')} - ${monday
    .add(offsetDays + 6, 'day')
    .format('MM-DD')}`

const at = (offsetDays: number, quota: number): QuotaDataItem => ({
  created_at: monday.add(offsetDays, 'day').add(9, 'hour').unix(),
  model_name: 'gpt-4o',
  quota,
  count: 1,
  token_used: 100,
})

type Point = { Time: string; rawQuota: number; Usage: number }

function linePoints(data: ReturnType<typeof processChartData>): Point[] {
  return (data.spec_line.data?.[0]?.values ?? []) as unknown as Point[]
}

function rawTotal(points: Point[]): number {
  return points.reduce((sum, item) => sum + (Number(item.rawQuota) || 0), 0)
}

describe('weekly bucketing', () => {
  it('maps every day of the same calendar week to ONE label', () => {
    const labels = [0, 1, 2, 3, 4, 5, 6].map((offset) =>
      formatChartTime(monday.add(offset, 'day').add(9, 'hour').unix(), 'week')
    )
    expect(new Set(labels).size).toBe(1)
    expect(labels[0]).toBe(label(0))
  })

  it('keeps different calendar weeks in different buckets', () => {
    const thisWeek = formatChartTime(monday.add(1, 'day').unix(), 'week')
    const nextWeek = formatChartTime(monday.add(8, 'day').unix(), 'week')
    expect(thisWeek).not.toBe(nextWeek)
    expect(nextWeek).toBe(label(7))
  })

  it('does not split one week into several chart buckets', () => {
    // Monday + Wednesday of the same week, plus the Monday of the next week
    const rows = [at(0, 100000), at(2, 200000), at(7, 300000)]
    const points = linePoints(processChartData(rows, 'week'))
    const populated = new Set(
      points.filter((item) => item.rawQuota > 0).map((item) => item.Time)
    )
    expect(populated.size).toBe(2)
    expect(populated.has(label(0))).toBe(true)
    expect(populated.has(label(7))).toBe(true)
  })

  it('loses no data: the weekly total equals the hourly total', () => {
    // five days spread over three weeks, always mid-week so a day-based label
    // would differ from the week's Monday
    const rows = [
      at(1, 11000),
      at(3, 22000),
      at(8, 33000),
      at(10, 44000),
      at(15, 55000),
    ]
    const hourTotal = rawTotal(linePoints(processChartData(rows, 'hour')))
    const dayTotal = rawTotal(linePoints(processChartData(rows, 'day')))
    const weekTotal = rawTotal(linePoints(processChartData(rows, 'week')))

    expect(hourTotal).toBe(165000)
    expect(dayTotal).toBe(hourTotal)
    expect(weekTotal).toBe(hourTotal)
  })

  it('leaves hour and day labels untouched', () => {
    const ts = monday.add(1, 'day').add(9, 'hour').unix()
    expect(formatChartTime(ts, 'day')).toBe(monday.add(1, 'day').format('MM-DD'))
    expect(formatChartTime(ts, 'hour')).toBe(
      `${monday.add(1, 'day').format('MM-DD')} 09:00`
    )
  })
})

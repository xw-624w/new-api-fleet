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

import { processChartData } from './charts'

type Datum = { Usage: number; rawQuota: number }

const rows: QuotaDataItem[] = [
  {
    created_at: 1700000000,
    model_name: 'gpt-4',
    quota: 500000,
    count: 10,
    token_used: 1234,
  },
  {
    created_at: 1700000000,
    model_name: 'gpt-4o',
    quota: 250000,
    count: 5,
    token_used: 4321,
  },
]

function barValues(
  data: ReturnType<typeof processChartData>
): Datum[] {
  const values = data.spec_line.data?.[0]?.values as unknown as Datum[]
  return values ?? []
}

describe('consumption distribution metric', () => {
  it('is metric-agnostic by default: quota is summed as-is', () => {
    const data = processChartData(rows, 'hour')
    const total = barValues(data).reduce((sum, item) => sum + item.rawQuota, 0)
    expect(total).toBe(750000)
  })

  it('plots real token usage when the tokens metric is selected', () => {
    const data = processChartData(rows, 'hour', undefined, undefined, 'tokens')
    const values = barValues(data)

    // raw values are token counts, not quota
    expect(values.reduce((sum, item) => sum + item.rawQuota, 0)).toBe(5555)
    // and the plotted height is the token count itself, never divided by quotaPerUnit
    expect(values.reduce((sum, item) => sum + item.Usage, 0)).toBe(5555)
  })

  it('reports the header total in the selected unit', () => {
    const quota = processChartData(rows, 'hour')
    const tokens = processChartData(rows, 'hour', undefined, undefined, 'tokens')

    expect(tokens.totalQuotaDisplay).toBe((5555).toLocaleString())
    // the quota total is rendered through the currency settings, so it must not
    // be the raw 750000 number
    expect(quota.totalQuotaDisplay).not.toBe('750000')
  })

  it('keeps quota and token series independent per model', () => {
    const tokens = processChartData(rows, 'hour', undefined, undefined, 'tokens')
    const byModel = new Map(
      barValues(tokens).map((item) => [
        (item as unknown as { Model: string }).Model,
        item.rawQuota,
      ])
    )
    expect(byModel.get('gpt-4')).toBe(1234)
    expect(byModel.get('gpt-4o')).toBe(4321)
  })

  it('does not change the call-count specs', () => {
    const quota = processChartData(rows, 'hour')
    const tokens = processChartData(rows, 'hour', undefined, undefined, 'tokens')
    expect(tokens.totalCountDisplay).toBe(quota.totalCountDisplay)
  })
})

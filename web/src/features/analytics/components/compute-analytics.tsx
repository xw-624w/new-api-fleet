import { useQuery } from '@tanstack/react-query'
import {
  ChevronLeft,
  ChevronRight,
  Coins,
  Cpu,
  Gauge,
  Loader2,
  Server,
  Users,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  formatNumber,
  formatQuota,
  formatTokens,
  formatTimestamp,
  formatUseTime,
} from '@/lib/format'
import { ROLE } from '@/lib/roles'
import {
  getServerErrorMessage,
  requireServerSuccess,
} from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { getCallLogs, getConcurrency, getServerUsage } from '../api'
import type { ServerDimension, ServerUsageRow } from '../types'
import { ConcurrencyChart } from './concurrency-chart'

const RANGE_OPTIONS = [
  { labelKey: '1 Day', days: 1 },
  { labelKey: '7 Days', days: 7 },
  { labelKey: '14 Days', days: 14 },
  { labelKey: '29 Days', days: 29 },
] as const

const DIMENSION_OPTIONS = [
  { value: 'node', labelKey: 'Server Nodes', icon: Server },
  { value: 'channel', labelKey: 'Upstream Channels', icon: Cpu },
] as const

const PAGE_SIZE = 20

function StatTile({
  icon: Icon,
  label,
  value,
}: {
  icon: typeof Coins
  label: string
  value: string
}) {
  return (
    <div className='bg-muted/40 rounded-lg border p-3'>
      <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
        <Icon className='size-3.5' />
        {label}
      </div>
      <div className='mt-1 text-lg font-semibold tabular-nums'>{value}</div>
    </div>
  )
}

export function ComputeAnalytics() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const isAdmin = (user?.role ?? 0) >= ROLE.ADMIN

  const [dimension, setDimension] = useState<ServerDimension>('node')
  const [rangeDays, setRangeDays] = useState<number>(7)
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const [page, setPage] = useState(1)

  const range = useMemo(() => {
    const end = Math.floor(Date.now() / 1000)
    return { start: end - rangeDays * 86400, end }
  }, [rangeDays])

  const usage = useQuery({
    queryKey: ['compute-usage', dimension, range.start, rangeDays],
    queryFn: async () =>
      requireServerSuccess(await getServerUsage(dimension, range.start, range.end)),
    enabled: isAdmin,
    staleTime: 30_000,
  })

  const concurrency = useQuery({
    queryKey: ['compute-concurrency', range.start, rangeDays],
    queryFn: async () =>
      requireServerSuccess(await getConcurrency(range.start, range.end, 300)),
    enabled: isAdmin,
    staleTime: 30_000,
  })

  const rows: ServerUsageRow[] = usage.data?.data?.rows ?? []
  const totals = usage.data?.data?.totals
  const concurrencyData = concurrency.data?.data
  const maxQuota = rows.reduce((max, row) => Math.max(max, row.quota), 0)

  const details = useQuery({
    queryKey: [
      'compute-details',
      dimension,
      selectedKey,
      page,
      range.start,
      rangeDays,
    ],
    queryFn: async () =>
      requireServerSuccess(
        await getCallLogs({
          start: range.start,
          end: range.end,
          page,
          pageSize: PAGE_SIZE,
          channel:
            dimension === 'channel' && selectedKey ? selectedKey : undefined,
          nodeName:
            dimension === 'node' && selectedKey ? selectedKey : undefined,
        })
      ),
    enabled: isAdmin && Boolean(selectedKey),
    staleTime: 15_000,
  })

  const detailPage = details.data?.data
  const totalPages = detailPage ? Math.max(1, Math.ceil(detailPage.total / PAGE_SIZE)) : 1

  const selectedRow = rows.find((row) => row.key === selectedKey)
  const selectedLabel =
    selectedRow?.name ||
    (dimension === 'node' ? t('Unassigned node') : `#${selectedKey ?? ''}`)

  if (!isAdmin) {
    return (
      <Card>
        <CardContent className='text-muted-foreground py-10 text-center text-sm'>
          {t('Compute analytics is available to administrators only')}
        </CardContent>
      </Card>
    )
  }

  const selectDimension = (next: ServerDimension) => {
    setDimension(next)
    setSelectedKey(null)
    setPage(1)
  }

  const selectRow = (row: ServerUsageRow) => {
    setSelectedKey((current) => (current === row.key ? null : row.key))
    setPage(1)
  }

  return (
    <div className='space-y-4'>
      <Card>
        <CardHeader>
          <CardTitle className='flex items-center gap-2'>
            <Gauge className='size-4' />
            {t('Compute Analytics')}
          </CardTitle>
          <CardDescription>
            {t('Compute usage grouped by the server that served the call.')}
          </CardDescription>
        </CardHeader>
        <CardContent className='space-y-3'>
          <div className='flex flex-wrap items-center gap-2'>
            <div className='bg-muted/60 inline-flex h-8 overflow-x-auto rounded-lg border p-0.5'>
              {RANGE_OPTIONS.map((option) => (
                <button
                  key={option.days}
                  type='button'
                  onClick={() => {
                    setRangeDays(option.days)
                    setPage(1)
                  }}
                  className={`inline-flex shrink-0 items-center rounded-md px-3 text-xs font-medium transition-colors ${
                    rangeDays === option.days
                      ? 'bg-background text-foreground shadow-sm'
                      : 'text-muted-foreground hover:text-foreground'
                  }`}
                >
                  {t(option.labelKey)}
                </button>
              ))}
            </div>
            {usage.isFetching && (
              <Loader2 className='text-muted-foreground size-4 animate-spin' />
            )}
          </div>

          {usage.isError ? (
            <p className='text-destructive py-4 text-sm'>
              {getServerErrorMessage(usage.error)}
            </p>
          ) : (
            <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
              <StatTile
                icon={Server}
                label={t('Requests')}
                value={formatNumber(totals?.requests ?? 0)}
              />
              <StatTile
                icon={Cpu}
                label={t('Tokens')}
                value={formatTokens(totals?.token_used ?? 0)}
              />
              <StatTile
                icon={Coins}
                label={t('Quota')}
                value={formatQuota(totals?.quota ?? 0)}
              />
              <StatTile
                icon={Gauge}
                label={t('Peak Concurrency')}
                value={formatNumber(concurrencyData?.peak ?? 0)}
              />
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('Concurrency')}</CardTitle>
          <CardDescription>
            {t('Reconstructed from call durations in the logs.')}{' '}
            {concurrencyData
              ? `${t('Average')} ${concurrencyData.average.toFixed(1)} · ${t('Calls')} ${formatNumber(concurrencyData.calls)}`
              : ''}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {concurrency.isError ? (
            <p className='text-destructive py-4 text-sm'>
              {getServerErrorMessage(concurrency.error)}
            </p>
          ) : concurrency.isLoading ? (
            <div className='text-muted-foreground flex items-center gap-2 py-8 text-sm'>
              <Loader2 className='size-4 animate-spin' />
              {t('Loading...')}
            </div>
          ) : (
            <ConcurrencyChart
              points={concurrencyData?.points ?? []}
              bucketSeconds={concurrencyData?.bucket_seconds ?? 300}
            />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className='flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
          <div>
            <CardTitle>{t('Compute usage by server')}</CardTitle>
            <CardDescription>
              {t('Click a row to see its call details.')}
            </CardDescription>
          </div>
          <div className='bg-muted/60 inline-flex h-8 overflow-x-auto rounded-lg border p-0.5'>
            {DIMENSION_OPTIONS.map((option) => {
              const Icon = option.icon
              return (
                <button
                  key={option.value}
                  type='button'
                  onClick={() => selectDimension(option.value)}
                  className={`inline-flex shrink-0 items-center gap-1.5 rounded-md px-3 text-xs font-medium transition-colors ${
                    dimension === option.value
                      ? 'bg-background text-foreground shadow-sm'
                      : 'text-muted-foreground hover:text-foreground'
                  }`}
                >
                  <Icon className='size-3.5' />
                  {t(option.labelKey)}
                </button>
              )
            })}
          </div>
        </CardHeader>
        <CardContent>
          {usage.isLoading ? (
            <div className='text-muted-foreground flex items-center gap-2 py-8 text-sm'>
              <Loader2 className='size-4 animate-spin' />
              {t('Loading...')}
            </div>
          ) : rows.length === 0 ? (
            <p className='text-muted-foreground py-8 text-center text-sm'>
              {t('No data')}
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>
                    {dimension === 'node' ? t('Server Nodes') : t('Upstream Channels')}
                  </TableHead>
                  <TableHead className='text-right'>{t('Requests')}</TableHead>
                  <TableHead className='text-right'>{t('Tokens')}</TableHead>
                  <TableHead className='text-right'>{t('Quota')}</TableHead>
                  <TableHead className='w-32'>{t('Share')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((row) => {
                  const label =
                    row.name ||
                    (dimension === 'node' ? t('Unassigned node') : `#${row.key}`)
                  const share = maxQuota > 0 ? (row.quota / maxQuota) * 100 : 0
                  const isSelected = row.key === selectedKey
                  return (
                    <TableRow
                      key={row.key || 'empty'}
                      onClick={() => selectRow(row)}
                      className={`cursor-pointer ${
                        isSelected ? 'bg-muted/60' : ''
                      }`}
                    >
                      <TableCell className='font-medium'>
                        <div className='flex items-center gap-2'>
                          {dimension === 'node' ? (
                            <Server className='text-muted-foreground size-3.5' />
                          ) : (
                            <Cpu className='text-muted-foreground size-3.5' />
                          )}
                          {label}
                        </div>
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {formatNumber(row.requests)}
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {formatTokens(row.token_used)}
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {formatQuota(row.quota)}
                      </TableCell>
                      <TableCell>
                        <div className='bg-muted h-2 w-full overflow-hidden rounded-full'>
                          <div
                            className='bg-primary h-full rounded-full'
                            style={{ width: `${Math.max(share, 2)}%` }}
                          />
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {selectedKey && (
        <Card>
          <CardHeader className='flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
            <div>
              <CardTitle className='flex items-center gap-2'>
                <Users className='size-4' />
                {t('Call Details')} · {selectedLabel}
              </CardTitle>
              <CardDescription>
                {detailPage
                  ? `${t('Total')} ${formatNumber(detailPage.total)}`
                  : ''}
              </CardDescription>
            </div>
            <div className='flex items-center gap-2'>
              <Button
                variant='outline'
                size='sm'
                disabled={page <= 1}
                onClick={() => setPage((current) => Math.max(1, current - 1))}
              >
                <ChevronLeft className='size-3.5' />
                {t('Previous')}
              </Button>
              <span className='text-muted-foreground text-xs'>
                {page} / {totalPages}
              </span>
              <Button
                variant='outline'
                size='sm'
                disabled={page >= totalPages}
                onClick={() => setPage((current) => current + 1)}
              >
                {t('Next')}
                <ChevronRight className='size-3.5' />
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            {details.isLoading ? (
              <div className='text-muted-foreground flex items-center gap-2 py-6 text-sm'>
                <Loader2 className='size-4 animate-spin' />
                {t('Loading...')}
              </div>
            ) : details.isError ? (
              <p className='text-destructive py-4 text-sm'>
                {getServerErrorMessage(details.error)}
              </p>
            ) : (detailPage?.items ?? []).length === 0 ? (
              <p className='text-muted-foreground py-6 text-center text-sm'>
                {t('No data')}
              </p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('Time')}</TableHead>
                    <TableHead>{t('Account')}</TableHead>
                    <TableHead>{t('Token')}</TableHead>
                    <TableHead>{t('Model')}</TableHead>
                    <TableHead className='text-right'>{t('Duration')}</TableHead>
                    <TableHead className='text-right'>{t('Quota')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {(detailPage?.items ?? []).map((item) => (
                    <TableRow key={item.id}>
                      <TableCell className='whitespace-nowrap text-xs'>
                        {formatTimestamp(item.created_at)}
                      </TableCell>
                      <TableCell className='text-xs'>
                        {item.username || '-'}
                      </TableCell>
                      <TableCell className='text-xs'>
                        {item.token_name || '-'}
                      </TableCell>
                      <TableCell className='text-xs'>
                        {item.model_name || '-'}
                      </TableCell>
                      <TableCell className='text-right text-xs tabular-nums'>
                        {formatUseTime(item.use_time)}
                      </TableCell>
                      <TableCell className='text-right text-xs tabular-nums'>
                        {formatQuota(item.quota)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  )
}

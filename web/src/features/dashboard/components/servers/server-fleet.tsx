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
import { useQuery } from '@tanstack/react-query'
import {
  Activity,
  AlertTriangle,
  Boxes,
  Cpu,
  Gauge,
  History,
  Layers,
  MemoryStick,
  PencilLine,
  Server,
  Shield,
  Thermometer,
  Zap,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

import { AliasDialog, type AliasTarget } from './alias-dialog'
import {
  getFleetApps,
  getFleetHostDetail,
  getFleetHosts,
  getFleetSeries,
  getFleetSummary,
} from './api'
import { MetaHistoryDialog } from './history-dialog'
import { MetaEditDialog } from './meta-dialog'
import { TrendChart } from './trend-chart'
import type { FleetApp, FleetHostDetail, FleetHostRow } from './types'

const RANGE_OPTIONS = [1, 7, 14, 29] as const

const APP_COLORS = [
  '#6366f1',
  '#22d3ee',
  '#f59e0b',
  '#a855f7',
  '#34d399',
  '#94a3b8',
]

function utilTone(util: number): string {
  if (util >= 75) return 'bg-emerald-500'
  if (util >= 40) return 'bg-sky-500'
  if (util >= 15) return 'bg-amber-500'
  return 'bg-rose-500'
}

function StatTile(props: {
  icon: typeof Cpu
  label: string
  value: string
  hint?: string
}) {
  const Icon = props.icon
  return (
    <div className='rounded-lg border p-3'>
      <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
        <Icon className='size-3.5' />
        {props.label}
      </div>
      <div className='mt-1 text-xl font-semibold tabular-nums'>
        {props.value}
      </div>
      {props.hint && (
        <div className='text-muted-foreground/70 mt-0.5 text-[11px]'>
          {props.hint}
        </div>
      )}
    </div>
  )
}

function UtilBar({ value }: { value: number }) {
  return (
    <div className='flex items-center gap-2'>
      <div className='bg-muted h-1.5 w-20 overflow-hidden rounded-full'>
        <div
          className={cn('h-full rounded-full', utilTone(value))}
          style={{ width: `${Math.min(100, Math.max(0, value))}%` }}
        />
      </div>
      <span className='w-9 text-right text-xs tabular-nums'>
        {Math.round(value)}%
      </span>
    </div>
  )
}

export function ServerFleet() {
  const { t } = useTranslation()
  const [rangeDays, setRangeDays] = useState<number>(1)
  const [poolScope, setPoolScope] = useState<
    'all' | 'dedicated' | 'shared' | 'unassigned'
  >('all')
  const [groupBy, setGroupBy] = useState<'app' | 'host'>('app')
  const [hostFilter, setHostFilter] = useState<number>(0)
  const [selectedHostId, setSelectedHostId] = useState<number | null>(null)
  const [aliasOpen, setAliasOpen] = useState(false)
  const [aliasTarget, setAliasTarget] = useState<AliasTarget | null>(null)

  const summary = useQuery({
    queryKey: ['fleet-summary', rangeDays, poolScope],
    queryFn: () =>
      getFleetSummary(rangeDays, poolScope === 'all' ? '' : poolScope),
    refetchInterval: 30_000,
  })

  const hostsQuery = useQuery({
    queryKey: ['fleet-hosts'],
    queryFn: getFleetHosts,
    refetchInterval: 30_000,
  })

  const appsQuery = useQuery({
    queryKey: ['fleet-apps', rangeDays, hostFilter, poolScope],
    queryFn: () =>
      getFleetApps(8, rangeDays, hostFilter, poolScope === 'all' ? '' : poolScope),
    refetchInterval: 60_000,
  })

  const seriesQuery = useQuery({
    queryKey: ['fleet-series', rangeDays, groupBy],
    queryFn: () => getFleetSeries(rangeDays, groupBy),
    refetchInterval: 60_000,
  })

  const detailQuery = useQuery({
    queryKey: ['fleet-host', selectedHostId],
    queryFn: () => getFleetHostDetail(selectedHostId as number),
    enabled: selectedHostId != null,
    refetchInterval: 30_000,
  })

  const hosts = hostsQuery.data ?? []
  // 选了某个池，列表只留含该池卡的机器，和上面的「共 N 台」对得上
  const visibleHosts =
    poolScope === 'all'
      ? hosts
      : hosts.filter((h) =>
          poolScope === 'dedicated'
            ? h.pool_dedicated > 0
            : poolScope === 'shared'
              ? h.pool_shared > 0
              : h.pool_unassigned > 0
        )
  const s = summary.data
  const loading =
    summary.isLoading || hostsQuery.isLoading || seriesQuery.isLoading

  const idleOf = (h: (typeof hosts)[number]) =>
    poolScope === 'all'
      ? h.idle_cards
      : poolScope === 'dedicated'
        ? h.idle_dedicated
        : poolScope === 'shared'
          ? h.idle_shared
          : h.idle_unassigned

  const wasted = hosts
    .filter((h) => idleOf(h) > 0)
    .sort((a, b) => idleOf(b) - idleOf(a))

  // 把「利用率之和」换算成「占当前口径算力的百分比」：value ÷ 卡数
  // 口径跟着上面的统计口径切换，分母就是当前池的卡数
  const scopeSeries = useMemo(() => {
    const raw = seriesQuery.data
    const cards = s?.gpus.total ?? 0
    if (!raw || cards <= 0) return raw
    const points = raw.points.map((p) => {
      const out: Record<string, number> = { ts: Number(p.ts) }
      for (const k of raw.keys) out[k] = (p[k] ?? 0) / cards
      return out
    })
    return { ...raw, points }
  }, [seriesQuery.data, s?.gpus.total])

  if (loading) {
    return (
      <div className='space-y-3'>
        <Skeleton className='h-24 w-full' />
        <Skeleton className='h-64 w-full' />
      </div>
    )
  }

  const empty = hosts.length === 0

  return (
    <div className='space-y-3 sm:space-y-4'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex items-center gap-2'>
          {s && s.degraded ? (
            <Badge variant='outline' className='text-amber-600'>
              {t('Monitoring source offline')}
            </Badge>
          ) : (
            <Badge variant='secondary' className='font-normal'>
              {t('Live')}
            </Badge>
          )}
          <span className='text-muted-foreground text-xs'>
            {s?.last_ingest_at
              ? t('Last report {{time}}', {
                  time: new Date(s.last_ingest_at * 1000).toLocaleString(),
                })
              : t('No agent has reported yet')}
          </span>
          {poolScope !== 'all' ? (
            <Badge variant='outline' className='text-[11px]'>
              仅
              {poolScope === 'dedicated'
                ? '专属'
                : poolScope === 'shared'
                  ? '弹性'
                  : '未分配'}
              卡
            </Badge>
          ) : null}
        </div>
        <div className='flex items-center gap-2'>
          <div className='bg-muted/60 inline-flex overflow-hidden rounded-lg border p-0.5'>
            {RANGE_OPTIONS.map((days) => (
              <Button
                key={days}
                size='sm'
                variant={rangeDays === days ? 'default' : 'ghost'}
                className='h-7 rounded-md px-3 text-xs'
                onClick={() => setRangeDays(days)}
              >
                {t('{{count}} Days', { count: days })}
              </Button>
            ))}
          </div>
        </div>
      </div>

      {/* 口径切换：切到某个池后，下面 5 张 KPI 全部只算该池的卡 */}
      <div className='flex flex-wrap items-center gap-2'>
        <span className='text-muted-foreground text-xs'>统计口径</span>
        <div className='bg-muted/60 inline-flex overflow-hidden rounded-lg border p-0.5'>
          {(
            [
              ['all', '全部'],
              ['dedicated', '专属'],
              ['shared', '弹性'],
              ['unassigned', '未分配'],
            ] as const
          ).map(([key, label]) => (
            <Button
              key={key}
              size='sm'
              variant={poolScope === key ? 'default' : 'ghost'}
              className='h-7 rounded-md px-3 text-xs'
              onClick={() => setPoolScope(key)}
            >
              {label}
            </Button>
          ))}
        </div>
        <span className='text-muted-foreground/70 text-[11px]'>
          {poolScope === 'all'
            ? '全部显卡'
            : `仅统计${poolScope === 'dedicated' ? '专属' : poolScope === 'shared' ? '弹性' : '未分配'}卡`}
        </span>
      </div>

      <div className='grid grid-cols-2 gap-3 lg:grid-cols-5'>
        <StatTile
          icon={Server}
          label={t('Total GPUs')}
          value={String(s?.gpus.total ?? 0)}
          hint={t('NVIDIA {{nvidia}} · Intel {{intel}}', {
            nvidia: s?.gpus.nvidia ?? 0,
            intel: s?.gpus.intel ?? 0,
          })}
        />
        <StatTile
          icon={Activity}
          label={t('GPUs In Use')}
          value={String(s?.gpus.in_use ?? 0)}
          hint={t('{{count}} hosts online', { count: s?.hosts.online ?? 0 })}
        />
        <StatTile
          icon={Gauge}
          label={t('Avg Utilization')}
          value={`${Math.round(s?.avg_util ?? 0)}%`}
          hint={t('All collected GPUs')}
        />
        <StatTile
          icon={MemoryStick}
          label={t('VRAM Used')}
          value={`${((s?.vram_used ?? 0) / 1024).toFixed(1)} / ${((s?.vram_total ?? 0) / 1024).toFixed(1)} GB`}
          hint={t('Used {{pct}}%', {
            pct: Math.round(
              ((s?.vram_used ?? 0) / Math.max(1, s?.vram_total ?? 0)) * 100
            ),
          })}
        />
        <StatTile
          icon={Zap}
          label={(() => {
            const h = s?.covered_hours_today ?? 0
            // 选了 7/29 天但数据还没攒够时，如实显示实际覆盖时长，不冒充整个区间
            if (rangeDays > 1 && h > 0 && h < rangeDays * 24 - 2) {
              return t('Average GPUs over {{hours}}h', {
                hours: Math.round(h),
              })
            }
            return rangeDays === 1
              ? t('Average GPUs running today')
              : t('Average GPUs over {{days}} days', { days: rangeDays })
          })()}
          value={
            (s?.covered_hours_today ?? 0) > 0.05
              ? `${((s?.gpu_hours_today ?? 0) / (s?.covered_hours_today ?? 1)).toFixed(1)} ${t('GPUs')}`
              : t('Collecting')
          }
          hint={
            (s?.covered_hours_today ?? 0) > 0.05
              ? t('{{hours}} card-hours over {{covered}}h', {
                  hours: (s?.gpu_hours_today ?? 0).toFixed(1),
                  covered: (s?.covered_hours_today ?? 0).toFixed(0),
                })
              : t('not enough samples yet')
          }
        />
      </div>

      {/* 算力区分：专属 / 弹性 / 未分配 */}
      {poolScope === 'all' && s?.pools ? (
        <div className='grid grid-cols-3 gap-3'>
          <StatTile
            icon={Shield}
            label='专属卡'
            value={String(s.pools.dedicated?.cards ?? 0)}
            hint={`${s.pools.dedicated?.in_use ?? 0} 在用 · ${(
              s.pools.dedicated?.gpu_hours ?? 0
            ).toFixed(1)} 卡时 · ${s.pools.dedicated?.projects ?? 0} 个项目`}
          />
          <StatTile
            icon={Zap}
            label='弹性卡'
            value={String(s.pools.shared?.cards ?? 0)}
            hint={`${s.pools.shared?.in_use ?? 0} 在用 · ${(
              s.pools.shared?.gpu_hours ?? 0
            ).toFixed(1)} 卡时 · ${s.pools.shared?.idle ?? 0} 张空闲`}
          />
          <StatTile
            icon={Layers}
            label='未分配'
            value={String(s.pools.unassigned?.cards ?? 0)}
            hint='未打标的卡，不计入弹性池'
          />
        </div>
      ) : null}

      {empty ? (
        <Card>
          <CardContent className='py-12 text-center'>
            <p className='text-muted-foreground text-sm'>
              {t('No agent has reported yet')}
            </p>
            <p className='text-muted-foreground/70 mt-2 text-xs'>
              {t('Generate a token and start fleet-agent on your servers')}
            </p>
          </CardContent>
        </Card>
      ) : (
        <Card>
          <CardHeader className='flex-row items-center justify-between'>
            <div>
              <CardTitle className='flex items-center gap-2 text-base'>
                <Boxes className='size-4' />
                {t('Server List')}
              </CardTitle>
              <CardDescription>
                {t('Pick a host to inspect its GPUs and deployed apps')}
              </CardDescription>
            </div>
            <div className='text-muted-foreground text-xs'>
              {t('{{online}} online / {{total}} total', {
                online: s?.hosts.online ?? 0,
                total: s?.hosts.total ?? 0,
              })}
            </div>
          </CardHeader>
          <CardContent className='overflow-x-auto'>
            <table className='w-full text-sm'>
              <thead>
                <tr className='text-muted-foreground border-b text-left text-xs'>
                  <th className='py-2 pr-3 font-medium'>{t('Host')}</th>
                  <th className='py-2 pr-3 font-medium'>{t('Room')}</th>
                  <th className='py-2 pr-3 font-medium'>{t('GPU')}</th>
                  <th className='py-2 pr-3 font-medium'>算力池</th>
                  <th className='py-2 pr-3 font-medium'>{t('Utilization')}</th>
                  <th className='py-2 pr-3 font-medium'>{t('VRAM')}</th>
                  <th className='py-2 pr-3 font-medium'>{t('Temp')}</th>
                  <th className='py-2 pr-3 font-medium'>{t('Apps')}</th>
                  <th className='py-2 font-medium'>{t('Status')}</th>
                </tr>
              </thead>
              <tbody>
                {visibleHosts.map((host) => (
                  <tr
                    key={host.id}
                    className={cn(
                      'hover:bg-muted/40 cursor-pointer border-b last:border-0',
                      selectedHostId === host.id && 'bg-muted/50'
                    )}
                    onClick={() => {
                      const next = selectedHostId === host.id ? null : host.id
                      setSelectedHostId(next)
                      // 点机器 = 同时把应用排行切到这台机器；再点一次恢复全部
                      setHostFilter(next ?? 0)
                    }}
                  >
                    <td className='py-2 pr-3 font-medium'>
                      {host.display_name || host.hostname}
                      {host.ip ? (
                        <span className='text-muted-foreground ml-1 text-[11px] font-normal'>
                          {host.ip}
                        </span>
                      ) : null}
                    </td>
                    <td className='text-muted-foreground py-2 pr-3 text-xs'>
                      {host.room || t('Not registered')}
                    </td>
                    <td className='py-2 pr-3 text-xs'>{host.gpu_summary}</td>
                    <td className='py-2 pr-3 text-[11px]'>
                      <div className='flex flex-wrap items-center gap-1'>
                        {host.pool_dedicated > 0 ? (
                          <span className='rounded bg-sky-500/12 px-1 py-px text-sky-600'>
                            专属{host.pool_dedicated}
                          </span>
                        ) : null}
                        {host.pool_shared > 0 ? (
                          <span className='rounded bg-emerald-500/12 px-1 py-px text-emerald-600'>
                            弹性{host.pool_shared}
                          </span>
                        ) : null}
                        {host.pool_unassigned > 0 ? (
                          <span className='rounded bg-amber-500/12 px-1 py-px text-amber-600'>
                            未分配{host.pool_unassigned}
                          </span>
                        ) : null}
                        {host.pool_dedicated +
                          host.pool_shared +
                          host.pool_unassigned ===
                        0 ? (
                          <span className='text-muted-foreground/60'>—</span>
                        ) : null}
                      </div>
                    </td>
                    <td className='py-2 pr-3'>
                      {host.status === 'offline' ? (
                        '—'
                      ) : (
                        <UtilBar value={host.util} />
                      )}
                    </td>
                    <td className='text-muted-foreground py-2 pr-3 text-xs tabular-nums'>
                      {host.status === 'offline'
                        ? '—'
                        : `${(host.vram_used / 1024).toFixed(1)} / ${(host.vram_total / 1024).toFixed(1)} G`}
                    </td>
                    <td className='text-muted-foreground py-2 pr-3 text-xs tabular-nums'>
                      {host.status === 'offline'
                        ? '—'
                        : `${Math.round(host.temp_max)}℃`}
                    </td>
                    <td className='text-muted-foreground py-2 pr-3 text-xs'>
                      {host.apps}
                    </td>
                    <td className='py-2'>
                      <StatusBadge status={host.status} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </CardContent>
        </Card>
      )}

      <AliasDialog
        open={aliasOpen}
        onOpenChange={setAliasOpen}
        target={aliasTarget}
        onSaved={() => {
          void detailQuery.refetch()
          void appsQuery.refetch()
        }}
      />

      {selectedHostId != null && detailQuery.data && (
        <HostDetailCard
          detail={detailQuery.data}
          onRefresh={() => {
            void detailQuery.refetch()
            void hostsQuery.refetch()
          }}
          onLabelApp={(app) => {
            const matchType =
              app.compose_service !== ''
                ? 'service'
                : app.image !== ''
                  ? 'image'
                  : 'name'
            setAliasTarget({
              matchType,
              matchValue:
                matchType === 'service'
                  ? app.compose_service
                  : matchType === 'image'
                    ? app.image
                    : app.name,
              currentDisplay: app.display_name || app.name,
              detail: app.detail,
            })
            setAliasOpen(true)
          }}
        />
      )}

      <Card>
        <CardHeader className='flex-row items-center justify-between'>
          <div>
            <CardTitle className='flex items-center gap-2 text-base'>
              <Activity className='size-4' />
              {t('Compute Trend')}
            </CardTitle>
            <CardDescription>
              {t(
                'Top {{count}} items, the rest merged into Other. Click legend to toggle.',
                { count: 6 }
              )}
              <span className='mt-0.5 block text-[11px]'>
                数字 = 占「
                {poolScope === 'all'
                  ? '全部'
                  : poolScope === 'dedicated'
                    ? '专属'
                    : poolScope === 'shared'
                      ? '弹性'
                      : '未分配'}
                」口径 {s?.gpus.total ?? 0} 张卡总算力的百分比
              </span>
            </CardDescription>
          </div>
          <div className='text-muted-foreground text-xs'>单位：%</div>
        </CardHeader>
        <CardContent>
          <TrendChart
            series={scopeSeries}
            groupBy={groupBy}
            onGroupByChange={setGroupBy}
            rangeDays={rangeDays}
            suffix='%'
          />
        </CardContent>
      </Card>

      <div className='grid gap-3 lg:grid-cols-2'>
        <Card>
          <CardHeader>
            <CardTitle className='flex items-center gap-2 text-base'>
              <Cpu className='size-4' />
              {t('App Ranking')}
            </CardTitle>
            <CardDescription>
              {rangeDays === 1
                ? t('Average GPU utilization today')
                : t('Average GPU utilization over {{days}} days', {
                    days: rangeDays,
                  })}
              {poolScope !== 'all' ? (
                <span className='mt-0.5 block text-[11px]'>
                  已按「
                  {poolScope === 'dedicated'
                    ? '专属'
                    : poolScope === 'shared'
                      ? '弹性'
                      : '未分配'}
                  」池的卡占比折算
                </span>
              ) : null}
            </CardDescription>

            {/* 机器筛选：默认全部 */}
            <div className='flex flex-wrap gap-1.5 pt-1'>
              <Button
                size='sm'
                variant={hostFilter === 0 ? 'default' : 'outline'}
                className='h-6 px-2 text-[11px]'
                onClick={() => setHostFilter(0)}
              >
                {t('All machines')}
              </Button>
              {hosts.map((h) => (
                <Button
                  key={h.id}
                  size='sm'
                  variant={hostFilter === h.id ? 'default' : 'outline'}
                  className='h-6 px-2 text-[11px]'
                  onClick={() =>
                    setHostFilter(hostFilter === h.id ? 0 : h.id)
                  }
                >
                  {h.display_name || h.hostname}
                </Button>
              ))}
            </div>
          </CardHeader>
          <CardContent className='space-y-3'>
            {(appsQuery.data ?? []).length === 0 && (
              <p className='text-muted-foreground py-6 text-center text-sm'>
                {t('No app-level attribution yet')}
              </p>
            )}
            {(appsQuery.data ?? []).map((app, i) => {
              const cards = app.gpu_count > 0 ? app.gpu_count : 0
              // 每张卡的平均占用：122% 摊到 2 张卡 = 每张 61%，比“总和 122%”好懂
              const perCard =
                cards > 0 ? Math.min(100, app.gpu_util / cards) : app.gpu_util
              const maxPerCard = Math.max(
                1,
                ...(appsQuery.data ?? []).map((a) =>
                  a.gpu_count > 0
                    ? Math.min(100, a.gpu_util / a.gpu_count)
                    : a.gpu_util
                )
              )
              return (
                <div key={`${app.host}-${app.name}`} className='space-y-1'>
                  <div className='flex items-baseline justify-between gap-2 text-xs'>
                    <span className='min-w-0 truncate font-medium'>
                      {app.display_name || app.name}
                      <span className='text-muted-foreground ml-1 font-normal'>
                        @{app.host}
                      </span>
                    </span>
                    <span className='shrink-0 tabular-nums'>
                      {cards > 0 ? (
                        <span className='text-xs'>
                          <span className='text-muted-foreground'>
                            {t('Occupies')}{' '}
                          </span>
                          <span className='text-sm font-semibold'>{cards}</span>
                          <span className='text-muted-foreground ml-0.5'>
                            {t('GPUs')}
                          </span>
                        </span>
                      ) : (
                        <span className='text-muted-foreground'>
                          {t('GPU share unknown')}
                        </span>
                      )}
                    </span>
                  </div>
                  {app.detail ? (
                    <div className='text-muted-foreground truncate text-[11px]'>
                      {app.detail}
                      {app.display_name && app.display_name !== app.name ? (
                        <span className='opacity-60'> · {app.name}</span>
                      ) : null}
                    </div>
                  ) : null}

                  <div className='bg-muted h-2 overflow-hidden rounded-full'>
                    <div
                      className='h-full rounded-full'
                      style={{
                        width: `${Math.max(3, (perCard / maxPerCard) * 100)}%`,
                        backgroundColor: APP_COLORS[i % APP_COLORS.length],
                      }}
                    />
                  </div>

                  <div className='text-muted-foreground flex justify-between text-[10px] tabular-nums'>
                    <span>
                      {cards > 0
                        ? t('{{pct}}% busy per card', {
                            pct: perCard.toFixed(0),
                          })
                        : t('no GPU usage in this range')}
                    </span>
                    <span>
                      {app.gpu_count > 1
                        ? t('{{cards}} cards in total', { cards })
                        : app.gpu_count === 1
                          ? t('single card')
                          : ''}
                    </span>
                  </div>
                </div>
              )
            })}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className='flex items-center gap-2 text-base'>
              <AlertTriangle className='size-4' />
              {t('Waste Watch')}
            </CardTitle>
            <CardDescription>
              {t('Cards under 5% utilization — idle machines included')}
            </CardDescription>
          </CardHeader>
          <CardContent className='space-y-2'>
            {wasted.length === 0 && (
              <p className='text-muted-foreground py-6 text-center text-sm'>
                {t('No idle-but-reserved GPUs right now')}
              </p>
            )}
            {wasted.map((host) => (
              <div
                key={host.id}
                className='flex items-center justify-between rounded-md border px-3 py-2'
              >
                <div className='min-w-0'>
                  <div className='truncate text-sm font-medium'>
                    {host.display_name || host.hostname}
                  </div>
                  <div className='text-muted-foreground text-[11px]'>
                    {host.gpu_summary} · {host.room || t('Not registered')}
                  </div>
                </div>
                <div className='flex items-center gap-3 text-xs tabular-nums'>
                  <span className='text-rose-500'>
                    {idleOf(host)} {t('idle cards')}
                    {poolScope !== 'all' ? (
                      <span className='text-muted-foreground'>
                        （
                        {poolScope === 'dedicated'
                          ? '专属'
                          : poolScope === 'shared'
                            ? '弹性'
                            : '未分配'}
                        池）
                      </span>
                    ) : null}
                  </span>
                  <span className='text-muted-foreground'>
                    {host.vram_total > 0
                      ? Math.round((host.vram_used / host.vram_total) * 100)
                      : 0}
                    % {t('VRAM')}
                  </span>
                </div>
              </div>
            ))}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function StatusBadge({ status }: { status: FleetHostRow['status'] }) {
  const { t } = useTranslation()
  if (status === 'online') {
    return (
      <Badge
        variant='outline'
        className='border-emerald-500/40 text-emerald-600 dark:text-emerald-400'
      >
        {t('Online')}
      </Badge>
    )
  }
  if (status === 'stale') {
    return (
      <Badge variant='outline' className='text-amber-600'>
        {t('Stale')}
      </Badge>
    )
  }
  return (
    <Badge variant='outline' className='text-muted-foreground'>
      {t('Offline')}
    </Badge>
  )
}

function HostDetailCard({
  detail,
  onRefresh,
  onLabelApp,
}: {
  detail: FleetHostDetail
  onRefresh?: () => void
  onLabelApp?: (app: FleetApp) => void
}) {
  const { t } = useTranslation()
  const host = detail.host
  const meta = detail.meta
  const maxUtil = Math.max(1, ...detail.gpus.map((g) => g.util))
  const [editOpen, setEditOpen] = useState(false)
  const [historyOpen, setHistoryOpen] = useState(false)

  return (
    <Card>
      <CardHeader className='flex-row items-start justify-between gap-2'>
        <div>
          <CardTitle className='flex items-center gap-2 text-base'>
            <Server className='size-4' />
            {meta.display_name || host.hostname}
            <span className='text-muted-foreground text-xs font-normal'>
              {meta.room || t('Not registered')} · {host.ip} ·{' '}
              {meta.device_model || host.os}
            </span>
          </CardTitle>
          <CardDescription>
            {meta.usage || t('Per-GPU utilization, memory and temperature')}
          </CardDescription>
        </div>
        <div className='flex shrink-0 items-center gap-1.5'>
          <Button
            size='sm'
            variant='outline'
            className='h-7 px-2 text-xs'
            onClick={() => setEditOpen(true)}
          >
            <PencilLine className='mr-1 size-3.5' />
            {t('Edit ledger')}
          </Button>
          <Button
            size='sm'
            variant='ghost'
            className='h-7 px-2 text-xs'
            onClick={() => setHistoryOpen(true)}
          >
            <History className='mr-1 size-3.5' />
            {t('History')}
          </Button>
        </div>
      </CardHeader>

      <MetaEditDialog
        open={editOpen}
        onOpenChange={setEditOpen}
        hostId={host.id}
        hostName={meta.display_name || host.hostname}
        meta={meta}
        onSaved={onRefresh}
      />
      <MetaHistoryDialog
        open={historyOpen}
        onOpenChange={setHistoryOpen}
        hostId={host.id}
        hostName={meta.display_name || host.hostname}
        onRolledBack={onRefresh}
      />
      <CardContent className='space-y-4'>
        {detail.gpus.length === 0 ? (
          <p className='text-muted-foreground rounded-md border border-dashed py-4 text-center text-xs'>
            {t('This machine has no accelerator cards (CPU-only collector)')}
          </p>
        ) : null}
        <div className='grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-8'>
          {detail.gpus.map((g) => (
            <div key={`${g.vendor}-${g.gpu_index}`} className='rounded-md border p-2'>
              <div className='text-muted-foreground flex items-center justify-between text-[11px]'>
                <span>GPU{g.gpu_index}</span>
                <Thermometer className='size-3' />
              </div>
              <div className='text-muted-foreground/70 truncate text-[9px]'>
                {g.model}
              </div>
              {g.pool ? (
                <div className='mt-0.5 truncate text-[9px]'>
                  <span
                    className={cn(
                      'rounded px-1 py-px',
                      g.pool === 'dedicated'
                        ? 'bg-sky-500/12 text-sky-600'
                        : g.pool === 'shared'
                          ? 'bg-emerald-500/12 text-emerald-600'
                          : 'bg-amber-500/12 text-amber-600'
                    )}
                  >
                    {g.pool === 'dedicated'
                      ? `专属·${g.project || '未填项目'}`
                      : g.pool === 'shared'
                        ? '弹性'
                        : '未分配'}
                  </span>
                </div>
              ) : null}
              <div className='mt-1 text-sm font-semibold tabular-nums'>
                {Math.round(g.util)}%
              </div>
              <div className='bg-muted mt-1 h-1 overflow-hidden rounded-full'>
                <div
                  className={cn('h-full', utilTone(g.util))}
                  style={{ width: `${(g.util / maxUtil) * 100}%` }}
                />
              </div>
              <div className='text-muted-foreground mt-1 text-[10px] tabular-nums'>
                {Math.round(g.mem_used / 1024)}/
                {Math.round(g.mem_total / 1024)}G · {Math.round(g.temp)}℃
              </div>
            </div>
          ))}
        </div>

        <div>
          <div className='mb-1.5 text-xs font-medium'>
            {t('Deployed apps')}
          </div>
          <table className='w-full text-sm'>
            <thead>
              <tr className='text-muted-foreground border-b text-left text-xs'>
                <th className='py-1.5 pr-3 font-medium'>{t('App')}</th>
                <th className='py-1.5 pr-3 font-medium'>{t('Serving')}</th>
                <th className='py-1.5 pr-3 font-medium'>{t('GPUs occupied')}</th>
                <th className='py-1.5 pr-3 font-medium'>{t('GPU util')}</th>
                <th className='py-1.5 pr-3 font-medium'>{t('VRAM')}</th>
                <th className='py-1.5 pr-3 font-medium'>{t('Source')}</th>
                <th className='py-1.5 font-medium' />
              </tr>
            </thead>
            <tbody>
              {detail.apps.map((app) => (
                <tr key={app.id} className='border-b last:border-0'>
                  <td className='py-1.5 pr-3 font-medium'>
                    {app.display_name || app.name}
                    {app.display_name && app.display_name !== app.name ? (
                      <span className='text-muted-foreground ml-1 text-[11px] font-normal'>
                        {app.name}
                      </span>
                    ) : null}
                  </td>
                  <td className='text-muted-foreground py-1.5 pr-3 text-xs'>
                    {app.detail || '—'}
                  </td>
                  <td className='py-1.5 pr-3 text-xs tabular-nums'>
                    {app.gpu_count > 0 ? `${app.gpu_count} 张` : '—'}
                  </td>
                  <td className='py-1.5 pr-3 text-xs tabular-nums'>
                    {Math.round(app.gpu_util)}%
                  </td>
                  <td className='text-muted-foreground py-1.5 pr-3 text-xs tabular-nums'>
                    {Math.round(app.vram_used / 1024)}G
                  </td>
                  <td className='py-1.5 pr-3'>
                    {app.estimated ? (
                      <Badge variant='outline' className='text-[10px]'>
                        {t('estimated')}
                      </Badge>
                    ) : (
                      <Badge
                        variant='outline'
                        className='border-sky-500/40 text-[10px] text-sky-600 dark:text-sky-400'
                      >
                        {t('exact')}
                      </Badge>
                    )}
                  </td>
                  <td className='py-1.5 text-right'>
                    <Button
                      size='sm'
                      variant='ghost'
                      className='h-6 px-1.5 text-[11px]'
                      onClick={() => onLabelApp?.(app)}
                    >
                      <PencilLine className='mr-1 size-3' />
                      {t('Label')}
                    </Button>
                  </td>
                </tr>
              ))}
              {detail.apps.length === 0 && (
                <tr>
                  <td
                    colSpan={7}
                    className='text-muted-foreground py-4 text-center text-xs'
                  >
                    {t('No deployed apps reported')}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </CardContent>
    </Card>
  )
}

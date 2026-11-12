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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  Boxes,
  Download,
  Layers,
  Loader2,
  PencilLine,
  Shield,
  Zap,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

import {
  batchSetFleetGpuPool,
  getFleetBilling,
  getFleetPoolCards,
  getFleetPools,
  getFleetProjects,
  getFleetSeries,
  setFleetGpuPool,
} from './api'
import { TrendChart } from './trend-chart'
import type { FleetGpuCard, FleetPoolKind } from './types'

const RANGE_OPTIONS = [1, 7, 14, 29] as const

const POOL_META: Record<
  FleetPoolKind,
  { label: string; hint: string; tone: string }
> = {
  dedicated: {
    label: '专属',
    hint: '预留给固定项目，闲置属预留成本',
    tone: 'text-sky-600 border-sky-500/40',
  },
  shared: {
    label: '弹性',
    hint: '公共池，谁都能调，闲置即浪费',
    tone: 'text-emerald-600 border-emerald-500/40',
  },
  unassigned: {
    label: '未分配',
    hint: '还没打标，不计入弹性池',
    tone: 'text-amber-600 border-amber-500/40',
  },
}

const cardKey = (c: FleetGpuCard) => `${c.host_id}:${c.gpu_index}`

function PoolBadge({ pool }: { pool: FleetPoolKind }) {
  const meta = POOL_META[pool] ?? POOL_META.unassigned
  return (
    <Badge variant='outline' className={cn('font-normal', meta.tone)}>
      {meta.label}
    </Badge>
  )
}

function PoolPicker(props: {
  pool: FleetPoolKind
  onChange: (p: FleetPoolKind) => void
}) {
  return (
    <div className='grid gap-1.5'>
      <Label>算力池</Label>
      <div className='flex gap-1.5'>
        {(['dedicated', 'shared', 'unassigned'] as FleetPoolKind[]).map((k) => (
          <Button
            key={k}
            size='sm'
            variant={props.pool === k ? 'default' : 'outline'}
            className='h-7 px-3 text-xs'
            onClick={() => props.onChange(k)}
          >
            {POOL_META[k].label}
          </Button>
        ))}
      </div>
      <p className='text-muted-foreground text-[11px]'>
        {POOL_META[props.pool].hint}
      </p>
    </div>
  )
}

function StatCard(props: {
  pool: FleetPoolKind
  cards: number
  inUse: number
  idle: number
  hours: number
  projectCount?: number
  dimmed?: boolean
}) {
  const meta = POOL_META[props.pool]
  return (
    <div
      className={cn('rounded-lg border p-3', props.dimmed && 'opacity-45')}
    >
      <div className='flex items-center justify-between'>
        <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
          {props.pool === 'dedicated' ? (
            <Shield className='size-3.5' />
          ) : props.pool === 'shared' ? (
            <Zap className='size-3.5' />
          ) : (
            <Layers className='size-3.5' />
          )}
          {meta.label}
        </div>
        <Badge variant='outline' className={cn('font-normal', meta.tone)}>
          {props.cards} 张
        </Badge>
      </div>
      <div className='mt-1 flex items-baseline gap-1'>
        <span className='text-xl font-semibold tabular-nums'>{props.inUse}</span>
        <span className='text-muted-foreground text-xs'>在用</span>
        <span className='text-muted-foreground mx-1 text-xs'>/</span>
        <span className='text-muted-foreground text-xs tabular-nums'>
          {props.idle} 闲置
        </span>
      </div>
      <div className='text-muted-foreground/70 mt-0.5 text-[11px]'>
        {props.hours.toFixed(1)} 卡时
        {props.pool === 'dedicated' && props.projectCount != null
          ? ` · ${props.projectCount} 个项目`
          : ''}
      </div>
    </div>
  )
}

// 单卡编辑
function EditCardDialog(props: {
  card: FleetGpuCard | null
  projects: string[]
  onClose: () => void
  onSaved: () => void
}) {
  const { card, projects, onClose, onSaved } = props
  const [pool, setPool] = useState<FleetPoolKind>(card?.pool ?? 'unassigned')
  const [project, setProject] = useState(card?.project ?? '')
  const [team, setTeam] = useState(card?.team ?? '')
  const [note, setNote] = useState(card?.note ?? '')
  const [saving, setSaving] = useState(false)

  const save = async () => {
    if (!card) return
    if (pool === 'dedicated' && project.trim() === '') {
      toast.error('专属卡必须填写项目名')
      return
    }
    setSaving(true)
    try {
      await setFleetGpuPool(card.host_id, card.gpu_index, {
        pool,
        project,
        team,
        note,
      })
      toast.success('归属已更新')
      onSaved()
      onClose()
    } catch {
      toast.error('更新失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog
      open={card != null}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
      title={
        card ? `设置归属 — ${card.hostname} · GPU${card.gpu_index}` : '设置归属'
      }
      description={
        card
          ? `${card.model} · 当前 ${Math.round(card.util)}% 利用率 · 该机应用：${card.host_apps.join('、') || '无'}`
          : ''
      }
      footer={
        <>
          <Button variant='outline' onClick={onClose}>
            取消
          </Button>
          <Button onClick={save} disabled={saving}>
            {saving ? <Loader2 className='mr-2 size-4 animate-spin' /> : null}
            保存
          </Button>
        </>
      }
    >
      <div className='grid gap-3 py-1'>
        <PoolPicker pool={pool} onChange={setPool} />
        {pool === 'dedicated' && (
          <>
            <div className='grid gap-1.5'>
              <Label htmlFor='pool-project'>归属项目</Label>
              <Input
                id='pool-project'
                list='pool-project-options'
                value={project}
                placeholder='例如：客服机器人'
                onChange={(e) => setProject(e.target.value)}
              />
              <datalist id='pool-project-options'>
                {projects.map((p) => (
                  <option key={p} value={p} />
                ))}
              </datalist>
            </div>
            <div className='grid gap-1.5'>
              <Label htmlFor='pool-team'>所属团队（可选）</Label>
              <Input
                id='pool-team'
                value={team}
                placeholder='例如：算法组'
                onChange={(e) => setTeam(e.target.value)}
              />
            </div>
          </>
        )}
        <div className='grid gap-1.5'>
          <Label htmlFor='pool-note'>备注（可选）</Label>
          <Input
            id='pool-note'
            value={note}
            placeholder='例如：8 月起固定给推荐系统'
            onChange={(e) => setNote(e.target.value)}
          />
        </div>
      </div>
    </Dialog>
  )
}

// 批量编辑
function BatchEditDialog(props: {
  cards: FleetGpuCard[]
  projects: string[]
  onClose: () => void
  onSaved: () => void
}) {
  const { cards, projects, onClose, onSaved } = props
  const [pool, setPool] = useState<FleetPoolKind>('dedicated')
  const [project, setProject] = useState('')
  const [team, setTeam] = useState('')
  const [note, setNote] = useState('')
  const [saving, setSaving] = useState(false)

  const hosts = useMemo(() => {
    const set = new Set(cards.map((c) => c.hostname))
    return [...set]
  }, [cards])

  const save = async () => {
    if (pool === 'dedicated' && project.trim() === '') {
      toast.error('专属卡必须填写项目名')
      return
    }
    setSaving(true)
    try {
      const n = await batchSetFleetGpuPool({
        cards: cards.map((c) => ({
          host_id: c.host_id,
          gpu_index: c.gpu_index,
        })),
        pool,
        project,
        team,
        note,
      })
      toast.success(`已更新 ${n} 张卡的归属`)
      onSaved()
      onClose()
    } catch {
      toast.error('批量设置失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog
      open={cards.length > 0}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
      title={`批量设置归属 — ${cards.length} 张卡`}
      description={`涉及 ${hosts.length} 台机器：${hosts.join('、')}`}
      footer={
        <>
          <Button variant='outline' onClick={onClose}>
            取消
          </Button>
          <Button onClick={save} disabled={saving}>
            {saving ? <Loader2 className='mr-2 size-4 animate-spin' /> : null}
            应用到 {cards.length} 张卡
          </Button>
        </>
      }
    >
      <div className='grid gap-3 py-1'>
        <PoolPicker pool={pool} onChange={setPool} />
        {pool === 'dedicated' && (
          <>
            <div className='grid gap-1.5'>
              <Label htmlFor='batch-project'>归属项目</Label>
              <Input
                id='batch-project'
                list='batch-project-options'
                value={project}
                placeholder='例如：客服机器人'
                onChange={(e) => setProject(e.target.value)}
              />
              <datalist id='batch-project-options'>
                {projects.map((p) => (
                  <option key={p} value={p} />
                ))}
              </datalist>
            </div>
            <div className='grid gap-1.5'>
              <Label htmlFor='batch-team'>所属团队（可选）</Label>
              <Input
                id='batch-team'
                value={team}
                placeholder='例如：算法组'
                onChange={(e) => setTeam(e.target.value)}
              />
            </div>
          </>
        )}
        <div className='grid gap-1.5'>
          <Label htmlFor='batch-note'>备注（可选）</Label>
          <Input
            id='batch-note'
            value={note}
            placeholder='例如：2026-11 规划调整'
            onChange={(e) => setNote(e.target.value)}
          />
        </div>
        <p className='text-muted-foreground text-[11px]'>
          会逐张写入并记录变更历史，可在每张卡的历史里回滚。
        </p>
      </div>
    </Dialog>
  )
}

export function PoolView() {
  const [rangeDays, setRangeDays] = useState<number>(7)
  const [poolFilter, setPoolFilter] = useState<'all' | FleetPoolKind>('all')
  const [editing, setEditing] = useState<FleetGpuCard | null>(null)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [batchOpen, setBatchOpen] = useState(false)
  const [month, setMonth] = useState<string>(() => {
    const d = new Date()
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
  })
  const qc = useQueryClient()

  const poolsQuery = useQuery({
    queryKey: ['fleet-pools', rangeDays],
    queryFn: () => getFleetPools(rangeDays),
    refetchInterval: 30_000,
  })

  const cardsQuery = useQuery({
    queryKey: ['fleet-pool-cards', poolFilter],
    queryFn: () =>
      getFleetPoolCards({ pool: poolFilter === 'all' ? '' : poolFilter }),
    refetchInterval: 30_000,
  })

  const projectsQuery = useQuery({
    queryKey: ['fleet-projects'],
    queryFn: getFleetProjects,
  })

  const billingQuery = useQuery({
    queryKey: ['fleet-billing', month],
    queryFn: () => getFleetBilling(month),
  })

  const seriesQuery = useQuery({
    queryKey: ['fleet-pool-series', rangeDays],
    queryFn: () => getFleetSeries(rangeDays, 'pool'),
    refetchInterval: 60_000,
  })

  const pools = poolsQuery.data?.pools
  const projectRows = poolsQuery.data?.projects ?? []
  const cards = cardsQuery.data ?? []
  const billing = billingQuery.data

  const selectedCards = useMemo(
    () => cards.filter((c) => selected.has(cardKey(c))),
    [cards, selected]
  )
  const allSelected = cards.length > 0 && selectedCards.length === cards.length

  // 筛选到具体池时，左下的「按项目」换成「按机器」，并只统计该池的卡
  const hostRows = useMemo(() => {
    const m = new Map<
      string,
      { host: string; cards: number; utilSum: number; mem: number; memTotal: number }
    >()
    for (const c of cards) {
      const r = m.get(c.hostname) ?? {
        host: c.hostname,
        cards: 0,
        utilSum: 0,
        mem: 0,
        memTotal: 0,
      }
      r.cards += 1
      r.utilSum += c.util
      r.mem += c.mem_used
      r.memTotal += c.mem_total
      m.set(c.hostname, r)
    }
    return [...m.values()].sort((a, b) => b.cards - a.cards)
  }, [cards])

  // 分账表跟着池筛选走：专属只看有专属卡的项目，弹性只看有分摊的项目
  const billingRows = useMemo(() => {
    const rows = billing?.rows ?? []
    if (poolFilter === 'all') return rows
    if (poolFilter === 'dedicated') return rows.filter((r) => r.dedicated_cards > 0)
    if (poolFilter === 'shared') return rows.filter((r) => r.shared_hours > 0)
    return []
  }, [billing, poolFilter])

  // 选了某个池，趋势图只画这一条，和上面的筛选保持一致
  const chartSeries = useMemo(() => {
    const s = seriesQuery.data
    if (!s || poolFilter === 'all') return s
    const label =
      poolFilter === 'dedicated' ? '专属' : poolFilter === 'shared' ? '弹性' : '未分配'
    return { ...s, keys: [label] }
  }, [seriesQuery.data, poolFilter])

  const toggle = (c: FleetGpuCard) => {
    const k = cardKey(c)
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(k)) next.delete(k)
      else next.add(k)
      return next
    })
  }

  const toggleAll = () => {
    if (allSelected) {
      setSelected(new Set())
    } else {
      setSelected(new Set(cards.map(cardKey)))
    }
  }

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['fleet-pools'] })
    void qc.invalidateQueries({ queryKey: ['fleet-pool-cards'] })
    void qc.invalidateQueries({ queryKey: ['fleet-billing'] })
    void qc.invalidateQueries({ queryKey: ['fleet-summary'] })
    void qc.invalidateQueries({ queryKey: ['fleet-projects'] })
    setSelected(new Set())
  }

  const exportCsv = () => {
    if (!billing) return
    const head = '项目,专属卡数,预留卡时,有效卡时,弹性分摊卡时,合计卡时'
    const body = billing.rows
      .map((r) =>
        [
          r.project,
          r.dedicated_cards,
          r.reserved_hours.toFixed(1),
          r.effective_hours.toFixed(1),
          r.shared_hours.toFixed(1),
          r.total_hours.toFixed(1),
        ].join(',')
      )
      .join('\n')
    const blob = new Blob([`\uFEFF${head}\n${body}`], {
      type: 'text/csv;charset=utf-8',
    })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `fleet-billing-${billing.month}.csv`
    a.click()
    URL.revokeObjectURL(url)
  }

  // 选某个池时只展示这个池的卡片与总数，不再把另外两个摆出来
  const visiblePools: FleetPoolKind[] = useMemo(
    () =>
      poolFilter === 'all'
        ? (['dedicated', 'shared', 'unassigned'] as FleetPoolKind[])
        : [poolFilter],
    [poolFilter]
  )

  const scopeTotals = useMemo(() => {
    const list = visiblePools
      .map((k) => pools?.[k])
      .filter((p): p is NonNullable<typeof p> => Boolean(p))
    return {
      cards: list.reduce((n, p) => n + p.cards, 0),
      hours: list.reduce((n, p) => n + p.gpu_hours, 0),
    }
  }, [visiblePools, pools])

  if (poolsQuery.isLoading) {
    return (
      <div className='space-y-3'>
        <Skeleton className='h-24 w-full' />
        <Skeleton className='h-64 w-full' />
      </div>
    )
  }

  return (
    <div className='space-y-3 sm:space-y-4'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
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
              variant={poolFilter === key ? 'default' : 'ghost'}
              className='h-7 rounded-md px-3 text-xs'
              onClick={() => setPoolFilter(key)}
            >
              {label}
            </Button>
          ))}
        </div>
        <div className='flex items-center gap-2'>
          <span className='text-muted-foreground text-xs tabular-nums'>
            共 {scopeTotals.cards} 张卡 · {scopeTotals.hours.toFixed(1)} 卡时
            {poolFilter !== 'all' ? `（仅${POOL_META[poolFilter].label}）` : ''}
          </span>
          <div className='bg-muted/60 inline-flex overflow-hidden rounded-lg border p-0.5'>
            {RANGE_OPTIONS.map((days) => (
              <Button
                key={days}
                size='sm'
                variant={rangeDays === days ? 'default' : 'ghost'}
                className='h-7 rounded-md px-3 text-xs'
                onClick={() => setRangeDays(days)}
              >
                {days} 天
              </Button>
            ))}
          </div>
        </div>
      </div>

      <div className='grid grid-cols-1 gap-3 sm:grid-cols-3'>
        {visiblePools.map((k) => {
          const st = pools?.[k]
          return (
            <StatCard
              key={k}
              pool={k}
              cards={st?.cards ?? 0}
              inUse={st?.in_use ?? 0}
              idle={st?.idle ?? 0}
              hours={st?.gpu_hours ?? 0}
              projectCount={st?.projects ?? 0}
              dimmed={false}
            />
          )
        })}
      </div>

      <Card>
        <CardHeader>
          <CardTitle className='flex items-center gap-2 text-base'>
            <Activity className='size-4' />
            算力池趋势
          </CardTitle>
          <CardDescription>
            {poolFilter === 'all'
              ? '三个池的 GPU 利用率之和按池叠加；单位：利用率之和（可点图例隐藏）'
              : `${POOL_META[poolFilter].label}池的 GPU 平均利用率（1/7/14/29 天联动）`}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <TrendChart
            series={chartSeries}
            groupBy='pool'
            rangeDays={rangeDays}
          />
        </CardContent>
      </Card>

      <div className='grid gap-3 lg:grid-cols-2'>
        <Card>
          <CardHeader>
            <CardTitle className='flex items-center gap-2 text-base'>
              {poolFilter === 'all' || poolFilter === 'dedicated' ? (
                <>
                  <Shield className='size-4' />
                  专属池 · 按项目
                </>
              ) : (
                <>
                  <Layers className='size-4' />
                  {POOL_META[poolFilter].label}池 · 按机器
                </>
              )}
            </CardTitle>
            <CardDescription>
              {poolFilter === 'all' || poolFilter === 'dedicated'
                ? '预留卡时按卡数折算，有效卡时按利用率折算'
                : '当前筛选下每台机器有几张卡、平均利用率与显存占用'}
            </CardDescription>
          </CardHeader>
          <CardContent>
            {poolFilter === 'all' || poolFilter === 'dedicated' ? (
              projectRows.length === 0 ? (
                <p className='text-muted-foreground rounded-md border border-dashed py-6 text-center text-xs'>
                  还没有专属卡，先从下面的列表里给卡打标
                </p>
              ) : (
                <table className='w-full text-sm'>
                  <thead>
                    <tr className='text-muted-foreground border-b text-left text-xs'>
                      <th className='py-1.5 pr-3 font-medium'>项目</th>
                      <th className='py-1.5 pr-3 text-right font-medium'>卡数</th>
                      <th className='py-1.5 pr-3 text-right font-medium'>卡时</th>
                      <th className='py-1.5 text-right font-medium'>平均利用率</th>
                    </tr>
                  </thead>
                  <tbody>
                    {projectRows.map((p) => (
                      <tr key={p.project} className='border-b last:border-0'>
                        <td className='py-1.5 pr-3 font-medium'>{p.project}</td>
                        <td className='py-1.5 pr-3 text-right tabular-nums'>
                          {p.cards}
                        </td>
                        <td className='py-1.5 pr-3 text-right tabular-nums'>
                          {p.gpu_hours.toFixed(1)}
                        </td>
                        <td className='text-muted-foreground py-1.5 text-right tabular-nums'>
                          {p.avg_util.toFixed(0)}%
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )
            ) : hostRows.length === 0 ? (
              <p className='text-muted-foreground rounded-md border border-dashed py-6 text-center text-xs'>
                这个池里还没有卡
              </p>
            ) : (
              <table className='w-full text-sm'>
                <thead>
                  <tr className='text-muted-foreground border-b text-left text-xs'>
                    <th className='py-1.5 pr-3 font-medium'>主机</th>
                    <th className='py-1.5 pr-3 text-right font-medium'>卡数</th>
                    <th className='py-1.5 pr-3 text-right font-medium'>平均利用率</th>
                    <th className='py-1.5 text-right font-medium'>显存</th>
                  </tr>
                </thead>
                <tbody>
                  {hostRows.map((h) => (
                    <tr key={h.host} className='border-b last:border-0'>
                      <td className='py-1.5 pr-3 font-medium'>{h.host}</td>
                      <td className='py-1.5 pr-3 text-right tabular-nums'>
                        {h.cards}
                      </td>
                      <td className='py-1.5 pr-3 text-right tabular-nums'>
                        {(h.utilSum / Math.max(1, h.cards)).toFixed(0)}%
                      </td>
                      <td className='text-muted-foreground py-1.5 text-right text-xs tabular-nums'>
                        {Math.round(h.mem / 1024)}/
                        {Math.round(h.memTotal / 1024)}G
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className='flex-row items-start justify-between'>
            <div>
              <CardTitle className='flex items-center gap-2 text-base'>
                <Boxes className='size-4' />
                分账 · 按项目
              </CardTitle>
              <CardDescription>专属卡按归属计，弹性卡按应用分摊</CardDescription>
            </div>
            <div className='flex items-center gap-1.5'>
              <Input
                type='month'
                value={month}
                className='h-7 w-36 text-xs'
                onChange={(e) => setMonth(e.target.value)}
              />
              <Button
                size='sm'
                variant='outline'
                className='h-7 px-2 text-xs'
                onClick={exportCsv}
              >
                <Download className='size-3.5' />
              </Button>
            </div>
          </CardHeader>
          <CardContent className='overflow-x-auto'>
            {billingQuery.isLoading ? (
              <Skeleton className='h-32 w-full' />
            ) : poolFilter === 'unassigned' ? (
              <p className='text-muted-foreground rounded-md border border-dashed py-6 text-center text-xs'>
                未分配的卡不参与分账，先打标再统计
              </p>
            ) : billingRows.length === 0 ? (
              <p className='text-muted-foreground rounded-md border border-dashed py-6 text-center text-xs'>
                这个月这个口径下没有用量数据
              </p>
            ) : (
              <table className='w-full text-sm'>
                <thead>
                  <tr className='text-muted-foreground border-b text-left text-xs'>
                    <th className='py-1.5 pr-3 font-medium'>项目</th>
                    {poolFilter !== 'shared' && (
                      <>
                        <th className='py-1.5 pr-3 text-right font-medium'>专属卡</th>
                        <th className='py-1.5 pr-3 text-right font-medium'>预留(h)</th>
                        <th className='py-1.5 pr-3 text-right font-medium'>有效(h)</th>
                      </>
                    )}
                    {poolFilter !== 'dedicated' && (
                      <th className='py-1.5 pr-3 text-right font-medium'>弹性(h)</th>
                    )}
                    <th className='py-1.5 text-right font-medium'>合计(h)</th>
                  </tr>
                </thead>
                <tbody>
                  {billingRows.map((r) => (
                    <tr key={r.project} className='border-b last:border-0'>
                      <td className='py-1.5 pr-3 font-medium'>{r.project}</td>
                      {poolFilter !== 'shared' && (
                        <>
                          <td className='py-1.5 pr-3 text-right tabular-nums'>
                            {r.dedicated_cards}
                          </td>
                          <td className='text-muted-foreground py-1.5 pr-3 text-right tabular-nums'>
                            {r.reserved_hours.toFixed(1)}
                          </td>
                          <td className='py-1.5 pr-3 text-right tabular-nums'>
                            {r.effective_hours.toFixed(1)}
                          </td>
                        </>
                      )}
                      {poolFilter !== 'dedicated' && (
                        <td className='py-1.5 pr-3 text-right tabular-nums'>
                          {r.shared_hours.toFixed(1)}
                        </td>
                      )}
                      <td className='py-1.5 text-right font-medium tabular-nums'>
                        {r.total_hours.toFixed(1)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className='flex items-center gap-2 text-base'>
            <Layers className='size-4' />
            显卡归属
          </CardTitle>
          <CardDescription>
            逐卡打标：勾选多张可批量设置；专属卡填项目，弹性卡留空
          </CardDescription>
        </CardHeader>
        <CardContent className='overflow-x-auto'>
          {selectedCards.length > 0 ? (
            <div className='bg-muted/50 mb-2 flex flex-wrap items-center gap-2 rounded-md border px-3 py-2'>
              <span className='text-xs font-medium'>
                已选 {selectedCards.length} 张卡（{new Set(selectedCards.map((c) => c.hostname)).size} 台机器）
              </span>
              <Button
                size='sm'
                className='h-7 px-3 text-xs'
                onClick={() => setBatchOpen(true)}
              >
                批量设置归属
              </Button>
              <Button
                size='sm'
                variant='ghost'
                className='h-7 px-2 text-xs'
                onClick={() => setSelected(new Set())}
              >
                取消选择
              </Button>
            </div>
          ) : null}
          <table className='w-full text-sm'>
            <thead>
              <tr className='text-muted-foreground border-b text-left text-xs'>
                <th className='w-8 py-1.5 pr-2'>
                  <input
                    type='checkbox'
                    className='accent-primary size-4 cursor-pointer'
                    checked={allSelected}
                    onChange={toggleAll}
                    aria-label='全选'
                  />
                </th>
                <th className='py-1.5 pr-3 font-medium'>主机</th>
                <th className='py-1.5 pr-3 font-medium'>卡</th>
                <th className='py-1.5 pr-3 font-medium'>型号</th>
                <th className='py-1.5 pr-3 font-medium'>利用率</th>
                <th className='py-1.5 pr-3 font-medium'>显存</th>
                <th className='py-1.5 pr-3 font-medium'>池</th>
                <th className='py-1.5 pr-3 font-medium'>项目</th>
                <th className='py-1.5 pr-3 font-medium'>实际应用</th>
                <th className='py-1.5 font-medium' />
              </tr>
            </thead>
            <tbody>
              {cards.map((c) => {
                const k = cardKey(c)
                const checked = selected.has(k)
                return (
                  <tr
                    key={k}
                    className={cn(
                      'border-b last:border-0',
                      checked && 'bg-muted/40'
                    )}
                  >
                    <td className='py-1.5 pr-2'>
                      <input
                        type='checkbox'
                        className='accent-primary size-4 cursor-pointer'
                        checked={checked}
                        onChange={() => toggle(c)}
                        aria-label={`选择 ${c.hostname} GPU${c.gpu_index}`}
                      />
                    </td>
                    <td className='py-1.5 pr-3 font-medium'>{c.hostname}</td>
                    <td className='py-1.5 pr-3 tabular-nums'>GPU{c.gpu_index}</td>
                    <td className='text-muted-foreground truncate py-1.5 pr-3 text-xs'>
                      {c.model}
                    </td>
                    <td className='py-1.5 pr-3 tabular-nums'>
                      {Math.round(c.util)}%
                    </td>
                    <td className='text-muted-foreground py-1.5 pr-3 text-xs tabular-nums'>
                      {Math.round(c.mem_used / 1024)}/
                      {Math.round(c.mem_total / 1024)}G
                    </td>
                    <td className='py-1.5 pr-3'>
                      <PoolBadge pool={c.pool} />
                    </td>
                    <td className='py-1.5 pr-3'>
                      {c.project || (
                        <span className='text-muted-foreground/60'>—</span>
                      )}
                    </td>
                    <td className='text-muted-foreground max-w-48 truncate py-1.5 pr-3 text-xs'>
                      {c.host_apps.slice(0, 3).join('、') || '—'}
                    </td>
                    <td className='py-1.5 text-right'>
                      <Button
                        size='sm'
                        variant='ghost'
                        className='h-6 px-2 text-xs'
                        onClick={() => setEditing(c)}
                      >
                        <PencilLine className='mr-1 size-3' />
                        编辑
                      </Button>
                    </td>
                  </tr>
                )
              })}
              {cards.length === 0 && (
                <tr>
                  <td
                    colSpan={10}
                    className='text-muted-foreground py-6 text-center text-xs'
                  >
                    这个筛选条件下没有卡
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </CardContent>
      </Card>

      {editing ? (
        <EditCardDialog
          card={editing}
          projects={projectsQuery.data ?? []}
          onClose={() => setEditing(null)}
          onSaved={refresh}
        />
      ) : null}

      {batchOpen ? (
        <BatchEditDialog
          cards={selectedCards}
          projects={projectsQuery.data ?? []}
          onClose={() => setBatchOpen(false)}
          onSaved={refresh}
        />
      ) : null}
    </div>
  )
}

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
  getFleetBilling,
  getFleetPoolCards,
  getFleetPools,
  getFleetProjects,
  setFleetGpuPool,
} from './api'
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

function PoolBadge({ pool }: { pool: FleetPoolKind }) {
  const meta = POOL_META[pool] ?? POOL_META.unassigned
  return (
    <Badge variant='outline' className={cn('font-normal', meta.tone)}>
      {meta.label}
    </Badge>
  )
}

function StatCard(props: {
  pool: FleetPoolKind
  cards: number
  inUse: number
  idle: number
  hours: number
  projectCount?: number
}) {
  const meta = POOL_META[props.pool]
  return (
    <div className='rounded-lg border p-3'>
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
        <span className='text-xl font-semibold tabular-nums'>
          {props.inUse}
        </span>
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
        <div className='hidden'>{meta.hint}</div>
      </div>
    </div>
  )
}

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
        card
          ? `设置归属 — ${card.hostname} · GPU${card.gpu_index}`
          : '设置归属'
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
        <div className='grid gap-1.5'>
          <Label>算力池</Label>
          <div className='flex gap-1.5'>
            {(['dedicated', 'shared', 'unassigned'] as FleetPoolKind[]).map(
              (k) => (
                <Button
                  key={k}
                  size='sm'
                  variant={pool === k ? 'default' : 'outline'}
                  className='h-7 px-3 text-xs'
                  onClick={() => setPool(k)}
                >
                  {POOL_META[k].label}
                </Button>
              )
            )}
          </div>
          <p className='text-muted-foreground text-[11px]'>
            {POOL_META[pool].hint}
          </p>
        </div>
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

export function PoolView() {
  const [rangeDays, setRangeDays] = useState<number>(7)
  const [poolFilter, setPoolFilter] = useState<'all' | FleetPoolKind>('all')
  const [editing, setEditing] = useState<FleetGpuCard | null>(null)
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
      getFleetPoolCards({
        pool: poolFilter === 'all' ? '' : poolFilter,
      }),
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

  const pools = poolsQuery.data?.pools
  const projectRows = poolsQuery.data?.projects ?? []
  const cards = cardsQuery.data ?? []
  const billing = billingQuery.data

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['fleet-pools'] })
    void qc.invalidateQueries({ queryKey: ['fleet-pool-cards'] })
    void qc.invalidateQueries({ queryKey: ['fleet-billing'] })
    void qc.invalidateQueries({ queryKey: ['fleet-summary'] })
    void qc.invalidateQueries({ queryKey: ['fleet-projects'] })
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

  const totals = useMemo(() => {
    const list = Object.values(pools ?? {})
    return {
      cards: list.reduce((n, p) => n + p.cards, 0),
      hours: list.reduce((n, p) => n + p.gpu_hours, 0),
    }
  }, [pools])

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
      {/* 顶部：池筛选 + 时间范围 */}
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
            共 {totals.cards} 张卡 · {totals.hours.toFixed(1)} 卡时
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

      {/* 三个池 */}
      <div className='grid grid-cols-1 gap-3 sm:grid-cols-3'>
        {(['dedicated', 'shared', 'unassigned'] as FleetPoolKind[]).map((k) => {
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
            />
          )
        })}
      </div>

      <div className='grid gap-3 lg:grid-cols-2'>
        {/* 专属池按项目 */}
        <Card>
          <CardHeader>
            <CardTitle className='flex items-center gap-2 text-base'>
              <Shield className='size-4' />
              专属池 · 按项目
            </CardTitle>
            <CardDescription>
              预留卡时按卡数折算，有效卡时按利用率折算
            </CardDescription>
          </CardHeader>
          <CardContent>
            {projectRows.length === 0 ? (
              <p className='text-muted-foreground rounded-md border border-dashed py-6 text-center text-xs'>
                还没有专属卡，先从下面的列表里给卡打标
              </p>
            ) : (
              <table className='w-full text-sm'>
                <thead>
                  <tr className='text-muted-foreground border-b text-left text-xs'>
                    <th className='py-1.5 pr-3 font-medium'>项目</th>
                    <th className='py-1.5 pr-3 text-right font-medium'>
                      卡数
                    </th>
                    <th className='py-1.5 pr-3 text-right font-medium'>
                      卡时
                    </th>
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
            )}
          </CardContent>
        </Card>

        {/* 分账 */}
        <Card>
          <CardHeader className='flex-row items-start justify-between'>
            <div>
              <CardTitle className='flex items-center gap-2 text-base'>
                <Boxes className='size-4' />
                分账 · 按项目
              </CardTitle>
              <CardDescription>
                专属卡按归属计，弹性卡按应用分摊
              </CardDescription>
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
            ) : (billing?.rows?.length ?? 0) === 0 ? (
              <p className='text-muted-foreground rounded-md border border-dashed py-6 text-center text-xs'>
                这个月没有用量数据
              </p>
            ) : (
              <table className='w-full text-sm'>
                <thead>
                  <tr className='text-muted-foreground border-b text-left text-xs'>
                    <th className='py-1.5 pr-3 font-medium'>项目</th>
                    <th className='py-1.5 pr-3 text-right font-medium'>
                      专属卡
                    </th>
                    <th className='py-1.5 pr-3 text-right font-medium'>
                      预留(h)
                    </th>
                    <th className='py-1.5 pr-3 text-right font-medium'>
                      有效(h)
                    </th>
                    <th className='py-1.5 pr-3 text-right font-medium'>
                      弹性(h)
                    </th>
                    <th className='py-1.5 text-right font-medium'>合计(h)</th>
                  </tr>
                </thead>
                <tbody>
                  {billing?.rows.map((r) => (
                    <tr key={r.project} className='border-b last:border-0'>
                      <td className='py-1.5 pr-3 font-medium'>{r.project}</td>
                      <td className='py-1.5 pr-3 text-right tabular-nums'>
                        {r.dedicated_cards}
                      </td>
                      <td className='text-muted-foreground py-1.5 pr-3 text-right tabular-nums'>
                        {r.reserved_hours.toFixed(1)}
                      </td>
                      <td className='py-1.5 pr-3 text-right tabular-nums'>
                        {r.effective_hours.toFixed(1)}
                      </td>
                      <td className='py-1.5 pr-3 text-right tabular-nums'>
                        {r.shared_hours.toFixed(1)}
                      </td>
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

      {/* 卡列表 */}
      <Card>
        <CardHeader>
          <CardTitle className='flex items-center gap-2 text-base'>
            <Layers className='size-4' />
            显卡归属
          </CardTitle>
          <CardDescription>
            逐卡打标：专属卡填项目，弹性卡留空；未分配的卡不计入弹性池
          </CardDescription>
        </CardHeader>
        <CardContent className='overflow-x-auto'>
          <table className='w-full text-sm'>
            <thead>
              <tr className='text-muted-foreground border-b text-left text-xs'>
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
              {cards.map((c) => (
                <tr
                  key={`${c.host_id}-${c.gpu_index}`}
                  className='border-b last:border-0'
                >
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
              ))}
              {cards.length === 0 && (
                <tr>
                  <td
                    colSpan={9}
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
    </div>
  )
}

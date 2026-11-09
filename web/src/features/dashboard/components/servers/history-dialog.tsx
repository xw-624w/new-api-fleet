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
import { History, Undo2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'

import {
  getFleetMetaHistory,
  rollbackFleetMeta,
  type FleetMetaHistoryRow,
} from './api'

interface MetaHistoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  hostId: number
  hostName: string
  onRolledBack?: () => void
}

function parseDiff(row: FleetMetaHistoryRow): Array<{
  field: string
  labelKey: string
  old: string
  next: string
}> {
  try {
    const raw = JSON.parse(row.diff_json) as Record<
      string,
      { old?: string; new?: string }
    >
    return Object.entries(raw).map(([field, v]) => ({
      field,
      labelKey: FIELD_LABEL_KEYS[field] ?? field,
      old: String(v?.old ?? ''),
      next: String(v?.new ?? ''),
    }))
  } catch {
    return []
  }
}

// 字段名 → 展示用文案的 key（复用台账表单里的标签，保证中英文一致）
const FIELD_LABEL_KEYS: Record<string, string> = {
  display_name: 'Display name',
  room: 'Room',
  rack: 'Rack / U position',
  device_model: 'Device model',
  form_factor: 'Form factor',
  asset_no: 'Asset number',
  serial_no: 'Serial number',
  bmc_ip: 'BMC IP',
  owner: 'Owner',
  usage: 'Usage',
  env: 'Environment',
  purchased_at: 'Purchased at',
  warranty_until: 'Warranty until',
}

export function MetaHistoryDialog({
  open,
  onOpenChange,
  hostId,
  hostName,
  onRolledBack,
}: MetaHistoryDialogProps) {
  const { t } = useTranslation()
  const [busyId, setBusyId] = useState<number | null>(null)

  const history = useQuery({
    queryKey: ['fleet-meta-history', hostId],
    queryFn: () => getFleetMetaHistory(hostId),
    enabled: open,
  })

  const handleRollback = async (id: number) => {
    setBusyId(id)
    try {
      await rollbackFleetMeta(hostId, id)
      toast.success(t('Rolled back'))
      await history.refetch()
      onRolledBack?.()
    } catch {
      toast.error(t('Rollback failed'))
    } finally {
      setBusyId(null)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Ledger history — {{host}}', { host: hostName })}
      description={t('Every change is recorded and can be rolled back.')}
      contentHeight='min(60vh, 520px)'
      footer={
        <Button variant='outline' onClick={() => onOpenChange(false)}>
          {t('Close')}
        </Button>
      }
    >
      <div className='space-y-3 py-1'>
        {history.isLoading && <Skeleton className='h-24 w-full' />}
        {!history.isLoading && (history.data ?? []).length === 0 && (
          <p className='text-muted-foreground py-8 text-center text-sm'>
            {t('No changes recorded yet')}
          </p>
        )}
        {(history.data ?? []).map((row) => {
          const diff = parseDiff(row)
          return (
            <div key={row.id} className='rounded-lg border p-3'>
              <div className='flex items-center justify-between gap-2'>
                <div className='flex items-center gap-2 text-xs'>
                  <History className='text-muted-foreground size-3.5' />
                  <span className='font-medium'>
                    {new Date(row.changed_at * 1000).toLocaleString()}
                  </span>
                  <span className='text-muted-foreground'>{row.changed_by}</span>
                  <Badge variant='outline' className='text-[10px]'>
                    {row.source}
                  </Badge>
                </div>
                <Button
                  size='sm'
                  variant='ghost'
                  className='h-7 px-2 text-xs'
                  disabled={busyId === row.id}
                  onClick={() => handleRollback(row.id)}
                >
                  <Undo2 className='mr-1 size-3.5' />
                  {t('Roll back')}
                </Button>
              </div>
              <div className='mt-2 space-y-1'>
                {diff.map((d) => (
                  <div key={d.field} className='text-muted-foreground text-xs'>
                    <span className='text-foreground font-medium'>
                      {t(d.labelKey)}
                    </span>
                    {' : '}
                    <span className='line-through opacity-60'>{d.old || '—'}</span>
                    {' → '}
                    <span className='text-foreground'>{d.next || '—'}</span>
                  </div>
                ))}
                {diff.length === 0 && (
                  <div className='text-muted-foreground text-xs'>
                    {t('No field diff recorded')}
                  </div>
                )}
              </div>
            </div>
          )
        })}
      </div>
    </Dialog>
  )
}

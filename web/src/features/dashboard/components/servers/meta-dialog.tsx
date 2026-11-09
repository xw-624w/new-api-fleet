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
import { Loader2, Save } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'

import { updateFleetMeta, type FleetMetaFields } from './api'

interface MetaEditDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  hostId: number
  hostName: string
  meta: Partial<FleetMetaFields>
  onSaved?: () => void
}

const FIELDS: Array<{
  key: keyof FleetMetaFields
  labelKey: string
  placeholder?: string
}> = [
  { key: 'display_name', labelKey: 'Display name', placeholder: 'gpu-srv-01' },
  { key: 'room', labelKey: 'Room', placeholder: '三期机房' },
  { key: 'rack', labelKey: 'Rack / U position', placeholder: 'A-12 / U20' },
  { key: 'device_model', labelKey: 'Device model', placeholder: '联想 问天 WA5480 G3' },
  { key: 'form_factor', labelKey: 'Form factor', placeholder: '4U机架式' },
  { key: 'asset_no', labelKey: 'Asset number' },
  { key: 'serial_no', labelKey: 'Serial number' },
  { key: 'bmc_ip', labelKey: 'BMC IP' },
  { key: 'owner', labelKey: 'Owner', placeholder: 'AI 平台组' },
  { key: 'env', labelKey: 'Environment', placeholder: 'prod / test / spare' },
  { key: 'purchased_at', labelKey: 'Purchased at', placeholder: '2026-02-06' },
  { key: 'warranty_until', labelKey: 'Warranty until', placeholder: '2029-02-06' },
]

export function MetaEditDialog({
  open,
  onOpenChange,
  hostId,
  hostName,
  meta,
  onSaved,
}: MetaEditDialogProps) {
  const { t } = useTranslation()
  const [form, setForm] = useState<Record<string, string>>({})
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    const next: Record<string, string> = {}
    for (const f of FIELDS) next[f.key] = String(meta[f.key] ?? '')
    next.usage = String(meta.usage ?? '')
    setForm(next)
  }, [open, meta])

  const handleSave = async () => {
    const patch: Record<string, string> = {}
    for (const [key, value] of Object.entries(form)) {
      const original = String(
        key === 'usage' ? (meta.usage ?? '') : (meta[key as keyof FleetMetaFields] ?? '')
      )
      if (value !== original) patch[key] = value.trim()
    }
    if (Object.keys(patch).length === 0) {
      toast.info(t('Nothing changed'))
      onOpenChange(false)
      return
    }
    setSaving(true)
    try {
      await updateFleetMeta(hostId, patch)
      toast.success(t('Ledger updated'))
      onSaved?.()
      onOpenChange(false)
    } catch {
      toast.error(t('Failed to update ledger'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Edit ledger — {{host}}', { host: hostName })}
      description={t(
        'Ledger fields are maintained by humans; agents never overwrite them.'
      )}
      contentHeight='min(62vh, 560px)'
      contentClassName='sm:max-w-2xl'
      footer={
        <>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleSave} disabled={saving}>
            {saving ? (
              <Loader2 className='mr-2 size-4 animate-spin' />
            ) : (
              <Save className='mr-2 size-4' />
            )}
            {t('Save')}
          </Button>
        </>
      }
    >
      <div className='grid gap-3 py-1 sm:grid-cols-2'>
        {FIELDS.map((f) => (
          <div key={f.key} className='grid gap-1.5'>
            <Label htmlFor={`meta-${f.key}`}>{t(f.labelKey)}</Label>
            <Input
              id={`meta-${f.key}`}
              value={form[f.key] ?? ''}
              placeholder={f.placeholder}
              onChange={(e) =>
                setForm((prev) => ({ ...prev, [f.key]: e.target.value }))
              }
            />
          </div>
        ))}
        <div className='grid gap-1.5 sm:col-span-2'>
          <Label htmlFor='meta-usage'>{t('Usage')}</Label>
          <Textarea
            id='meta-usage'
            rows={2}
            value={form.usage ?? ''}
            placeholder={t('What runs on this machine')}
            onChange={(e) =>
              setForm((prev) => ({ ...prev, usage: e.target.value }))
            }
          />
        </div>
      </div>
    </Dialog>
  )
}

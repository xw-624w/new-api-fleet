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

import { upsertFleetAppAlias } from './api'

export interface AliasTarget {
  /** 建议匹配方式：优先服务名，其次镜像 */
  matchType: 'service' | 'image' | 'name'
  matchValue: string
  /** 当前展示名，用于预填 */
  currentDisplay?: string
  /** 探测到的模型名，作为提示 */
  detail?: string
}

interface AliasDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  target: AliasTarget | null
  onSaved?: () => void
}

const MATCH_TYPES = ['service', 'image', 'name'] as const

export function AliasDialog({
  open,
  onOpenChange,
  target,
  onSaved,
}: AliasDialogProps) {
  const { t } = useTranslation()
  const [matchType, setMatchType] = useState<string>('service')
  const [matchValue, setMatchValue] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [owner, setOwner] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open || !target) return
    setMatchType(target.matchType)
    setMatchValue(target.matchValue)
    setDisplayName(target.currentDisplay || target.detail || '')
    setOwner('')
  }, [open, target])

  const handleSave = async () => {
    if (!matchValue.trim() || !displayName.trim()) {
      toast.error(t('Name cannot be empty'))
      return
    }
    setSaving(true)
    try {
      await upsertFleetAppAlias({
        match_type: matchType,
        match_value: matchValue.trim(),
        display_name: displayName.trim(),
        owner: owner.trim(),
      })
      toast.success(t('Saved'))
      onSaved?.()
      onOpenChange(false)
    } catch {
      toast.error(t('Save failed'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Label this app')}
      description={t(
        'Give the container a name you recognise. It applies to every machine running the same service/image.'
      )}
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
      <div className='grid gap-3 py-1'>
        <div className='grid gap-1.5'>
          <Label>{t('Match by')}</Label>
          <div className='flex gap-1.5'>
            {MATCH_TYPES.map((mt) => (
              <Button
                key={mt}
                size='sm'
                type='button'
                variant={matchType === mt ? 'default' : 'outline'}
                className='h-7 text-xs'
                onClick={() => setMatchType(mt)}
              >
                {t(mt)}
              </Button>
            ))}
          </div>
        </div>

        <div className='grid gap-1.5'>
          <Label htmlFor='alias-value'>
            {matchType === 'service'
              ? t('Compose service')
              : matchType === 'image'
                ? t('Image')
                : t('App name')}
          </Label>
          <Input
            id='alias-value'
            value={matchValue}
            onChange={(e) => setMatchValue(e.target.value)}
          />
        </div>

        <div className='grid gap-1.5'>
          <Label htmlFor='alias-name'>{t('Display name')}</Label>
          <Input
            id='alias-name'
            value={displayName}
            placeholder={t('e.g. 制度问答-主模型')}
            onChange={(e) => setDisplayName(e.target.value)}
          />
          {target?.detail ? (
            <p className='text-muted-foreground text-[11px]'>
              {t('Detected: {{detail}}', { detail: target.detail })}
            </p>
          ) : null}
        </div>

        <div className='grid gap-1.5'>
          <Label htmlFor='alias-owner'>{t('Owner')}</Label>
          <Input
            id='alias-owner'
            value={owner}
            placeholder={t('optional')}
            onChange={(e) => setOwner(e.target.value)}
          />
        </div>
      </div>
    </Dialog>
  )
}

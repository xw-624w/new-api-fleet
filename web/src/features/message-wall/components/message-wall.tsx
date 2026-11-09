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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ChevronLeft,
  ChevronRight,
  Loader2,
  MessageSquare,
  Send,
  Trash2,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Textarea } from '@/components/ui/textarea'
import { ROLE } from '@/lib/roles'
import {
  getServerErrorMessage,
  requireServerSuccess,
} from '@/lib/server-error-message'
import { formatDate } from '@/lib/time'
import { useAuthStore } from '@/stores/auth-store'

import { addMessageWall, getMessageWall, removeMessageWall } from '../api'
import type { MessageWallItem } from '../types'

const PAGE_SIZE = 10
const MAX_CONTENT_LENGTH = 500

export function MessageWall() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const user = useAuthStore((state) => state.auth.user)

  const [content, setContent] = useState('')
  const [page, setPage] = useState(1)

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['message-wall', page],
    queryFn: async () =>
      requireServerSuccess(await getMessageWall(page, PAGE_SIZE)),
    staleTime: 60 * 1000,
  })

  const messages: MessageWallItem[] = data?.data?.messages ?? []
  const total = data?.data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const addMutation = useMutation({
    mutationFn: async (value: string) =>
      requireServerSuccess(await addMessageWall(value)),
    onSuccess: () => {
      setContent('')
      setPage(1)
      queryClient.invalidateQueries({ queryKey: ['message-wall'] })
      toast.success(t('Message posted'))
    },
    onError: (err) => toast.error(getServerErrorMessage(err)),
  })

  const deleteMutation = useMutation({
    mutationFn: async (id: number) =>
      requireServerSuccess(await removeMessageWall(id)),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['message-wall'] })
      toast.success(t('Message deleted'))
    },
    onError: (err) => toast.error(getServerErrorMessage(err)),
  })

  const handleSubmit = () => {
    const value = content.trim()
    if (!value) {
      toast.error(t('Message content cannot be empty'))
      return
    }
    addMutation.mutate(value)
  }

  // The author may withdraw their own message; administrators may remove any.
  const canDelete = (message: MessageWallItem) =>
    Boolean(user) &&
    (message.user_id === user?.id || (user?.role ?? 0) >= ROLE.ADMIN)

  return (
    <Card className='mt-4'>
      <CardHeader>
        <CardTitle className='flex items-center gap-2'>
          <MessageSquare className='size-4' />
          {t('Message Wall')}
          {total > 0 && (
            <span className='text-muted-foreground text-sm font-normal'>
              ({total})
            </span>
          )}
        </CardTitle>
        <CardDescription>
          {t('Share your thoughts about the models with everyone.')}
        </CardDescription>
      </CardHeader>

      <CardContent className='space-y-4'>
        {user ? (
          <div className='space-y-2'>
            <Textarea
              value={content}
              maxLength={MAX_CONTENT_LENGTH}
              onChange={(event) => setContent(event.target.value)}
              placeholder={t('Leave a message...')}
            />
            <div className='flex items-center justify-between gap-3'>
              <span className='text-muted-foreground text-xs'>
                {content.length}/{MAX_CONTENT_LENGTH}
              </span>
              <Button
                size='sm'
                disabled={addMutation.isPending || !content.trim()}
                onClick={handleSubmit}
              >
                {addMutation.isPending ? (
                  <Loader2 className='size-3.5 animate-spin' />
                ) : (
                  <Send className='size-3.5' />
                )}
                {t('Post')}
              </Button>
            </div>
          </div>
        ) : (
          <p className='text-muted-foreground text-sm'>
            {t('Sign in to leave a message.')}
          </p>
        )}

        {isLoading ? (
          <div className='text-muted-foreground flex items-center gap-2 py-6 text-sm'>
            <Loader2 className='size-4 animate-spin' />
            {t('Loading...')}
          </div>
        ) : isError ? (
          <p className='text-destructive py-6 text-sm'>
            {getServerErrorMessage(error)}
          </p>
        ) : messages.length === 0 ? (
          <p className='text-muted-foreground py-6 text-sm'>
            {t('No messages yet. Be the first to leave one.')}
          </p>
        ) : (
          <ul className='space-y-3'>
            {messages.map((message) => (
              <li
                key={message.id}
                className='bg-muted/40 flex items-start gap-3 rounded-lg p-3'
              >
                <div className='bg-primary/10 text-primary flex size-8 shrink-0 items-center justify-center rounded-full text-xs font-medium uppercase'>
                  {message.username.slice(0, 1) || '?'}
                </div>
                <div className='min-w-0 flex-1'>
                  <div className='flex flex-wrap items-center gap-2'>
                    <span className='truncate text-sm font-medium'>
                      {message.username}
                    </span>
                    <span className='text-muted-foreground text-xs'>
                      {formatDate(message.created_at)}
                    </span>
                  </div>
                  <p className='mt-1 text-sm break-words whitespace-pre-wrap'>
                    {message.content}
                  </p>
                </div>
                {canDelete(message) && (
                  <Button
                    variant='ghost'
                    size='icon-sm'
                    className='text-muted-foreground hover:text-destructive shrink-0'
                    disabled={deleteMutation.isPending}
                    onClick={() => deleteMutation.mutate(message.id)}
                    aria-label={t('Delete')}
                  >
                    <Trash2 className='size-4' />
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}

        {totalPages > 1 && (
          <div className='flex items-center justify-center gap-3 pt-2'>
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
        )}
      </CardContent>
    </Card>
  )
}

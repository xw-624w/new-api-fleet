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
import { api } from '@/lib/api'

import type { MessageWallResponse } from './types'

// ----------------------------------------------------------------------------
// Model square message wall APIs
// ----------------------------------------------------------------------------

// Public read: works signed in or signed out.
export async function getMessageWall(
  page = 1,
  pageSize = 20
): Promise<MessageWallResponse> {
  const res = await api.get('/api/message-wall', {
    params: { page, page_size: pageSize },
  })
  return res.data
}

// Signed-in users only.
export async function addMessageWall(
  content: string
): Promise<MessageWallResponse> {
  const res = await api.post('/api/message-wall', { content })
  return res.data
}

// Allowed for the author and for administrators.
export async function removeMessageWall(
  id: number
): Promise<MessageWallResponse> {
  const res = await api.delete(`/api/message-wall/${id}`)
  return res.data
}

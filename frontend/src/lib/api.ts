import type { ClipDetail, ClipListParams, ClipListResponse } from '../types'

async function readJSON<T>(response: Response): Promise<T> {
  const text = await response.text()
  const data = text ? JSON.parse(text) : null
  if (!response.ok) {
    throw new Error(data?.error || `HTTP ${response.status}`)
  }
  return data as T
}

export async function listClips(params: ClipListParams = {}): Promise<ClipListResponse> {
  const search = new URLSearchParams()
  if (params.q) {
    search.set('q', params.q)
  }
  if (params.status) {
    search.set('status', params.status)
  }
  if (params.limit !== undefined) {
    search.set('limit', String(params.limit))
  }
  if (params.offset !== undefined) {
    search.set('offset', String(params.offset))
  }
  const query = search.toString()
  const response = await fetch(`/api/clips${query ? `?${query}` : ''}`)
  return readJSON<ClipListResponse>(response)
}

export async function getClip(id: string): Promise<ClipDetail> {
  const response = await fetch(`/api/clips/${id}`)
  return readJSON<ClipDetail>(response)
}

export async function uploadClip(file: File, title: string): Promise<ClipDetail> {
  const body = new FormData()
  body.append('file', file)
  body.append('title', title)
  const response = await fetch('/api/clips', {
    method: 'POST',
    body
  })
  return readJSON<ClipDetail>(response)
}

export async function reprocessClip(id: string): Promise<ClipDetail> {
  const response = await fetch(`/api/clips/${id}/reprocess`, { method: 'POST' })
  return readJSON<ClipDetail>(response)
}

export async function deleteClip(id: string): Promise<void> {
  const response = await fetch(`/api/clips/${id}`, { method: 'DELETE' })
  await readJSON(response)
}

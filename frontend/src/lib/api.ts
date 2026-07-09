import type { Clip, ClipDetail } from '../types'

async function readJSON<T>(response: Response): Promise<T> {
  const text = await response.text()
  const data = text ? JSON.parse(text) : null
  if (!response.ok) {
    throw new Error(data?.error || `HTTP ${response.status}`)
  }
  return data as T
}

export async function listClips(): Promise<Clip[]> {
  const response = await fetch('/api/clips')
  const data = await readJSON<{ clips: Clip[] }>(response)
  return data.clips
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

export async function importDemo(): Promise<ClipDetail> {
  const response = await fetch('/api/demo/import', { method: 'POST' })
  return readJSON<ClipDetail>(response)
}

export async function deleteClip(id: string): Promise<void> {
  const response = await fetch(`/api/clips/${id}`, { method: 'DELETE' })
  await readJSON(response)
}

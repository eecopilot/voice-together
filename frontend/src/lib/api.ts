import type { ClipDetail, ClipListParams, ClipListResponse } from '../types'

export type UploadProgress =
  | { phase: 'uploading'; percent: number | null }
  | { phase: 'confirming' }

function parseJSON<T>(text: string, status: number, ok: boolean): T {
  let data: any = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      if (ok) {
        throw new Error('Server returned invalid JSON')
      }
    }
  }
  if (!ok) {
    throw new Error(data?.error || `HTTP ${status}`)
  }
  return data as T
}

async function readJSON<T>(response: Response): Promise<T> {
  return parseJSON<T>(await response.text(), response.status, response.ok)
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

export async function uploadClip(
  file: File,
  title: string,
  onProgress?: (progress: UploadProgress) => void
): Promise<ClipDetail> {
  const body = new FormData()
  body.append('file', file)
  body.append('title', title)

  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest()
    request.open('POST', '/api/clips')
    request.upload.onprogress = (event) => {
      onProgress?.({
        phase: 'uploading',
        percent: event.lengthComputable ? Math.min(99, Math.round((event.loaded / event.total) * 100)) : null
      })
    }
    request.upload.onload = () => onProgress?.({ phase: 'confirming' })
    request.onload = () => {
      try {
        resolve(parseJSON<ClipDetail>(request.responseText, request.status, request.status >= 200 && request.status < 300))
      } catch (error) {
        reject(error)
      }
    }
    request.onerror = () => reject(new Error('Upload connection failed'))
    request.onabort = () => reject(new Error('Upload was cancelled'))
    request.send(body)
  })
}

export async function reprocessClip(id: string): Promise<ClipDetail> {
  const response = await fetch(`/api/clips/${id}/reprocess`, { method: 'POST' })
  return readJSON<ClipDetail>(response)
}

export async function deleteClip(id: string): Promise<void> {
  const response = await fetch(`/api/clips/${id}`, { method: 'DELETE' })
  await readJSON(response)
}

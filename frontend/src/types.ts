export type ClipStatus = 'processing' | 'ready' | 'error'

export type Clip = {
  id: string
  title: string
  source_path: string
  source_hash: string
  audio_path: string
  duration: number
  language: string
  status: ClipStatus
  error: string
  created_at: string
}

export type ClipFilter = 'all' | ClipStatus

export type ClipCounts = Record<ClipFilter, number>

export type ClipListParams = {
  q?: string
  status?: ClipStatus
  limit?: number
  offset?: number
}

export type ClipListResponse = {
  clips: Clip[]
  total: number
  limit: number
  offset: number
  counts: ClipCounts
}

export type Segment = {
  id: number
  clip_id: string
  start: number
  end: number
  text: string
}

export type ClipDetail = {
  clip: Clip
  segments: Segment[]
  reused?: boolean
}

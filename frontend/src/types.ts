export type Clip = {
  id: string
  title: string
  source_path: string
  audio_path: string
  duration: number
  language: string
  status: 'processing' | 'ready' | 'error'
  error: string
  created_at: string
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
}

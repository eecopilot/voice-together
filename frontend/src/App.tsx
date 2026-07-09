import { ChangeEvent, FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import { Headphones, Loader2, MoreVertical, Pause, Play, RefreshCw, Repeat, Search, SkipBack, SkipForward, Trash2, Upload } from 'lucide-react'
import { Button } from './components/ui/button'
import { Card, CardBody, CardHeader } from './components/ui/card'
import { Select } from './components/ui/select'
import { deleteClip, getClip, listClips, reprocessClip, uploadClip } from './lib/api'
import { cn, formatTime } from './lib/utils'
import type { Clip, ClipDetail } from './types'

const speeds = [0.75, 1, 1.25]
const maxVisibleClips = 10

type ClipFilter = 'all' | Clip['status']

const clipFilterOptions: Array<{ value: ClipFilter; label: string }> = [
  { value: 'all', label: '全部' },
  { value: 'ready', label: '就绪' },
  { value: 'processing', label: '处理中' },
  { value: 'error', label: '失败' }
]

const statusLabels: Record<Clip['status'], string> = {
  ready: '已就绪',
  processing: '处理中',
  error: '失败'
}

function App() {
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const [clips, setClips] = useState<Clip[]>([])
  const [detail, setDetail] = useState<ClipDetail | null>(null)
  const [selectedIndex, setSelectedIndex] = useState<number | null>(null)
  const [loop, setLoop] = useState(true)
  const [speed, setSpeed] = useState(1)
  const [file, setFile] = useState<File | null>(null)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [isPlaying, setIsPlaying] = useState(false)
  const [openMenuId, setOpenMenuId] = useState<string | null>(null)
  const [clipQuery, setClipQuery] = useState('')
  const [clipFilter, setClipFilter] = useState<ClipFilter>('all')
  const [currentTime, setCurrentTime] = useState(0)
  const [duration, setDuration] = useState(0)

  const segments = detail?.segments ?? []
  const audioSource = detail?.clip.status === 'ready' ? `/api/clips/${detail.clip.id}/audio` : ''
  const effectiveDuration = duration || detail?.clip.duration || 0
  const seekValue = effectiveDuration > 0 ? Math.min(currentTime, effectiveDuration) : 0
  const progressPercent = effectiveDuration > 0 ? (seekValue / effectiveDuration) * 100 : 0

  useEffect(() => {
    refreshClips()
  }, [])

  useEffect(() => {
    if (audioRef.current) {
      audioRef.current.playbackRate = speed
    }
  }, [speed])

  const playbackIndex = findSegmentIndex(segments, currentTime)
  const currentIndex = playbackIndex ?? selectedIndex
  const currentSegment = currentIndex === null ? null : segments[currentIndex] ?? null
  const loopSegment = selectedIndex === null ? currentSegment : segments[selectedIndex] ?? currentSegment
  const totalSegments = segments.length
  const readyClips = useMemo(() => clips.filter((clip) => clip.status === 'ready').length, [clips])
  const clipStats = useMemo(
    () =>
      clips.reduce<Record<ClipFilter, number>>(
        (stats, clip) => {
          stats.all += 1
          stats[clip.status] += 1
          return stats
        },
        { all: 0, ready: 0, processing: 0, error: 0 }
      ),
    [clips]
  )
  const visibleClips = useMemo(() => {
    const query = clipQuery.trim().toLowerCase()
    return clips.filter((clip) => {
      if (clipFilter !== 'all' && clip.status !== clipFilter) {
        return false
      }
      if (!query) {
        return true
      }
      const searchable = [clip.title, clip.language, statusLabels[clip.status], clip.status].join(' ').toLowerCase()
      return searchable.includes(query)
    })
  }, [clipFilter, clipQuery, clips])
  const displayedClips = visibleClips.slice(0, maxVisibleClips)
  const hasMoreClips = visibleClips.length > displayedClips.length

  async function refreshClips() {
    try {
      const next = await listClips()
      setClips(next)
      if (!detail && next.length > 0) {
        const preferredClip = next.find((clip) => clip.status === 'ready') ?? next[0]
        await openClip(preferredClip.id)
      }
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  async function openClip(id: string) {
    setError('')
    setNotice('')
    setOpenMenuId(null)
    setIsPlaying(false)
    resetPlaybackState()
    const next = normalizeDetail(await getClip(id))
    setDetail(next)
    setSelectedIndex(next.segments.length > 0 ? 0 : null)
  }

  async function handleUpload(event: FormEvent) {
    event.preventDefault()
    if (!file) {
      setError('请选择视频或音频文件')
      return
    }

    setBusy('upload')
    setError('')
    setNotice('')
    try {
      const title = file.name.replace(/\.[^.]+$/, '')
      const next = normalizeDetail(await uploadClip(file, title))
      setDetail(next)
      setSelectedIndex(next.segments.length > 0 ? 0 : null)
      setIsPlaying(false)
      resetPlaybackState()
      setFile(null)
      setNotice(next.reused ? '这个文件已经在片段库中，已打开已有片段。' : '')
      await refreshClips()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy('')
    }
  }

  async function handleReprocess(id: string) {
    setBusy(`reprocess-${id}`)
    setError('')
    setNotice('')
    setOpenMenuId(null)
    try {
      const next = normalizeDetail(await reprocessClip(id))
      setDetail(next)
      setSelectedIndex(next.segments.length > 0 ? 0 : null)
      setIsPlaying(false)
      resetPlaybackState()
      setNotice('已重新完成转写。')
      await refreshClips()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy('')
    }
  }

  async function handleDelete(id: string) {
    setBusy(`delete-${id}`)
    setError('')
    setNotice('')
    setOpenMenuId(null)
    try {
      await deleteClip(id)
      if (detail?.clip.id === id) {
        setDetail(null)
        setSelectedIndex(null)
        setIsPlaying(false)
        resetPlaybackState()
      }
      await refreshClips()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy('')
    }
  }

  function handleFileChange(event: ChangeEvent<HTMLInputElement>) {
    setFile(event.target.files?.[0] ?? null)
    setNotice('')
  }

  function playSegment(index: number) {
    const segment = segments[index]
    const audio = audioRef.current
    if (!segment || !audio) {
      return
    }
    setSelectedIndex(index)
    audio.currentTime = segment.start
    setCurrentTime(segment.start)
    void audio.play()
  }

  function previousSegment() {
    if (!detail || segments.length === 0) {
      return
    }
    const nextIndex = Math.max(0, (currentIndex ?? 0) - 1)
    playSegment(nextIndex)
  }

  function nextSegment() {
    if (!detail || segments.length === 0) {
      return
    }
    const nextIndex = Math.min(segments.length - 1, (currentIndex ?? 0) + 1)
    playSegment(nextIndex)
  }

  function togglePlay() {
    const audio = audioRef.current
    if (!audio) {
      return
    }
    if (audio.paused) {
      void audio.play()
    } else {
      audio.pause()
    }
  }

  function handleTimeUpdate() {
    const audio = audioRef.current
    if (!audio || !detail) {
      return
    }
    const current = audio.currentTime
    if (loop && loopSegment && current >= loopSegment.end) {
      audio.currentTime = loopSegment.start
      setCurrentTime(loopSegment.start)
      void audio.play()
      return
    }

    setCurrentTime(current)
  }

  function handleLoadedMetadata() {
    const audio = audioRef.current
    if (!audio) {
      return
    }
    setDuration(Number.isFinite(audio.duration) ? audio.duration : 0)
    setCurrentTime(audio.currentTime)
  }

  function handleSeek(value: number) {
    const audio = audioRef.current
    if (!audio) {
      return
    }
    audio.currentTime = value
    setCurrentTime(value)
    const index = findSegmentIndex(segments, value)
    if (index !== null) {
      setSelectedIndex(index)
    }
  }

  function resetPlaybackState() {
    setCurrentTime(0)
    setDuration(0)
  }

  return (
    <main className="min-h-screen bg-background text-foreground">
      <div className="mx-auto grid max-w-[1440px] grid-cols-1 gap-5 px-4 py-5 lg:grid-cols-[340px_minmax(0,1fr)]">
        <aside className="grid min-w-0 gap-5 lg:sticky lg:top-5 lg:max-h-[calc(100vh-40px)] lg:grid-rows-[auto_minmax(0,1fr)]">
          <Card className="min-w-0">
            <CardHeader>
              <div className="flex items-center gap-3">
                <div className="flex h-10 w-10 items-center justify-center rounded-md bg-teal-50 text-primary">
                  <Headphones size={22} />
                </div>
                <div className="min-w-0">
                  <h1 className="text-lg font-bold">Voice Together</h1>
                  <p className="text-sm text-muted">{readyClips} 个可学习片段</p>
                </div>
              </div>
            </CardHeader>
            <CardBody>
              <form className="grid gap-3" onSubmit={handleUpload}>
                <label className="grid gap-2 text-sm font-semibold">
                  上传视频或音频
                  <span className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] items-center gap-2 rounded-md border border-border bg-white p-2">
                    <span className="inline-flex h-9 cursor-pointer items-center justify-center rounded border border-border bg-slate-50 px-3 text-sm font-semibold text-slate-900">
                      选择文件
                    </span>
                    <span className="truncate text-sm font-medium text-muted">{file?.name ?? '未选择文件'}</span>
                    <input className="sr-only" type="file" accept="video/*,audio/*" onChange={handleFileChange} />
                  </span>
                </label>
                <Button variant="primary" type="submit" disabled={busy === 'upload'}>
                  {busy === 'upload' ? <Loader2 className="animate-spin" size={16} /> : <Upload size={16} />}
                  上传并转写
                </Button>
              </form>
              {error ? <p className="mt-3 rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">{error}</p> : null}
              {notice ? <p className="mt-3 rounded-md bg-teal-50 px-3 py-2 text-sm text-primary">{notice}</p> : null}
            </CardBody>
          </Card>

          <Card className="flex min-h-0 min-w-0 flex-col">
            <CardHeader className="grid gap-3">
              <div className="flex items-center justify-between gap-3">
                <h2 className="text-sm font-bold">片段库</h2>
                <span className="shrink-0 text-xs font-semibold text-muted">
                  {displayedClips.length}/{visibleClips.length}
                </span>
              </div>
              <label className="flex h-9 min-w-0 items-center gap-2 rounded-md border border-border bg-white px-3 text-muted">
                <Search size={15} />
                <input
                  className="min-w-0 flex-1 bg-transparent text-sm font-medium text-slate-900 outline-none placeholder:text-muted"
                  value={clipQuery}
                  onChange={(event) => {
                    setClipQuery(event.target.value)
                    setOpenMenuId(null)
                  }}
                  placeholder="搜索标题或语言"
                />
              </label>
              <div className="grid grid-cols-4 gap-1 rounded-md bg-slate-100 p-1">
                {clipFilterOptions.map((option) => (
                  <button
                    key={option.value}
                    className={cn(
                      'h-8 rounded px-1 text-xs font-semibold transition',
                      clipFilter === option.value ? 'bg-white text-slate-950 shadow-sm' : 'text-muted hover:text-slate-900'
                    )}
                    type="button"
                    onClick={() => {
                      setClipFilter(option.value)
                      setOpenMenuId(null)
                    }}
                  >
                    {option.label} {clipStats[option.value]}
                  </button>
                ))}
              </div>
            </CardHeader>
            <CardBody className="min-h-0 overflow-y-auto p-3">
              {clips.length === 0 ? (
                <p className="px-2 py-6 text-sm text-muted">还没有导入片段。</p>
              ) : displayedClips.length === 0 ? (
                <p className="px-2 py-6 text-sm text-muted">没有匹配的片段。</p>
              ) : (
                <div className="grid gap-2">
                {hasMoreClips ? (
                  <p className="rounded-md bg-slate-50 px-3 py-2 text-xs font-medium text-muted">
                    只显示前 {maxVisibleClips} 个，搜索可找到更多片段。
                  </p>
                ) : null}
                {displayedClips.map((clip) => (
                  <article
                    key={clip.id}
                    className={cn(
                      'grid min-w-0 gap-2 rounded-md border p-3',
                      detail?.clip.id === clip.id ? 'border-primary bg-teal-50/60' : 'border-border bg-white'
                    )}
                  >
                    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] gap-2">
                      <button className="min-w-0 text-left" type="button" onClick={() => openClip(clip.id)}>
                        <strong className="block truncate text-sm">{clip.title}</strong>
                        <span className="mt-1 flex min-w-0 items-center gap-2 text-xs text-muted">
                          <span className="shrink-0">{clip.status === 'ready' ? `${clip.duration.toFixed(1)}s` : statusLabels[clip.status]}</span>
                          <span className="truncate">{formatClipDate(clip.created_at)}</span>
                        </span>
                      </button>
                      <div className="flex items-start">
                        <Button
                          className="h-8 w-8 px-0"
                          variant="ghost"
                          type="button"
                          onClick={() => setOpenMenuId(openMenuId === clip.id ? null : clip.id)}
                          aria-label="片段操作"
                        >
                          <MoreVertical size={15} />
                        </Button>
                      </div>
                    </div>
                    <div className="flex items-center justify-between gap-2">
                      <span className={cn('rounded px-2 py-1 text-xs font-semibold', clip.status === 'ready' ? 'bg-emerald-50 text-emerald-700' : clip.status === 'error' ? 'bg-red-50 text-red-700' : 'bg-amber-50 text-amber-700')}>
                        {statusLabels[clip.status]}
                      </span>
                      {detail?.clip.id === clip.id ? <span className="text-xs font-semibold text-primary">当前</span> : null}
                    </div>
                    {openMenuId === clip.id ? (
                      <div className="grid grid-cols-2 gap-2 border-t border-border pt-2">
                        <button
                          className="inline-flex h-8 items-center justify-center gap-2 rounded-md bg-slate-50 px-2 text-xs font-semibold text-slate-800 hover:bg-slate-100 disabled:opacity-50"
                          type="button"
                          onClick={() => handleReprocess(clip.id)}
                          disabled={busy === `reprocess-${clip.id}` || clip.status === 'processing'}
                        >
                          {busy === `reprocess-${clip.id}` ? <Loader2 className="animate-spin" size={13} /> : <RefreshCw size={13} />}
                          重新转写
                        </button>
                        <button
                          className="inline-flex h-8 items-center justify-center gap-2 rounded-md bg-red-50 px-2 text-xs font-semibold text-red-700 hover:bg-red-100 disabled:opacity-50"
                          type="button"
                          onClick={() => handleDelete(clip.id)}
                          disabled={busy === `delete-${clip.id}`}
                        >
                          <Trash2 size={13} />
                          删除
                        </button>
                      </div>
                    ) : null}
                  </article>
                ))}
                </div>
              )}
            </CardBody>
          </Card>
        </aside>

        <section className="grid min-w-0 content-start gap-5">
          <Card className="min-w-0">
            <CardHeader>
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="min-w-0">
                  <h2 className="text-xl font-bold">{detail?.clip.title ?? '选择一个片段开始学习'}</h2>
                  <p className="text-sm text-muted">
                    {detail ? `${totalSegments} 句字幕 · ${detail.clip.language || 'auto'}` : '上传文件后，会在这里同步播放语音和字幕。'}
                  </p>
                </div>
                {currentSegment ? (
                  <span className="rounded-md bg-orange-50 px-3 py-2 text-sm font-semibold text-orange-700">
                    {formatTime(currentSegment.start)} - {formatTime(currentSegment.end)}
                  </span>
                ) : null}
              </div>
            </CardHeader>
            <CardBody className="grid gap-4">
              {detail?.clip.error ? (
                <p className="rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-800">{detail.clip.error}</p>
              ) : null}
              <audio
                ref={audioRef}
                className="hidden"
                src={audioSource}
                preload="metadata"
                onLoadedMetadata={handleLoadedMetadata}
                onTimeUpdate={handleTimeUpdate}
                onPlay={() => setIsPlaying(true)}
                onPause={() => setIsPlaying(false)}
                onEnded={() => setIsPlaying(false)}
              />
              <div className="grid gap-3 rounded-lg bg-slate-50/90 p-3 sm:p-4">
                <div className="grid min-w-0 gap-1.5">
                  <div className="flex items-center justify-between gap-3 text-xs font-semibold text-muted">
                    <span className="font-mono">{formatTime(seekValue)}</span>
                    <span className="font-mono">{formatTime(effectiveDuration)}</span>
                  </div>
                  <input
                    className="player-range"
                    type="range"
                    min={0}
                    max={Math.max(effectiveDuration, 0)}
                    step={0.05}
                    value={seekValue}
                    onChange={(event) => handleSeek(Number(event.target.value))}
                    disabled={!audioSource || effectiveDuration <= 0}
                    style={{
                      background: `linear-gradient(to right, #0f766e 0%, #0f766e ${progressPercent}%, #cbd5e1 ${progressPercent}%, #cbd5e1 100%)`
                    }}
                    aria-label="播放进度"
                  />
                </div>

                {currentSegment ? (
                  <div className="rounded-md border border-teal-100 bg-white px-3 py-2.5 sm:px-4 sm:py-3">
                    <p className="text-base font-semibold leading-7 text-slate-950">{currentSegment.text}</p>
                  </div>
                ) : null}

                <div className="grid grid-cols-[48px_48px_48px_48px_48px] items-center gap-2 md:flex md:flex-wrap">
                  <Button className="h-10 w-full px-0 md:w-auto md:px-4" type="button" onClick={previousSegment} disabled={!audioSource || segments.length === 0} aria-label="上一句">
                    <SkipBack size={16} />
                    <span className="hidden md:inline">上一句</span>
                  </Button>
                  <Button className="h-10 w-full rounded-full px-0 md:w-auto md:px-4" variant="primary" type="button" onClick={togglePlay} disabled={!audioSource} aria-label={isPlaying ? '暂停' : '播放'}>
                    {isPlaying ? <Pause size={16} /> : <Play size={16} />}
                    <span className="hidden md:inline">{isPlaying ? '暂停' : '播放'}</span>
                  </Button>
                  <Button className="h-10 w-full px-0 md:w-auto md:px-4" type="button" onClick={nextSegment} disabled={!audioSource || segments.length === 0} aria-label="下一句">
                    <span className="hidden md:inline">下一句</span>
                    <SkipForward size={16} />
                  </Button>
                  <Button className="h-10 w-full px-0 md:w-auto md:px-4" type="button" variant={loop ? 'primary' : 'secondary'} onClick={() => setLoop(!loop)} aria-label={loop ? '循环当前句' : '不循环'}>
                    <Repeat size={16} />
                    <span className="hidden md:inline">{loop ? '循环当前句' : '不循环'}</span>
                  </Button>
                  <Select
                    className="w-full md:w-20"
                    value={speed}
                    onChange={(event) => setSpeed(Number(event.target.value))}
                    aria-label="播放速度"
                  >
                    {speeds.map((value) => (
                      <option key={value} value={value}>
                        {value}x
                      </option>
                    ))}
                  </Select>
                </div>
              </div>
            </CardBody>
          </Card>

          <Card className="min-w-0">
            <CardHeader>
              <h2 className="text-sm font-bold">字幕</h2>
            </CardHeader>
            <CardBody className="max-h-[58vh] overflow-y-auto p-0">
              {!detail ? (
                <p className="px-5 py-8 text-sm text-muted">暂无字幕。</p>
              ) : segments.length === 0 ? (
                <p className="px-5 py-8 text-sm text-muted">没有识别到字幕。</p>
              ) : (
                <div className="divide-y divide-border">
                  {segments.map((segment, index) => (
                    <button
                      key={`${segment.start}-${index}`}
                      type="button"
                      className={cn(
                        'grid w-full grid-cols-[76px_minmax(0,1fr)] items-start gap-4 px-4 py-4 text-left transition sm:grid-cols-[92px_minmax(0,1fr)] sm:px-5',
                        currentIndex === index ? 'bg-teal-50' : 'hover:bg-slate-50'
                      )}
                      onClick={() => playSegment(index)}
                    >
                      <span className="pt-1 font-mono text-xs font-semibold tabular-nums text-muted">{formatTime(segment.start)}</span>
                      <span className={cn('min-w-0 text-base leading-7', currentIndex === index ? 'font-semibold text-primary' : 'text-slate-800')}>
                        {segment.text}
                      </span>
                    </button>
                  ))}
                </div>
              )}
            </CardBody>
          </Card>
        </section>
      </div>
    </main>
  )
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : '操作失败'
}

function formatClipDate(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  }).format(date)
}

function findSegmentIndex(segments: ClipDetail['segments'], time: number) {
  if (!Number.isFinite(time)) {
    return null
  }
  const index = segments.findIndex((segment) => time >= segment.start && time < segment.end)
  return index === -1 ? null : index
}

function normalizeDetail(detail: ClipDetail): ClipDetail {
  return {
    ...detail,
    segments: Array.isArray(detail.segments) ? detail.segments : []
  }
}

export default App

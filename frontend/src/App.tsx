import { ChangeEvent, FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import { Headphones, Loader2, Pause, Play, Repeat, SkipBack, SkipForward, Trash2, Upload } from 'lucide-react'
import { Button } from './components/ui/button'
import { Card, CardBody, CardHeader } from './components/ui/card'
import { deleteClip, getClip, importDemo, listClips, uploadClip } from './lib/api'
import { cn, formatTime } from './lib/utils'
import type { Clip, ClipDetail } from './types'

const speeds = [0.75, 1, 1.25]

function App() {
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const [clips, setClips] = useState<Clip[]>([])
  const [detail, setDetail] = useState<ClipDetail | null>(null)
  const [selectedIndex, setSelectedIndex] = useState<number | null>(null)
  const [activeIndex, setActiveIndex] = useState<number | null>(null)
  const [loop, setLoop] = useState(true)
  const [speed, setSpeed] = useState(1)
  const [file, setFile] = useState<File | null>(null)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')

  const selectedSegment = selectedIndex === null ? null : detail?.segments[selectedIndex] ?? null
  const audioSource = detail?.clip.status === 'ready' ? `/api/clips/${detail.clip.id}/audio` : ''

  useEffect(() => {
    refreshClips()
  }, [])

  useEffect(() => {
    if (audioRef.current) {
      audioRef.current.playbackRate = speed
    }
  }, [speed])

  const activeOrSelectedIndex = activeIndex ?? selectedIndex
  const totalSegments = detail?.segments.length ?? 0
  const readyClips = useMemo(() => clips.filter((clip) => clip.status === 'ready').length, [clips])

  async function refreshClips() {
    try {
      const next = await listClips()
      setClips(next)
      if (!detail && next.length > 0) {
        await openClip(next[0].id)
      }
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  async function openClip(id: string) {
    setError('')
    const next = await getClip(id)
    setDetail(next)
    setSelectedIndex(next.segments.length > 0 ? 0 : null)
    setActiveIndex(null)
  }

  async function handleUpload(event: FormEvent) {
    event.preventDefault()
    if (!file) {
      setError('请选择视频或音频文件')
      return
    }

    setBusy('upload')
    setError('')
    try {
      const title = file.name.replace(/\.[^.]+$/, '')
      const next = await uploadClip(file, title)
      setDetail(next)
      setSelectedIndex(next.segments.length > 0 ? 0 : null)
      setFile(null)
      await refreshClips()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy('')
    }
  }

  async function handleImportDemo() {
    setBusy('demo')
    setError('')
    try {
      const next = await importDemo()
      setDetail(next)
      setSelectedIndex(next.segments.length > 0 ? 0 : null)
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
    try {
      await deleteClip(id)
      if (detail?.clip.id === id) {
        setDetail(null)
        setSelectedIndex(null)
        setActiveIndex(null)
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
  }

  function playSegment(index: number) {
    const segment = detail?.segments[index]
    const audio = audioRef.current
    if (!segment || !audio) {
      return
    }
    setSelectedIndex(index)
    setActiveIndex(index)
    audio.currentTime = segment.start
    void audio.play()
  }

  function previousSegment() {
    if (!detail || detail.segments.length === 0) {
      return
    }
    const nextIndex = Math.max(0, (selectedIndex ?? activeIndex ?? 0) - 1)
    playSegment(nextIndex)
  }

  function nextSegment() {
    if (!detail || detail.segments.length === 0) {
      return
    }
    const nextIndex = Math.min(detail.segments.length - 1, (selectedIndex ?? activeIndex ?? 0) + 1)
    playSegment(nextIndex)
  }

  function togglePlay() {
    const audio = audioRef.current
    if (!audio) {
      return
    }
    if (audio.paused) {
      if (selectedSegment && (audio.currentTime < selectedSegment.start || audio.currentTime >= selectedSegment.end)) {
        audio.currentTime = selectedSegment.start
      }
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
    const index = detail.segments.findIndex((segment) => current >= segment.start && current < segment.end)
    setActiveIndex(index === -1 ? null : index)

    if (loop && selectedSegment && current >= selectedSegment.end) {
      audio.currentTime = selectedSegment.start
      void audio.play()
    }
  }

  return (
    <main className="min-h-screen bg-background text-foreground">
      <div className="mx-auto grid max-w-7xl gap-5 px-4 py-5 lg:grid-cols-[340px_1fr]">
        <aside className="grid gap-5 content-start">
          <Card>
            <CardHeader>
              <div className="flex items-center gap-3">
                <div className="flex h-10 w-10 items-center justify-center rounded-md bg-teal-50 text-primary">
                  <Headphones size={22} />
                </div>
                <div>
                  <h1 className="text-lg font-bold">Voice Together</h1>
                  <p className="text-sm text-muted">{readyClips} 个可学习片段</p>
                </div>
              </div>
            </CardHeader>
            <CardBody>
              <form className="grid gap-3" onSubmit={handleUpload}>
                <label className="grid gap-2 text-sm font-semibold">
                  上传视频或音频
                  <input
                    className="rounded-md border border-border bg-white px-3 py-2 text-sm"
                    type="file"
                    accept="video/*,audio/*"
                    onChange={handleFileChange}
                  />
                </label>
                <Button variant="primary" type="submit" disabled={busy === 'upload'}>
                  {busy === 'upload' ? <Loader2 className="animate-spin" size={16} /> : <Upload size={16} />}
                  上传并转写
                </Button>
                <Button type="button" onClick={handleImportDemo} disabled={busy === 'demo'}>
                  {busy === 'demo' ? <Loader2 className="animate-spin" size={16} /> : null}
                  导入 demo.mp4
                </Button>
              </form>
              {error ? <p className="mt-3 rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">{error}</p> : null}
            </CardBody>
          </Card>

          <Card>
            <CardHeader>
              <h2 className="text-sm font-bold">片段库</h2>
            </CardHeader>
            <CardBody className="grid gap-2">
              {clips.length === 0 ? (
                <p className="text-sm text-muted">还没有导入片段。</p>
              ) : (
                clips.map((clip) => (
                  <article
                    key={clip.id}
                    className={cn(
                      'grid gap-2 rounded-md border p-3',
                      detail?.clip.id === clip.id ? 'border-primary bg-teal-50/60' : 'border-border bg-white'
                    )}
                  >
                    <button className="text-left" type="button" onClick={() => openClip(clip.id)}>
                      <strong className="block truncate text-sm">{clip.title}</strong>
                      <span className="text-xs text-muted">
                        {clip.status === 'ready' ? `${clip.duration.toFixed(1)}s` : clip.status}
                      </span>
                    </button>
                    <div className="flex items-center justify-between gap-2">
                      <span className={cn('rounded px-2 py-1 text-xs font-semibold', clip.status === 'ready' ? 'bg-emerald-50 text-emerald-700' : clip.status === 'error' ? 'bg-red-50 text-red-700' : 'bg-amber-50 text-amber-700')}>
                        {clip.status === 'ready' ? '已就绪' : clip.status === 'error' ? '失败' : '处理中'}
                      </span>
                      <Button
                        className="h-8 px-2"
                        variant="ghost"
                        type="button"
                        onClick={() => handleDelete(clip.id)}
                        disabled={busy === `delete-${clip.id}`}
                        aria-label="删除片段"
                      >
                        <Trash2 size={15} />
                      </Button>
                    </div>
                  </article>
                ))
              )}
            </CardBody>
          </Card>
        </aside>

        <section className="grid gap-5 content-start">
          <Card>
            <CardHeader>
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <h2 className="text-xl font-bold">{detail?.clip.title ?? '选择一个片段开始学习'}</h2>
                  <p className="text-sm text-muted">
                    {detail ? `${totalSegments} 句字幕 · ${detail.clip.language || 'auto'}` : '上传或导入 demo 后，会在这里同步播放语音和字幕。'}
                  </p>
                </div>
                {selectedSegment ? (
                  <span className="rounded-md bg-orange-50 px-3 py-2 text-sm font-semibold text-orange-700">
                    {formatTime(selectedSegment.start)} - {formatTime(selectedSegment.end)}
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
                className="w-full"
                src={audioSource}
                controls
                preload="metadata"
                onTimeUpdate={handleTimeUpdate}
              />
              <div className="flex flex-wrap items-center gap-2">
                <Button type="button" onClick={togglePlay} disabled={!detail}>
                  <Play size={16} />
                  <Pause size={16} className="hidden" />
                  播放/暂停
                </Button>
                <Button type="button" onClick={previousSegment} disabled={!detail}>
                  <SkipBack size={16} />
                  上一句
                </Button>
                <Button type="button" onClick={nextSegment} disabled={!detail}>
                  下一句
                  <SkipForward size={16} />
                </Button>
                <Button type="button" variant={loop ? 'primary' : 'secondary'} onClick={() => setLoop(!loop)}>
                  <Repeat size={16} />
                  {loop ? '循环当前句' : '不循环'}
                </Button>
                <select
                  className="h-10 rounded-md border border-border bg-white px-3 text-sm font-semibold"
                  value={speed}
                  onChange={(event) => setSpeed(Number(event.target.value))}
                >
                  {speeds.map((value) => (
                    <option key={value} value={value}>
                      {value}x
                    </option>
                  ))}
                </select>
              </div>
            </CardBody>
          </Card>

          <Card>
            <CardHeader>
              <h2 className="text-sm font-bold">字幕</h2>
            </CardHeader>
            <CardBody className="max-h-[58vh] overflow-y-auto p-0">
              {!detail ? (
                <p className="px-5 py-8 text-sm text-muted">暂无字幕。</p>
              ) : detail.segments.length === 0 ? (
                <p className="px-5 py-8 text-sm text-muted">没有识别到字幕。</p>
              ) : (
                <div className="divide-y divide-border">
                  {detail.segments.map((segment, index) => (
                    <button
                      key={`${segment.start}-${index}`}
                      type="button"
                      className={cn(
                        'grid w-full grid-cols-[72px_1fr] gap-3 px-5 py-4 text-left transition',
                        activeOrSelectedIndex === index ? 'bg-teal-50' : 'hover:bg-slate-50'
                      )}
                      onClick={() => playSegment(index)}
                    >
                      <span className="font-mono text-xs text-muted">{formatTime(segment.start)}</span>
                      <span className={cn('text-base leading-7', activeOrSelectedIndex === index ? 'font-semibold text-primary' : 'text-slate-800')}>
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

export default App

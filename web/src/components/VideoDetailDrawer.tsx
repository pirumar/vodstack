import { useCallback, useEffect, useRef, useState, type ChangeEvent, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { AnimatePresence, motion } from 'framer-motion'
import {
  X,
  Copy,
  Check,
  Code2,
  Film,
  Layers,
  Clock,
  HardDrive,
  Activity,
  ShieldCheck,
  Sparkles,
  Captions as CaptionsIcon,
  Lock,
  LoaderCircle,
  Users,
  Pencil,
  Download,
  Link2,
  Fingerprint,
  KeyRound,
  Save,
  Tag,
  Image as ImageIcon,
  Upload as UploadIcon,
  BarChart3,
} from 'lucide-react'
import { Search, Play, RefreshCw } from 'lucide-react'
import {
  api,
  type Video,
  type VideoAnalytics,
  type ViewerProgressRow,
  type Visibility,
  type SearchHit,
  type VideoOperation,
  type OperationKind,
  type OperationStatus,
} from '../api'
import { useLibrary } from '../LibraryContext'
import { Card } from './ui'
import { StatusChip } from './StatusChip'
import { VideoPlayer } from './VideoPlayer'
import { ChapterCaptionEditor } from './ChapterCaptionEditor'
import { Heatmap } from './charts/Heatmap'
import { formatBytes, formatDuration } from '../lib/format'

// Right-side slide-in drawer with the full video detail: player, metadata,
// chapters/captions editor, per-video analytics + heatmap, advanced ops, and
// access policy. Opened from the Library route /library/:videoId.
export function VideoDetailDrawer({
  videoId,
  initialSeek,
  onClose,
  onChanged,
}: {
  videoId: string
  initialSeek?: number
  onClose: () => void
  // Called after a metadata edit (title/description/tags) so the parent list can
  // refresh and reflect the change on the video cards.
  onChanged?: () => void
}) {
  const { embedBaseUrl, libraryId } = useLibrary()

  const [video, setVideo] = useState<Video | null>(null)
  const [hlsUrl, setHlsUrl] = useState('')
  const [posterUrl, setPosterUrl] = useState('')
  const [analytics, setAnalytics] = useState<VideoAnalytics | null>(null)
  const [viewers, setViewers] = useState<ViewerProgressRow[] | null>(null)
  const [heatmap, setHeatmap] = useState<number[] | null>(null)
  const [copied, setCopied] = useState<string | null>(null)
  const [download, setDownload] = useState<{ url?: string; enabled: boolean } | null>(null)
  const [editingTitle, setEditingTitle] = useState(false)
  const [titleDraft, setTitleDraft] = useState('')
  const [busy, setBusy] = useState<string | null>(null)
  const [ops, setOps] = useState<Record<string, VideoOperation>>({})
  const [toast, setToast] = useState('')
  // Player seek control: a nonce makes repeat clicks on the same moment re-seek.
  const [seek, setSeek] = useState<{ sec: number; nonce: number } | null>(
    initialSeek != null && !Number.isNaN(initialSeek) ? { sec: initialSeek, nonce: 1 } : null,
  )
  const seekTo = (sec: number) => setSeek((s) => ({ sec, nonce: (s?.nonce ?? 0) + 1 }))

  // Pull the live status of advanced operations into a kind→op map.
  const loadOps = useCallback(() => {
    api
      .getVideoOperations(videoId)
      .then((d) => {
        const m: Record<string, VideoOperation> = {}
        for (const op of d.operations) m[op.kind] = op
        setOps(m)
      })
      .catch(() => {})
  }, [videoId])

  const load = useCallback(() => {
    api.getVideo(videoId).then(setVideo).catch(() => {})
    api
      .play(videoId)
      .then((d) => {
        setHlsUrl(d.hlsUrl ?? '')
        setPosterUrl(d.posterUrl ?? '')
      })
      .catch(() => {})
    api.getVideoAnalytics(videoId).then(setAnalytics).catch(() => {})
    api.getVideoViewers(videoId).then(setViewers).catch(() => {})
    api
      .download(videoId)
      .then((d) => setDownload({ url: d.downloadUrl, enabled: !!d.downloadUrl }))
      .catch(() => setDownload({ enabled: false }))
    loadOps()
  }, [videoId, loadOps])

  useEffect(() => {
    setVideo(null)
    setAnalytics(null)
    setViewers(null)
    setHeatmap(null)
    setDownload(null)
    setEditingTitle(false)
    setPosterUrl('')
    setOps({})
    load()
  }, [load])

  // When a frame-grab poster job finishes, refresh the signed play payload so the
  // new poster preview shows up without a manual reload.
  const posterDone = ops['poster']?.status === 'done'
  useEffect(() => {
    if (!posterDone) return
    api.play(videoId).then((d) => setPosterUrl(d.posterUrl ?? '')).catch(() => {})
  }, [posterDone, videoId])

  // Poll operation status: fast (2.5s) while anything is queued/running, slow
  // (10s) otherwise so a job started elsewhere still shows up. Mirrors the
  // LibraryPage video-status polling cadence.
  const opsWorking = Object.values(ops).some(
    (o) => o.status === 'queued' || o.status === 'running',
  )
  useEffect(() => {
    const id = window.setInterval(loadOps, opsWorking ? 2500 : 10000)
    return () => window.clearInterval(id)
  }, [opsWorking, loadOps])

  // Close on Escape.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  // Best-effort heatmap from the public embed endpoint (CORS-enabled).
  useEffect(() => {
    if (!embedBaseUrl) return
    const base = embedBaseUrl.replace(/\/$/, '')
    fetch(`${base}/embed/${libraryId}/${videoId}/heatmap`)
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => setHeatmap(Array.isArray(d?.heatmap) ? d.heatmap : null))
      .catch(() => {})
  }, [embedBaseUrl, libraryId, videoId])

  function flash(msg: string) {
    setToast(msg)
    setTimeout(() => setToast(''), 2600)
  }

  // Generic copy-to-clipboard with a per-key checkmark flash.
  function copyVal(key: string, text: string) {
    if (!text) return
    navigator.clipboard.writeText(text)
    setCopied(key)
    setTimeout(() => setCopied(null), 1600)
  }

  function copyEmbed() {
    if (!video) return
    const base = (embedBaseUrl || '').replace(/\/$/, '')
    const src = `${base}/embed/${video.libraryId || libraryId}/${video.videoId}`
    copyVal(
      'embed',
      `<iframe src="${src}" width="640" height="360" frameborder="0" allow="autoplay; fullscreen; picture-in-picture" allowfullscreen></iframe>`,
    )
  }

  // Persist a metadata patch, optimistically update local state, and notify the
  // parent so the library cards refresh.
  async function saveMeta(patch: { title?: string; description?: string; tags?: string[] }, ok: string) {
    if (!video) return
    const updated = await api.updateVideo(video.videoId, patch)
    setVideo((v) => (v ? { ...v, ...updated } : v))
    flash(ok)
    onChanged?.()
  }

  async function saveTitle() {
    const next = titleDraft.trim()
    setEditingTitle(false)
    if (!video || !next || next === video.title) return
    try {
      await saveMeta({ title: next }, 'Başlık güncellendi')
    } catch (e) {
      flash(e instanceof Error ? e.message : 'kaydedilemedi')
    }
  }

  async function runOp(key: string, fn: () => Promise<unknown>, ok: string) {
    setBusy(key)
    try {
      await fn()
      flash(ok)
      // Optimistically mark the job queued so the button locks immediately, then
      // let polling pick up running→done/failed from the backend.
      const kind = OP_KIND[key]
      if (kind) {
        setOps((m) => ({
          ...m,
          [kind]: { kind, status: 'queued', updatedAt: new Date().toISOString() },
        }))
        loadOps()
      }
    } catch (e) {
      flash(e instanceof Error ? e.message : 'işlem başarısız')
    } finally {
      setBusy(null)
    }
  }

  const ready = video?.status === 4

  return (
    <AnimatePresence>
      <motion.div
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        exit={{ opacity: 0 }}
        onClick={onClose}
        className="fixed inset-0 z-50 bg-ink/70 backdrop-blur-sm"
      >
        <motion.aside
          initial={{ x: '100%' }}
          animate={{ x: 0 }}
          exit={{ x: '100%' }}
          transition={{ type: 'tween', duration: 0.32, ease: [0.16, 1, 0.3, 1] }}
          onClick={(e) => e.stopPropagation()}
          className="absolute right-0 top-0 flex h-full w-full max-w-4xl flex-col border-l border-edge bg-graphite shadow-deck"
        >
          {/* Header */}
          <div className="sticky top-0 z-10 flex items-center gap-3 border-b border-edge bg-graphite/95 px-5 py-3.5 backdrop-blur">
            <Film className="h-4 w-4 shrink-0 text-signal" />
            <div className="min-w-0 flex-1">
              <div className="eyebrow">video</div>
              {editingTitle && video ? (
                <input
                  autoFocus
                  value={titleDraft}
                  onChange={(e) => setTitleDraft(e.target.value)}
                  onBlur={saveTitle}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') saveTitle()
                    else if (e.key === 'Escape') setEditingTitle(false)
                  }}
                  className="input w-full px-2 py-1 font-display text-base font-semibold"
                />
              ) : (
                <div className="flex min-w-0 items-center gap-1.5">
                  <h3 className="truncate font-display text-base font-semibold" title={video?.title}>
                    {video?.title ?? 'Yükleniyor…'}
                  </h3>
                  {video && (
                    <button
                      onClick={() => {
                        setTitleDraft(video.title)
                        setEditingTitle(true)
                      }}
                      className="grid h-6 w-6 shrink-0 place-items-center rounded-md text-haze transition hover:bg-edge hover:text-chalk"
                      aria-label="Adı düzenle"
                      title="Adı düzenle"
                    >
                      <Pencil className="h-3.5 w-3.5" />
                    </button>
                  )}
                </div>
              )}
            </div>
            {video && <StatusChip status={video.status} />}
            <button
              onClick={onClose}
              className="grid h-8 w-8 place-items-center rounded-lg text-haze transition hover:bg-edge hover:text-chalk"
              aria-label="Kapat"
            >
              <X className="h-4 w-4" />
            </button>
          </div>

          {/* Body */}
          <div className="min-h-0 flex-1 overflow-y-auto p-5">
            {!video ? (
              <div className="grid place-items-center py-32">
                <LoaderCircle className="h-6 w-6 animate-spin text-signal" />
              </div>
            ) : (
              <div className="space-y-5">
                {toast && (
                  <div className="rounded-lg border border-signal/40 bg-signal/10 px-4 py-2 font-mono text-xs text-signal">
                    {toast}
                  </div>
                )}

                {ready ? (
                  <Card className="overflow-hidden p-3">
                    <VideoPlayer
                      videoId={video.videoId}
                      title={video.title}
                      seekTo={seek?.sec}
                      seekNonce={seek?.nonce}
                    />
                    <div className="flex flex-wrap items-center justify-end gap-2 px-1 pt-3">
                      <button onClick={() => copyVal('url', hlsUrl)} className="btn-ghost text-xs">
                        {copied === 'url' ? <Check className="h-3.5 w-3.5 text-ok" /> : <Copy className="h-3.5 w-3.5" />}
                        Oynatma URL
                      </button>
                      <button onClick={copyEmbed} className="btn-ghost text-xs">
                        {copied === 'embed' ? <Check className="h-3.5 w-3.5 text-ok" /> : <Code2 className="h-3.5 w-3.5" />}
                        Embed
                      </button>
                    </div>
                  </Card>
                ) : (
                  <Card className="grid place-items-center py-16 text-center">
                    <p className="font-mono text-xs uppercase tracking-[0.2em] text-haze">video henüz hazır değil</p>
                  </Card>
                )}

                {ready && (
                  <PosterCard
                    videoId={video.videoId}
                    posterUrl={posterUrl}
                    posterOp={ops['poster']?.status}
                    onUploaded={(url) => {
                      setPosterUrl(url)
                      flash('Poster güncellendi')
                      onChanged?.()
                    }}
                    onFrameQueued={() => {
                      flash('Poster işi sıraya alındı')
                      setOps((m) => ({
                        ...m,
                        poster: { kind: 'poster', status: 'queued', updatedAt: new Date().toISOString() },
                      }))
                      loadOps()
                    }}
                    onError={(msg) => flash(msg)}
                  />
                )}

                <div className="grid gap-5 sm:grid-cols-2">
                  <Card className="p-5">
                    <div className="eyebrow mb-3">özellikler</div>
                    <dl className="space-y-2.5 text-sm">
                      <Meta icon={<Clock className="h-3.5 w-3.5" />} label="Süre" value={formatDuration(video.length)} />
                      <Meta icon={<Layers className="h-3.5 w-3.5" />} label="Çözünürlük" value={video.availableResolutions || '—'} />
                      <Meta
                        icon={<Film className="h-3.5 w-3.5" />}
                        label="Boyut"
                        value={video.width && video.height ? `${video.width}×${video.height}` : '—'}
                      />
                      <Meta icon={<HardDrive className="h-3.5 w-3.5" />} label="Depolama" value={formatBytes(video.storageSize)} />
                    </dl>
                  </Card>

                  <Card className="p-5">
                    <div className="mb-3 flex items-center justify-between gap-2">
                      <div className="flex items-center gap-2">
                        <Activity className="h-4 w-4 text-signal" />
                        <h4 className="font-display text-sm font-semibold">İzleyici analitiği</h4>
                      </div>
                      <Link
                        to={`/videos/${video.videoId}/stats`}
                        className="inline-flex items-center gap-1 font-mono text-[10px] uppercase tracking-[0.14em] text-haze transition hover:text-signal"
                        title="Detaylı istatistikler"
                      >
                        <BarChart3 className="h-3 w-3" /> detaylı
                      </Link>
                    </div>
                    {analytics ? (
                      <div className="grid grid-cols-2 gap-2">
                        <Mini label="oturum" value={analytics.sessions} />
                        <Mini label="başlatma" value={analytics.starts} />
                        <Mini label="tamamlanma" value={analytics.completions} />
                        <Mini label="başlangıç" value={`${Math.round(analytics.avgStartupMs)}ms`} />
                        <Mini label="rebuffer" value={analytics.rebuffers} tone={analytics.rebuffers ? 'warn' : undefined} />
                        <Mini label="hata" value={analytics.errors} tone={analytics.errors ? 'bad' : undefined} />
                      </div>
                    ) : (
                      <p className="font-mono text-[11px] text-haze">Henüz veri yok.</p>
                    )}
                  </Card>
                </div>

                <IdentityCard
                  video={video}
                  hlsUrl={hlsUrl}
                  download={download}
                  copied={copied}
                  onCopy={copyVal}
                />

                <MetadataCard
                  video={video}
                  onSave={async (patch) => {
                    try {
                      await saveMeta(patch, 'İçerik güncellendi')
                    } catch (e) {
                      flash(e instanceof Error ? e.message : 'kaydedilemedi')
                    }
                  }}
                />

                {heatmap && heatmap.length > 0 && (
                  <Card className="p-5">
                    <div className="eyebrow mb-2">izlenme yoğunluğu</div>
                    <Heatmap buckets={heatmap} duration={video.length} />
                  </Card>
                )}

                <Card className="p-5">
                  <div className="mb-3 flex items-center gap-2">
                    <Users className="h-4 w-4 text-signal" />
                    <h4 className="font-display text-sm font-semibold">Bu videoyu izleyenler</h4>
                  </div>
                  {viewers && viewers.length > 0 ? (
                    <div className="overflow-hidden rounded-xl border border-edge">
                      <table className="w-full text-sm">
                        <thead>
                          <tr className="border-b border-edge bg-ink/40 text-left font-mono text-[10px] uppercase tracking-[0.18em] text-haze">
                            <th className="px-4 py-2.5 font-medium">İzleyici</th>
                            <th className="px-4 py-2.5 text-right font-medium">İlerleme</th>
                            <th className="px-4 py-2.5 text-right font-medium">Konum</th>
                            <th className="px-4 py-2.5 text-right font-medium">Durum</th>
                            <th className="px-4 py-2.5 text-right font-medium">Son izleme</th>
                          </tr>
                        </thead>
                        <tbody>
                          {viewers.map((vw) => (
                            <tr key={vw.viewerId} className="border-b border-edge/50 transition last:border-0 hover:bg-edge/20">
                              <td className="px-4 py-2.5 font-mono text-chalk">{vw.viewerId}</td>
                              <td className="px-4 py-2.5 text-right font-mono text-signal">{Math.round(vw.watchedPercent)}%</td>
                              <td className="px-4 py-2.5 text-right font-mono text-haze">{formatDuration(Math.round(vw.position))}</td>
                              <td className="px-4 py-2.5 text-right font-mono">
                                {vw.completed ? <span className="text-ok">tamamlandı</span> : <span className="text-warn">devam ediyor</span>}
                              </td>
                              <td className="px-4 py-2.5 text-right font-mono text-haze">
                                {new Date(vw.lastWatchedAt).toLocaleDateString()}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  ) : (
                    <p className="font-mono text-[11px] text-haze">Henüz izleyen yok.</p>
                  )}
                </Card>

                <Card className="p-5">
                  <ChapterCaptionEditor videoId={video.videoId} onSaved={load} />
                </Card>

                <Card className="p-5">
                  <div className="mb-3 flex items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                      <Search className="h-4 w-4 text-signal" />
                      <h4 className="font-display text-sm font-semibold">Transkriptte ara</h4>
                    </div>
                    <div className="flex items-center gap-2">
                      {ops['search_index']?.status && (
                        <OpStatusBadge status={ops['search_index'].status} />
                      )}
                      <button
                        onClick={() =>
                          runOp('reindex', () => api.reindexVideo(video.videoId), 'İndeksleme sıraya alındı')
                        }
                        disabled={
                          !ready ||
                          busy === 'reindex' ||
                          ops['search_index']?.status === 'queued' ||
                          ops['search_index']?.status === 'running'
                        }
                        className="btn-ghost text-xs"
                        title="Bu videonun arama indeksini yeniden oluştur"
                      >
                        {busy === 'reindex' || ops['search_index']?.status === 'running' ? (
                          <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <RefreshCw className="h-3.5 w-3.5" />
                        )}
                        Yeniden indeksle
                      </button>
                    </div>
                  </div>
                  <TranscriptSearch videoId={video.videoId} onSeek={seekTo} />
                </Card>

                <Card className="p-5">
                  <div className="mb-3 flex items-center gap-2">
                    <Sparkles className="h-4 w-4 text-signal" />
                    <h4 className="font-display text-sm font-semibold">Gelişmiş işlemler</h4>
                  </div>
                  <div className="space-y-2">
                    <OpButton
                      icon={<Layers className="h-3.5 w-3.5" />}
                      label="AV1 üret"
                      hint="daha küçük dosya, daha iyi sıkıştırma"
                      disabled={!ready || busy === 'av1'}
                      loading={busy === 'av1'}
                      status={ops['av1']?.status}
                      error={ops['av1']?.error}
                      onClick={() => runOp('av1', () => api.generateAV1(video.videoId), 'AV1 işi sıraya alındı')}
                    />
                    <OpButton
                      icon={<CaptionsIcon className="h-3.5 w-3.5" />}
                      label="Otomatik altyazı"
                      hint="ASR ile konuşmadan altyazı"
                      disabled={!ready || busy === 'asr'}
                      loading={busy === 'asr'}
                      status={ops['caption']?.status}
                      error={ops['caption']?.error}
                      onClick={() => runOp('asr', () => api.autoCaption(video.videoId), 'Altyazı işi sıraya alındı')}
                    />
                    <OpButton
                      icon={<Sparkles className="h-3.5 w-3.5" />}
                      label="AI içerik üret"
                      hint="transkriptten özet + etiket + bölümler"
                      disabled={!ready || busy === 'ai'}
                      loading={busy === 'ai'}
                      status={ops['ai_content']?.status}
                      error={ops['ai_content']?.error}
                      onClick={() =>
                        runOp('ai', () => api.generateAiContent(video.videoId), 'AI içerik işi sıraya alındı')
                      }
                    />
                    <OpButton
                      icon={<Lock className="h-3.5 w-3.5" />}
                      label="AES-128 şifrele"
                      hint="şifreli HLS yeniden kodlama"
                      disabled={!ready || busy === 'enc'}
                      loading={busy === 'enc'}
                      status={ops['encrypt']?.status}
                      error={ops['encrypt']?.error}
                      onClick={() => {
                        if (confirm('Video AES-128 ile yeniden kodlanacak. Devam edilsin mi?'))
                          runOp('enc', () => api.encrypt(video.videoId), 'Şifreleme işi sıraya alındı')
                      }}
                    />
                  </div>
                </Card>

                <AccessCard
                  busy={busy === 'access'}
                  onSave={(v, refs, exp) =>
                    runOp(
                      'access',
                      () =>
                        api.setAccess(video.videoId, {
                          visibility: v,
                          allowedReferrers: refs,
                          expiresAt: exp || null,
                        }),
                      'Erişim politikası güncellendi',
                    )
                  }
                />

                {video.status === 5 && video.errorMessage && (
                  <p className="font-mono text-xs text-bad">{video.errorMessage}</p>
                )}
              </div>
            )}
          </div>
        </motion.aside>
      </motion.div>
    </AnimatePresence>
  )
}

// TranscriptSearch is the in-drawer per-video search: query this video's
// transcript and click a moment to seek the player above.
function TranscriptSearch({ videoId, onSeek }: { videoId: string; onSeek: (sec: number) => void }) {
  const [q, setQ] = useState('')
  const [hits, setHits] = useState<SearchHit[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [enabled, setEnabled] = useState(true)
  const timer = useRef<number | null>(null)

  function onChange(v: string) {
    setQ(v)
    if (timer.current) window.clearTimeout(timer.current)
    if (!v.trim()) {
      setHits(null)
      return
    }
    timer.current = window.setTimeout(async () => {
      setBusy(true)
      try {
        const r = await api.search(v, videoId)
        setEnabled(r.enabled)
        setHits(r.results)
      } catch {
        setHits([])
      } finally {
        setBusy(false)
      }
    }, 350)
  }

  return (
    <div>
      <div className="relative">
        <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-haze" />
        <input
          value={q}
          onChange={(e) => onChange(e.target.value)}
          placeholder="bu videoda anlamsal ara…"
          className="input w-full pl-9 pr-9"
        />
        {busy && (
          <LoaderCircle className="absolute right-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 animate-spin text-signal" />
        )}
      </div>
      {!enabled ? (
        <p className="mt-3 font-mono text-[11px] text-haze">
          Arama etkin değil — Arama sayfasından aç ve videoyu indeksle.
        </p>
      ) : hits === null ? null : hits.length === 0 ? (
        <p className="mt-3 font-mono text-[11px] text-haze">Eşleşme yok.</p>
      ) : (
        <div className="mt-3 space-y-1.5">
          {hits.map((h, i) => (
            <button
              key={i}
              onClick={() => onSeek(h.startSec)}
              className="group flex w-full items-start gap-2.5 rounded-lg border border-edge bg-ink/40 px-3 py-2 text-left transition hover:border-signal/40"
            >
              <span className="mt-0.5 inline-flex shrink-0 items-center gap-1 rounded-md bg-signal/15 px-1.5 py-0.5 font-mono text-[10px] text-signal ring-1 ring-signal/30">
                <Play className="h-2.5 w-2.5" />
                {formatDuration(Math.round(h.startSec))}
              </span>
              <span className="min-w-0 flex-1 text-[13px] leading-snug text-haze transition group-hover:text-chalk">
                {h.snippet}
              </span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

function Meta({ icon, label, value }: { icon: ReactNode; label: string; value: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <dt className="inline-flex items-center gap-2 text-haze">
        {icon}
        {label}
      </dt>
      <dd className="font-mono text-chalk">{value}</dd>
    </div>
  )
}

function Mini({ label, value, tone }: { label: string; value: ReactNode; tone?: 'warn' | 'bad' }) {
  const color = tone === 'bad' ? 'text-bad' : tone === 'warn' ? 'text-warn' : 'text-chalk'
  return (
    <div className="rounded-lg border border-edge bg-ink/40 px-3 py-2">
      <div className={`font-mono text-lg font-semibold ${color}`}>{value}</div>
      <div className="font-mono text-[9px] uppercase tracking-[0.18em] text-haze">{label}</div>
    </div>
  )
}

// PosterCard manages the video's custom poster: preview the current one, upload
// an image, or grab a frame at a timestamp (worker job). Mirrors Bunny's
// "set thumbnail" — pick a frame or upload your own.
function PosterCard({
  videoId,
  posterUrl,
  posterOp,
  onUploaded,
  onFrameQueued,
  onError,
}: {
  videoId: string
  posterUrl: string
  posterOp?: OperationStatus
  onUploaded: (url: string) => void
  onFrameQueued: () => void
  onError: (msg: string) => void
}) {
  const fileRef = useRef<HTMLInputElement>(null)
  const [seconds, setSeconds] = useState('3')
  const [busy, setBusy] = useState<'upload' | 'frame' | null>(null)
  const frameWorking = posterOp === 'queued' || posterOp === 'running'

  async function onPick(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    e.target.value = '' // allow re-picking the same file
    if (!file) return
    setBusy('upload')
    try {
      const res = await api.uploadPoster(videoId, file)
      onUploaded(res.posterUrl)
    } catch (err) {
      onError(err instanceof Error ? err.message : 'poster yüklenemedi')
    } finally {
      setBusy(null)
    }
  }

  async function fromFrame() {
    const t = Math.max(0, parseFloat(seconds) || 0)
    setBusy('frame')
    try {
      await api.setPosterFromFrame(videoId, t)
      onFrameQueued()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'kare alınamadı')
    } finally {
      setBusy(null)
    }
  }

  return (
    <Card className="p-5">
      <div className="mb-3 flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <ImageIcon className="h-4 w-4 text-signal" />
          <h4 className="font-display text-sm font-semibold">Poster</h4>
        </div>
        {posterOp && <OpStatusBadge status={posterOp} />}
      </div>
      <div className="flex flex-col gap-4 sm:flex-row">
        <div className="aspect-video w-full overflow-hidden rounded-lg border border-edge bg-ink/60 sm:w-48">
          {posterUrl ? (
            <img src={posterUrl} alt="poster" className="h-full w-full object-cover" />
          ) : (
            <div className="grid h-full place-items-center font-mono text-[10px] uppercase tracking-[0.18em] text-haze/60">
              poster yok
            </div>
          )}
        </div>
        <div className="flex-1 space-y-3">
          <input ref={fileRef} type="file" accept="image/*" onChange={onPick} className="hidden" />
          <button
            onClick={() => fileRef.current?.click()}
            disabled={busy === 'upload'}
            className="btn-ghost w-full justify-center text-xs disabled:cursor-not-allowed disabled:opacity-40"
          >
            {busy === 'upload' ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <UploadIcon className="h-3.5 w-3.5" />}
            Görsel yükle
          </button>
          <div className="flex items-center gap-2">
            <input
              type="number"
              min={0}
              step="0.5"
              value={seconds}
              onChange={(e) => setSeconds(e.target.value)}
              className="input w-20"
              title="saniye"
            />
            <span className="font-mono text-[10px] uppercase tracking-[0.14em] text-haze">sn</span>
            <button
              onClick={fromFrame}
              disabled={busy === 'frame' || frameWorking}
              className="btn-ghost flex-1 justify-center text-xs disabled:cursor-not-allowed disabled:opacity-40"
            >
              {busy === 'frame' || frameWorking ? (
                <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Film className="h-3.5 w-3.5" />
              )}
              Bu kareyi kullan
            </button>
          </div>
          <p className="font-mono text-[10px] text-haze">
            Görsel yükle ya da videonun verilen saniyesinden bir kare al.
          </p>
        </div>
      </div>
    </Card>
  )
}

// IdentityCard lists copyable identifiers/links and a direct download button.
function IdentityCard({
  video,
  hlsUrl,
  download,
  copied,
  onCopy,
}: {
  video: Video
  hlsUrl: string
  download: { url?: string; enabled: boolean } | null
  copied: string | null
  onCopy: (key: string, text: string) => void
}) {
  return (
    <Card className="p-5">
      <div className="mb-3 flex items-center gap-2">
        <Fingerprint className="h-4 w-4 text-signal" />
        <h4 className="font-display text-sm font-semibold">Kimlik & bağlantılar</h4>
      </div>
      <div className="space-y-2">
        <CopyRow
          icon={<Fingerprint className="h-3.5 w-3.5" />}
          label="Video ID"
          value={video.videoId}
          copyKey="vid"
          copied={copied}
          onCopy={onCopy}
        />
        <CopyRow
          icon={<KeyRound className="h-3.5 w-3.5" />}
          label="Library ID"
          value={video.libraryId}
          copyKey="lib"
          copied={copied}
          onCopy={onCopy}
        />
        {hlsUrl && (
          <CopyRow
            icon={<Link2 className="h-3.5 w-3.5" />}
            label="HLS URL"
            value={hlsUrl}
            copyKey="hls"
            copied={copied}
            onCopy={onCopy}
          />
        )}
        <div className="flex items-center gap-3 rounded-lg border border-edge bg-ink/40 px-3 py-2">
          <span className="text-haze">
            <Download className="h-3.5 w-3.5" />
          </span>
          <div className="min-w-0 flex-1">
            <div className="font-mono text-[10px] uppercase tracking-[0.18em] text-haze">İndirme (MP4)</div>
            <div className="truncate font-mono text-[11px] text-chalk">
              {download === null
                ? 'yükleniyor…'
                : download.enabled
                  ? 'orijinal dosya hazır'
                  : 'indirme kapalı'}
            </div>
          </div>
          {download?.enabled && download.url && (
            <a
              href={download.url}
              download
              target="_blank"
              rel="noreferrer"
              className="btn-ghost shrink-0 text-xs"
            >
              <Download className="h-3.5 w-3.5" /> İndir
            </a>
          )}
        </div>
      </div>
    </Card>
  )
}

// CopyRow renders one labeled value with a copy-to-clipboard button.
function CopyRow({
  icon,
  label,
  value,
  copyKey,
  copied,
  onCopy,
}: {
  icon: ReactNode
  label: string
  value: string
  copyKey: string
  copied: string | null
  onCopy: (key: string, text: string) => void
}) {
  return (
    <div className="flex items-center gap-3 rounded-lg border border-edge bg-ink/40 px-3 py-2">
      <span className="text-haze">{icon}</span>
      <div className="min-w-0 flex-1">
        <div className="font-mono text-[10px] uppercase tracking-[0.18em] text-haze">{label}</div>
        <div className="truncate font-mono text-[11px] text-chalk" title={value}>
          {value}
        </div>
      </div>
      <button
        onClick={() => onCopy(copyKey, value)}
        className="grid h-7 w-7 shrink-0 place-items-center rounded-md text-haze transition hover:bg-edge hover:text-chalk"
        aria-label={`${label} kopyala`}
        title="Kopyala"
      >
        {copied === copyKey ? <Check className="h-3.5 w-3.5 text-ok" /> : <Copy className="h-3.5 w-3.5" />}
      </button>
    </div>
  )
}

// MetadataCard edits the description + tags (AI-generated, but correctable).
function MetadataCard({
  video,
  onSave,
}: {
  video: Video
  onSave: (patch: { description?: string; tags?: string[] }) => Promise<void>
}) {
  const [desc, setDesc] = useState(video.description ?? '')
  const [tags, setTags] = useState((video.tags ?? []).join(', '))
  const [busy, setBusy] = useState(false)

  // Reseed when switching to a different video so we don't show stale drafts.
  useEffect(() => {
    setDesc(video.description ?? '')
    setTags((video.tags ?? []).join(', '))
  }, [video.videoId, video.description, video.tags])

  const dirty =
    desc !== (video.description ?? '') || tags !== (video.tags ?? []).join(', ')

  async function save() {
    setBusy(true)
    try {
      await onSave({
        description: desc,
        tags: tags.split(',').map((t) => t.trim()).filter(Boolean),
      })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card className="p-5">
      <div className="mb-3 flex items-center gap-2">
        <Sparkles className="h-4 w-4 text-signal" />
        <h4 className="font-display text-sm font-semibold">İçerik (açıklama & etiketler)</h4>
      </div>
      <div className="space-y-3">
        <div>
          <label className="eyebrow mb-1 block">açıklama</label>
          <textarea
            value={desc}
            onChange={(e) => setDesc(e.target.value)}
            rows={4}
            placeholder="Video açıklaması…"
            className="input w-full resize-y leading-relaxed"
          />
        </div>
        <div>
          <label className="eyebrow mb-1 flex items-center gap-1.5">
            <Tag className="h-3 w-3" /> etiketler (virgülle)
          </label>
          <input
            value={tags}
            onChange={(e) => setTags(e.target.value)}
            placeholder="eğitim, react, başlangıç"
            className="input w-full"
          />
        </div>
        <button
          onClick={save}
          disabled={busy || !dirty}
          className="btn-primary w-full justify-center text-sm disabled:cursor-not-allowed disabled:opacity-40"
        >
          {busy ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
          Kaydet
        </button>
      </div>
    </Card>
  )
}

// Maps an advanced-op UI key to its backend video_operations.kind.
const OP_KIND: Record<string, OperationKind> = {
  av1: 'av1',
  asr: 'caption',
  ai: 'ai_content',
  enc: 'encrypt',
  reindex: 'search_index',
}

// OpStatusBadge renders a small colored pill for an operation's live status.
function OpStatusBadge({ status }: { status: OperationStatus }) {
  const map: Record<OperationStatus, { label: string; cls: string; spin?: boolean }> = {
    queued: { label: 'sırada', cls: 'border-warn/40 bg-warn/10 text-warn' },
    running: { label: 'çalışıyor', cls: 'border-signal/40 bg-signal/10 text-signal', spin: true },
    done: { label: 'bitti', cls: 'border-ok/40 bg-ok/10 text-ok' },
    failed: { label: 'başarısız', cls: 'border-bad/40 bg-bad/10 text-bad' },
  }
  const s = map[status]
  return (
    <span
      className={`inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 font-mono text-[10px] ${s.cls}`}
    >
      {status === 'running' && <LoaderCircle className="h-2.5 w-2.5 animate-spin" />}
      {status === 'done' && <Check className="h-2.5 w-2.5" />}
      {s.label}
    </span>
  )
}

function OpButton({
  icon,
  label,
  hint,
  disabled,
  loading,
  status,
  error,
  onClick,
}: {
  icon: ReactNode
  label: string
  hint: string
  disabled?: boolean
  loading?: boolean
  status?: OperationStatus
  error?: string
  onClick: () => void
}) {
  // A queued/running job locks the button regardless of the parent's flag.
  const inFlight = status === 'queued' || status === 'running'
  return (
    <button
      onClick={onClick}
      disabled={disabled || inFlight}
      className="flex w-full flex-col gap-1.5 rounded-lg border border-edge bg-ink/40 px-3 py-2.5 text-left transition hover:border-signal/40 disabled:cursor-not-allowed disabled:opacity-40"
    >
      <span className="flex w-full items-center gap-3">
        <span className="text-signal">
          {loading || status === 'running' ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : icon}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block text-sm font-medium">{label}</span>
          <span className="block truncate font-mono text-[10px] text-haze">{hint}</span>
        </span>
        {status && <OpStatusBadge status={status} />}
      </span>
      {status === 'failed' && error && (
        <span className="pl-[27px] font-mono text-[10px] text-bad">{error}</span>
      )}
    </button>
  )
}

function AccessCard({
  busy,
  onSave,
}: {
  busy: boolean
  onSave: (visibility: Visibility, referrers: string[], expiresAt: string) => void
}) {
  const [visibility, setVisibility] = useState<Visibility>('public')
  const [referrers, setReferrers] = useState('')
  const [expiresAt, setExpiresAt] = useState('')

  return (
    <Card className="p-5">
      <div className="mb-3 flex items-center gap-2">
        <ShieldCheck className="h-4 w-4 text-signal" />
        <h4 className="font-display text-sm font-semibold">Erişim politikası</h4>
      </div>
      <div className="space-y-3">
        <div>
          <label className="eyebrow mb-1 block">görünürlük</label>
          <select value={visibility} onChange={(e) => setVisibility(e.target.value as Visibility)} className="input w-full">
            <option value="public">public — herkese açık</option>
            <option value="signed">signed — imzalı URL</option>
            <option value="private">private — gizli</option>
          </select>
        </div>
        <div>
          <label className="eyebrow mb-1 block">izinli referrer'lar (virgülle)</label>
          <input
            value={referrers}
            onChange={(e) => setReferrers(e.target.value)}
            placeholder="example.com, app.example.com"
            className="input w-full"
          />
        </div>
        <div>
          <label className="eyebrow mb-1 block">son kullanma (opsiyonel)</label>
          <input type="datetime-local" value={expiresAt} onChange={(e) => setExpiresAt(e.target.value)} className="input w-full" />
        </div>
        <button
          onClick={() =>
            onSave(
              visibility,
              referrers.split(',').map((s) => s.trim()).filter(Boolean),
              expiresAt ? new Date(expiresAt).toISOString() : '',
            )
          }
          disabled={busy}
          className="btn-primary w-full justify-center text-sm"
        >
          {busy ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <ShieldCheck className="h-4 w-4" />}
          Kaydet
        </button>
      </div>
    </Card>
  )
}

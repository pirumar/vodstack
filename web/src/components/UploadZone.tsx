import { useCallback, useRef, useState } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { UploadCloud, Film, Link2, LoaderCircle } from 'lucide-react'
import { api } from '../api'
import { VideoEditorModal } from './VideoEditorModal'
import type { Edl } from '../lib/edl'

interface Upload {
  name: string
  pct: number
  phase: 'uploading' | 'queued' | 'error'
}

export function UploadZone({
  onUploaded,
  folderId,
}: {
  onUploaded: () => void
  folderId?: string | null
}) {
  const [drag, setDrag] = useState(false)
  const [uploads, setUploads] = useState<Record<string, Upload>>({})
  const [pending, setPending] = useState<File[]>([])
  const inputRef = useRef<HTMLInputElement>(null)

  const handleFiles = useCallback((files: FileList | null) => {
    if (!files) return
    const vids = Array.from(files).filter((f) => f.type.startsWith('video/'))
    if (vids.length) setPending((q) => [...q, ...vids])
  }, [])

  const startUpload = useCallback(
    async (file: File, editSpec?: Edl) => {
      const key = `${file.name}-${file.size}-${Math.random().toString(36).slice(2)}`
      setUploads((u) => ({ ...u, [key]: { name: file.name, pct: 0, phase: 'uploading' } }))
      try {
        const title = file.name.replace(/\.[^.]+$/, '')
        const { videoId } = await api.createVideo(title, folderId, editSpec)
        await api.uploadSource(videoId, file, (pct) =>
          setUploads((u) => ({ ...u, [key]: { ...u[key], pct } })),
        )
        setUploads((u) => ({ ...u, [key]: { ...u[key], phase: 'queued', pct: 100 } }))
        onUploaded()
        setTimeout(() => setUploads((u) => {
          const { [key]: _, ...rest } = u
          return rest
        }), 2500)
      } catch {
        setUploads((u) => ({ ...u, [key]: { ...u[key], phase: 'error' } }))
      }
    },
    [folderId, onUploaded],
  )

  const active = Object.entries(uploads)
  const current = pending[0]
  const advance = () => setPending((q) => q.slice(1))

  return (
    <div>
      <ImportBar onImported={onUploaded} />

      <motion.div
        onDragOver={(e) => {
          e.preventDefault()
          setDrag(true)
        }}
        onDragLeave={() => setDrag(false)}
        onDrop={(e) => {
          e.preventDefault()
          setDrag(false)
          handleFiles(e.dataTransfer.files)
        }}
        onClick={() => inputRef.current?.click()}
        animate={{
          borderColor: drag ? 'rgba(255,158,44,0.7)' : 'rgba(35,39,46,1)',
          backgroundColor: drag ? 'rgba(255,158,44,0.06)' : 'rgba(22,25,29,0.5)',
        }}
        className="group relative mt-3 cursor-pointer overflow-hidden rounded-2xl border border-dashed border-edge p-8 text-center shadow-deck"
      >
        <input
          ref={inputRef}
          type="file"
          accept="video/*"
          multiple
          hidden
          onChange={(e) => handleFiles(e.target.files)}
        />
        <div className="pointer-events-none flex flex-col items-center gap-3">
          <span className="grid h-12 w-12 place-items-center rounded-xl bg-signal/12 ring-1 ring-signal/25 transition group-hover:scale-105">
            <UploadCloud className="h-6 w-6 text-signal" />
          </span>
          <div>
            <p className="font-display text-base font-semibold">
              Videoyu buraya bırak <span className="text-haze">/ ya da seç</span>
            </p>
            <p className="mt-1 font-mono text-[11px] uppercase tracking-[0.18em] text-haze">
              mp4 · mov · mkv → otomatik abr hls
            </p>
          </div>
        </div>
      </motion.div>

      {active.length > 0 && (
        <div className="mt-4 space-y-2" data-uploads>
          {active.map(([key, u]) => (
            <div
              key={key}
              className="flex items-center gap-3 rounded-xl border border-edge bg-panel/60 px-4 py-3"
            >
              <Film className="h-4 w-4 shrink-0 text-haze" />
              <div className="min-w-0 flex-1">
                <div className="flex items-center justify-between gap-3">
                  <span className="truncate text-sm">{u.name}</span>
                  <span className="font-mono text-[11px] text-haze">
                    {u.phase === 'error'
                      ? 'hata'
                      : u.phase === 'queued'
                        ? 'kuyruğa alındı ✓'
                        : `${u.pct}%`}
                  </span>
                </div>
                <div className="mt-1.5 h-1 overflow-hidden rounded-full bg-ink">
                  <div
                    className={`h-full rounded-full transition-all duration-200 ${
                      u.phase === 'error' ? 'bg-bad' : 'bg-signal'
                    }`}
                    style={{ width: `${u.pct}%` }}
                  />
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      {current && (
        <VideoEditorModal
          key={current.name + current.size + pending.length}
          file={current}
          onConfirm={(edl) => {
            startUpload(current, edl)
            advance()
          }}
          onSkip={() => {
            startUpload(current)
            advance()
          }}
          onCancel={advance}
        />
      )}
    </div>
  )
}

// ImportBar lets an admin pull an existing video in by URL (migrate a
// Bunny/Vimeo/any MP4 into vodstack — no browser upload needed).
function ImportBar({ onImported }: { onImported: () => void }) {
  const [open, setOpen] = useState(false)
  const [url, setUrl] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!/^https?:\/\//i.test(url)) {
      setError('http(s) ile başlayan bir URL girin')
      return
    }
    setBusy(true)
    setError('')
    try {
      await api.importUrl(url.trim())
      setUrl('')
      setOpen(false)
      onImported()
    } catch {
      setError('içe aktarma başlatılamadı')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="relative flex justify-end">
      <button
        onClick={() => setOpen((o) => !o)}
        className="inline-flex items-center gap-1.5 rounded-lg border border-edge bg-panel/50 px-3 py-1.5 font-mono text-[11px] uppercase tracking-[0.14em] text-haze transition hover:border-signal/40 hover:text-chalk"
      >
        <Link2 className="h-3.5 w-3.5" /> URL'den içe aktar
      </button>

      <AnimatePresence>
        {open && (
          <motion.form
            initial={{ opacity: 0, y: -6, height: 0 }}
            animate={{ opacity: 1, y: 0, height: 'auto' }}
            exit={{ opacity: 0, y: -6, height: 0 }}
            onSubmit={submit}
            className="absolute right-5 z-10 mt-9 w-full max-w-md rounded-xl border border-edge bg-panel p-3 shadow-deck"
          >
            <label className="eyebrow mb-1.5 block">kaynak video URL'si</label>
            <div className="flex gap-2">
              <input
                autoFocus
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://.../lesson.mp4"
                className="min-w-0 flex-1 rounded-lg border border-edge bg-ink/70 px-3 py-2 font-mono text-xs outline-none focus:border-signal/60"
              />
              <button
                type="submit"
                disabled={busy || !url}
                className="inline-flex items-center gap-1.5 rounded-lg bg-signal px-3 py-2 text-xs font-semibold text-ink transition hover:bg-signal-soft disabled:opacity-40"
              >
                {busy ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : 'Çek'}
              </button>
            </div>
            {error && <p className="mt-2 font-mono text-[10px] text-bad">⚠ {error}</p>}
          </motion.form>
        )}
      </AnimatePresence>
    </div>
  )
}

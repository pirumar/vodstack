import { useEffect, useState } from 'react'
import { Plus, Trash2, Captions, ListTree, Save, Upload, LoaderCircle } from 'lucide-react'
import { api, type Caption, type Chapter } from '../api'

function parseTime(s: string): number {
  const parts = s.trim().split(':').map((p) => parseInt(p, 10))
  if (parts.some((n) => isNaN(n))) return NaN
  return parts.reduce((acc, n) => acc * 60 + n, 0)
}

function fmtTime(sec: number): string {
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = Math.floor(sec % 60)
  const pad = (n: number) => String(n).padStart(2, '0')
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`
}

interface Row {
  time: string
  title: string
}

// Inline chapters + captions editor (the former ManageModal body, without the
// modal chrome) for the video detail page.
export function ChapterCaptionEditor({
  videoId,
  onSaved,
}: {
  videoId: string
  onSaved?: () => void
}) {
  const [rows, setRows] = useState<Row[]>([])
  const [captions, setCaptions] = useState<Caption[]>([])
  const [savingCh, setSavingCh] = useState(false)
  const [capLang, setCapLang] = useState('tr')
  const [capLabel, setCapLabel] = useState('Türkçe')
  const [uploading, setUploading] = useState(false)
  const [msg, setMsg] = useState('')

  useEffect(() => {
    setMsg('')
    api
      .getVideo(videoId)
      .then((v) => {
        setCaptions(v.captions ?? [])
        const fresh = ((v as { chapters?: Chapter[] }).chapters) ?? []
        setRows(fresh.map((c) => ({ time: fmtTime(c.start), title: c.title })))
      })
      .catch(() => {})
  }, [videoId])

  async function saveChapters() {
    setSavingCh(true)
    setMsg('')
    try {
      const chapters: Chapter[] = rows
        .map((r) => ({ start: parseTime(r.time), title: r.title.trim() }))
        .filter((c) => !isNaN(c.start) && c.title)
      await api.setChapters(videoId, chapters)
      setMsg('Bölümler kaydedildi ✓')
      onSaved?.()
    } catch {
      setMsg('Bölümler kaydedilemedi')
    } finally {
      setSavingCh(false)
    }
  }

  async function uploadCaption(file: File) {
    setUploading(true)
    setMsg('')
    try {
      const content = await file.text()
      await api.uploadCaption(videoId, capLang.trim().toLowerCase(), capLabel.trim() || capLang, content)
      const v = await api.getVideo(videoId)
      setCaptions(v.captions ?? [])
      setMsg('Altyazı eklendi ✓')
      onSaved?.()
    } catch {
      setMsg('Altyazı yüklenemedi')
    } finally {
      setUploading(false)
    }
  }

  async function removeCaption(lang: string) {
    await api.deleteCaption(videoId, lang).catch(() => {})
    setCaptions((cs) => cs.filter((c) => c.lang !== lang))
    onSaved?.()
  }

  return (
    <div className="space-y-8">
      {/* Chapters */}
      <section>
        <div className="mb-3 flex items-center gap-2">
          <ListTree className="h-4 w-4 text-signal" />
          <h4 className="font-display text-sm font-semibold">Bölümler (YouTube tarzı)</h4>
        </div>
        <div className="space-y-2">
          {rows.map((r, i) => (
            <div key={i} className="flex items-center gap-2">
              <input
                value={r.time}
                onChange={(e) => setRows((rs) => rs.map((x, j) => (j === i ? { ...x, time: e.target.value } : x)))}
                placeholder="5:30"
                className="w-20 rounded-lg border border-edge bg-ink/70 px-2.5 py-2 text-center font-mono text-xs outline-none focus:border-signal/60"
              />
              <input
                value={r.title}
                onChange={(e) => setRows((rs) => rs.map((x, j) => (j === i ? { ...x, title: e.target.value } : x)))}
                placeholder="Bölüm başlığı"
                className="min-w-0 flex-1 rounded-lg border border-edge bg-ink/70 px-3 py-2 text-sm outline-none focus:border-signal/60"
              />
              <button
                onClick={() => setRows((rs) => rs.filter((_, j) => j !== i))}
                className="rounded-lg p-2 text-haze transition hover:bg-bad/20 hover:text-bad"
              >
                <Trash2 className="h-3.5 w-3.5" />
              </button>
            </div>
          ))}
        </div>
        <div className="mt-3 flex items-center gap-2">
          <button
            onClick={() => setRows((rs) => [...rs, { time: '', title: '' }])}
            className="inline-flex items-center gap-1.5 rounded-lg border border-edge bg-panel/60 px-3 py-1.5 text-xs text-haze transition hover:border-signal/40 hover:text-chalk"
          >
            <Plus className="h-3.5 w-3.5" /> Bölüm ekle
          </button>
          <button
            onClick={saveChapters}
            disabled={savingCh}
            className="ml-auto inline-flex items-center gap-1.5 rounded-lg bg-signal px-3 py-1.5 text-xs font-semibold text-ink transition hover:bg-signal-soft disabled:opacity-40"
          >
            {savingCh ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
            Bölümleri kaydet
          </button>
        </div>
      </section>

      {/* Captions */}
      <section>
        <div className="mb-3 flex items-center gap-2">
          <Captions className="h-4 w-4 text-signal" />
          <h4 className="font-display text-sm font-semibold">Altyazılar</h4>
        </div>
        {captions.length > 0 ? (
          <div className="mb-3 space-y-2">
            {captions.map((c) => (
              <div key={c.lang} className="flex items-center gap-3 rounded-lg border border-edge bg-panel/50 px-3 py-2">
                <span className="rounded bg-ink px-2 py-0.5 font-mono text-[10px] uppercase text-haze">{c.lang}</span>
                <span className="text-sm">{c.label}</span>
                <button
                  onClick={() => removeCaption(c.lang)}
                  className="ml-auto rounded-lg p-1.5 text-haze transition hover:bg-bad/20 hover:text-bad"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </div>
            ))}
          </div>
        ) : (
          <p className="mb-3 font-mono text-[11px] text-haze">Henüz altyazı yok.</p>
        )}

        <div className="flex flex-wrap items-end gap-2 rounded-xl border border-edge bg-panel/40 p-3">
          <div>
            <label className="eyebrow mb-1 block">dil kodu</label>
            <input
              value={capLang}
              onChange={(e) => setCapLang(e.target.value)}
              placeholder="tr"
              className="w-20 rounded-lg border border-edge bg-ink/70 px-2.5 py-2 font-mono text-xs outline-none focus:border-signal/60"
            />
          </div>
          <div className="min-w-0 flex-1">
            <label className="eyebrow mb-1 block">etiket</label>
            <input
              value={capLabel}
              onChange={(e) => setCapLabel(e.target.value)}
              placeholder="Türkçe"
              className="w-full rounded-lg border border-edge bg-ink/70 px-3 py-2 text-sm outline-none focus:border-signal/60"
            />
          </div>
          <label className="inline-flex cursor-pointer items-center gap-1.5 rounded-lg bg-signal px-3 py-2 text-xs font-semibold text-ink transition hover:bg-signal-soft">
            {uploading ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Upload className="h-3.5 w-3.5" />}
            .vtt / .srt yükle
            <input
              type="file"
              accept=".vtt,.srt,text/vtt"
              hidden
              onChange={(e) => e.target.files?.[0] && uploadCaption(e.target.files[0])}
            />
          </label>
        </div>
      </section>

      {msg && <p className="font-mono text-xs text-ok">{msg}</p>}
    </div>
  )
}

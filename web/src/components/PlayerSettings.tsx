import { useEffect, useState } from 'react'
import { Save, LoaderCircle, RotateCcw, Eye } from 'lucide-react'
import { api, type PlayerConfig } from '../api'

const CONTROL_LABELS: Record<string, string> = {
  playPause: 'Oynat / Duraklat',
  seekBackward: '10sn Geri',
  seekForward: '10sn İleri',
  mute: 'Sessiz',
  volume: 'Ses',
  currentTime: 'Geçen Süre',
  duration: 'Süre',
  progress: 'İlerleme',
  captions: 'Altyazılar',
  settings: 'Ayarlar',
  pip: 'Resim içinde Resim',
  airplay: 'AirPlay',
  chromecast: 'Chromecast',
  fullscreen: 'Tam Ekran',
  bigPlayButton: 'Büyük Oynat Düğmesi',
}

const LANGUAGES: { code: string; label: string }[] = [
  { code: 'tr', label: 'Türkçe' },
  { code: 'en', label: 'English' },
  { code: 'de', label: 'Deutsch' },
  { code: 'fr', label: 'Français' },
  { code: 'es', label: 'Español' },
]

const FONTS = ['Roboto', 'Inter', 'Open Sans', 'Lato', 'Montserrat', 'system-ui']
const SPEED_PRESETS = [0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2, 3, 4]

export function PlayerSettings({
  embedBaseUrl,
  libraryId,
  previewVideoId,
}: {
  embedBaseUrl: string
  libraryId: string
  previewVideoId?: string
}) {
  const [cfg, setCfg] = useState<PlayerConfig | null>(null)
  const [allControls, setAllControls] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [customSpeed, setCustomSpeed] = useState('')
  const [previewKey, setPreviewKey] = useState(0)

  useEffect(() => {
    api.getPlayerSettings().then((r) => {
      setCfg(r.config)
      setAllControls(r.allControls)
    })
  }, [])

  if (!cfg) {
    return (
      <div className="grid place-items-center py-24">
        <LoaderCircle className="h-6 w-6 animate-spin text-signal" />
      </div>
    )
  }

  const patch = (p: Partial<PlayerConfig>) => setCfg((c) => (c ? { ...c, ...p } : c))

  function toggleControl(key: string) {
    if (!cfg) return
    const on = cfg.controls.includes(key)
    patch({ controls: on ? cfg.controls.filter((k) => k !== key) : [...cfg.controls, key] })
  }

  function toggleSpeed(s: number) {
    if (!cfg) return
    const on = cfg.playbackSpeeds.includes(s)
    const next = on ? cfg.playbackSpeeds.filter((x) => x !== s) : [...cfg.playbackSpeeds, s].sort((a, b) => a - b)
    patch({ playbackSpeeds: next.length ? next : [1] })
  }

  function addCustomSpeed() {
    const v = parseFloat(customSpeed.replace(',', '.'))
    if (!isFinite(v) || v < 0.1 || v > 4 || cfg!.playbackSpeeds.includes(v)) return
    patch({ playbackSpeeds: [...cfg!.playbackSpeeds, v].sort((a, b) => a - b) })
    setCustomSpeed('')
  }

  async function save() {
    if (!cfg) return
    setBusy(true)
    setSaved(false)
    try {
      const updated = await api.setPlayerSettings(cfg)
      setCfg(updated)
      setSaved(true)
      setPreviewKey((k) => k + 1) // reload preview iframe with new settings
      setTimeout(() => setSaved(false), 2000)
    } catch {
      /* surfaced via lack of saved state */
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid gap-8 lg:grid-cols-[1fr_minmax(320px,420px)]">
      {/* Settings form */}
      <div className="space-y-8">
        <Section title="Genel">
          <Field label="Player UI Dili">
            <select
              value={cfg.language}
              onChange={(e) => patch({ language: e.target.value })}
              className="input"
            >
              {LANGUAGES.map((l) => (
                <option key={l.code} value={l.code}>
                  {l.label} ({l.code})
                </option>
              ))}
            </select>
          </Field>
          <Field label="Yazı Tipi">
            <select value={cfg.fontFamily} onChange={(e) => patch({ fontFamily: e.target.value })} className="input">
              {FONTS.map((f) => (
                <option key={f} value={f}>
                  {f}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Birincil Renk">
            <ColorInput value={cfg.primaryColor} onChange={(v) => patch({ primaryColor: v })} />
          </Field>
        </Section>

        <Section title="Altyazı Görünümü">
          <Field label="Metin Rengi">
            <ColorInput value={cfg.captions.color} onChange={(v) => patch({ captions: { ...cfg.captions, color: v } })} />
          </Field>
          <Field label="Arka Plan">
            <ColorInput
              value={cfg.captions.background}
              onChange={(v) => patch({ captions: { ...cfg.captions, background: v } })}
            />
          </Field>
          <Field label="Yazı Boyutu (px)">
            <input
              type="number"
              min={8}
              max={96}
              value={cfg.captions.fontSize}
              onChange={(e) => patch({ captions: { ...cfg.captions, fontSize: parseInt(e.target.value) || 24 } })}
              className="input w-24"
            />
          </Field>
        </Section>

        <Section title="Player Kontrolleri">
          <div className="grid grid-cols-2 gap-1.5 sm:grid-cols-3">
            {(allControls.length ? allControls : Object.keys(CONTROL_LABELS)).map((key) => (
              <label
                key={key}
                className="flex cursor-pointer items-center gap-2 rounded-lg border border-edge bg-panel/50 px-3 py-2 text-xs"
              >
                <input
                  type="checkbox"
                  checked={cfg.controls.includes(key)}
                  onChange={() => toggleControl(key)}
                  className="accent-signal"
                />
                <span className="truncate">{CONTROL_LABELS[key] ?? key}</span>
              </label>
            ))}
          </div>
        </Section>

        <Section title="Oynatma Hızı Seçenekleri">
          <div className="flex flex-wrap gap-1.5">
            {Array.from(new Set([...SPEED_PRESETS, ...cfg.playbackSpeeds]))
              .sort((a, b) => a - b)
              .map((s) => (
                <button
                  key={s}
                  onClick={() => toggleSpeed(s)}
                  className={`rounded-full px-3 py-1.5 font-mono text-xs transition ${
                    cfg.playbackSpeeds.includes(s)
                      ? 'bg-signal text-ink'
                      : 'border border-edge bg-panel/50 text-haze hover:text-chalk'
                  }`}
                >
                  x{s}
                </button>
              ))}
          </div>
          <div className="mt-3 flex items-center gap-2">
            <input
              value={customSpeed}
              onChange={(e) => setCustomSpeed(e.target.value)}
              placeholder="örn. 1.15"
              className="input w-28"
              onKeyDown={(e) => e.key === 'Enter' && addCustomSpeed()}
            />
            <button onClick={addCustomSpeed} className="btn-ghost text-xs">
              Özel hız ekle
            </button>
          </div>
          <Field label="Varsayılan Hız">
            <select
              value={cfg.defaultSpeed}
              onChange={(e) => patch({ defaultSpeed: parseFloat(e.target.value) })}
              className="input w-28"
            >
              {cfg.playbackSpeeds.map((s) => (
                <option key={s} value={s}>
                  x{s}
                </option>
              ))}
            </select>
          </Field>
        </Section>

        <Section title="Davranış">
          <Toggle
            label="Watchtime heatmap göster"
            hint="Yeterli veri toplandığında ilerleme çubuğu üstünde popüler bölümleri gösterir."
            checked={cfg.showHeatmap}
            onChange={(v) => patch({ showHeatmap: v })}
          />
          <Toggle
            label="Kaldığı yerden devam (resume)"
            hint="İzleyici döndüğünde videoyu bıraktığı yerden başlatır."
            checked={cfg.resumePlayback}
            onChange={(v) => patch({ resumePlayback: v })}
          />
          <Toggle
            label="Kompakt kontroller"
            hint="Daha küçük kontrol arayüzü, daha fazla video görünürlüğü."
            checked={cfg.compactControls}
            onChange={(v) => patch({ compactControls: v })}
          />
        </Section>

        <Section title="Özel CSS (head)">
          <textarea
            value={cfg.customCSS}
            onChange={(e) => patch({ customCSS: e.target.value })}
            placeholder="media-player { --media-controls-color: #fff; }"
            rows={5}
            className="input w-full font-mono text-xs"
          />
        </Section>

        <div className="flex items-center gap-3">
          <button onClick={save} disabled={busy} className="btn-primary">
            {busy ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
            Kaydet
          </button>
          {saved && <span className="font-mono text-xs text-ok">kaydedildi ✓</span>}
          <button onClick={() => setPreviewKey((k) => k + 1)} className="btn-ghost ml-auto text-xs">
            <RotateCcw className="h-3.5 w-3.5" /> Önizlemeyi yenile
          </button>
        </div>
      </div>

      {/* Live preview */}
      <div className="lg:sticky lg:top-24 lg:self-start">
        <div className="mb-2 flex items-center gap-2">
          <Eye className="h-4 w-4 text-signal" />
          <h3 className="font-display text-sm font-semibold">Canlı Önizleme</h3>
        </div>
        {previewVideoId ? (
          <div className="overflow-hidden rounded-xl border border-edge bg-ink">
            <iframe
              key={previewKey}
              src={`${embedBaseUrl}/embed/${libraryId}/${previewVideoId}?preview=1`}
              className="aspect-video w-full"
              allow="autoplay; fullscreen; picture-in-picture"
              allowFullScreen
            />
          </div>
        ) : (
          <div className="grid aspect-video place-items-center rounded-xl border border-dashed border-edge bg-panel/30 px-6 text-center">
            <p className="font-mono text-[11px] uppercase tracking-[0.16em] text-haze">
              önizleme için hazır bir video gerekli
            </p>
          </div>
        )}
        <p className="mt-2 font-mono text-[10px] text-haze">
          Değişiklikleri görmek için Kaydet'e bas; önizleme otomatik yenilenir.
        </p>
      </div>
    </div>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section>
      <h3 className="mb-3 font-mono text-[11px] uppercase tracking-[0.18em] text-signal">{title}</h3>
      <div className="space-y-3">{children}</div>
    </section>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4">
      <label className="text-sm text-haze">{label}</label>
      {children}
    </div>
  )
}

function ColorInput({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <div className="flex items-center gap-2">
      <input
        type="color"
        value={/^#[0-9a-fA-F]{6}$/.test(value) ? value : '#000000'}
        onChange={(e) => onChange(e.target.value)}
        className="h-8 w-10 cursor-pointer rounded border border-edge bg-transparent"
      />
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="input w-24 font-mono text-xs"
      />
    </div>
  )
}

function Toggle({
  label,
  hint,
  checked,
  onChange,
}: {
  label: string
  hint?: string
  checked: boolean
  onChange: (v: boolean) => void
}) {
  return (
    <label className="flex cursor-pointer items-start gap-3">
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} className="mt-0.5 accent-signal" />
      <span>
        <span className="text-sm">{label}</span>
        {hint && <span className="block font-mono text-[10px] text-haze">{hint}</span>}
      </span>
    </label>
  )
}

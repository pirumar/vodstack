import { useEffect, useRef, useState } from 'react'
import { LoaderCircle, Check, Film, Upload, Trash2 } from 'lucide-react'
import { api, type EncodingConfig } from '../api'
import { Card } from './ui'

const RES_LABEL: Record<string, string> = {
  '240p': '240p',
  '360p': '360p',
  '480p': '480p',
  '720p': '720p (HD)',
  '1080p': '1080p (FHD)',
  '1440p': '1440p (QHD)',
  '2160p': '2160p (4K)',
}
const CODEC_LABEL: Record<string, string> = {
  h264: 'H.264 (AVC)',
  hevc: 'H.265 (HEVC)',
  av1: 'AV1',
  vp9: 'VP9',
}
const POSITIONS = [
  ['topLeft', 'Sol üst'],
  ['topRight', 'Sağ üst'],
  ['bottomLeft', 'Sol alt'],
  ['bottomRight', 'Sağ alt'],
  ['center', 'Orta'],
] as const

// Library-wide encoding defaults (Bunny "Encoding Tier" controls). New uploads
// inherit these; in-flight/finished videos keep the snapshot from their creation.
export function EncodingSettings() {
  const [cfg, setCfg] = useState<EncodingConfig | null>(null)
  const [allRes, setAllRes] = useState<string[]>([])
  const [allCodecs, setAllCodecs] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [err, setErr] = useState('')
  const fileRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    api
      .getEncodingSettings()
      .then((r) => {
        setCfg(r.config)
        setAllRes(r.allResolutions)
        setAllCodecs(r.allCodecs)
      })
      .catch(() => {})
  }, [])

  if (!cfg) {
    return (
      <Card className="grid place-items-center p-10">
        <LoaderCircle className="h-5 w-5 animate-spin text-signal" />
      </Card>
    )
  }

  const patch = (p: Partial<EncodingConfig>) => setCfg((c) => (c ? { ...c, ...p } : c))
  const patchWm = (p: Partial<EncodingConfig['watermark']>) =>
    setCfg((c) => (c ? { ...c, watermark: { ...c.watermark, ...p } } : c))

  function toggleIn(list: string[], value: string): string[] {
    return list.includes(value) ? list.filter((v) => v !== value) : [...list, value]
  }

  async function save() {
    if (!cfg) return
    setBusy(true)
    setSaved(false)
    setErr('')
    try {
      const updated = await api.setEncodingSettings(cfg)
      setCfg(updated)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'kayıt başarısız')
    } finally {
      setBusy(false)
    }
  }

  async function onWatermarkFile(file: File) {
    setErr('')
    try {
      setCfg(await api.uploadWatermark(file))
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'filigran yüklenemedi')
    }
  }

  async function removeWatermark() {
    try {
      setCfg(await api.deleteWatermark())
    } catch {
      /* ignore */
    }
  }

  const heavy = cfg.codecs.some((c) => c !== 'h264') || cfg.resolutions.some((r) => r === '1440p' || r === '2160p')

  return (
    <Card className="space-y-6 p-5">
      <div className="flex items-center gap-2">
        <Film className="h-4 w-4 text-signal" />
        <h4 className="font-display text-sm font-semibold">Kodlama ayarları</h4>
      </div>

      {/* Resolutions */}
      <div>
        <label className="eyebrow mb-2 block">çözünürlükler</label>
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
          {allRes.map((r) => (
            <Checkbox
              key={r}
              label={RES_LABEL[r] ?? r}
              checked={cfg.resolutions.includes(r)}
              onChange={() => patch({ resolutions: toggleIn(cfg.resolutions, r) })}
            />
          ))}
        </div>
        <p className="mt-1 font-mono text-[10px] text-haze">
          kaynaktan büyük çözünürlükler atlanır; daha fazla çözünürlük daha fazla depolama demektir
        </p>
      </div>

      {/* Codecs */}
      <div>
        <label className="eyebrow mb-2 block">çıktı codec'leri</label>
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
          {allCodecs.map((c) => (
            <Checkbox
              key={c}
              label={CODEC_LABEL[c] ?? c}
              checked={cfg.codecs.includes(c)}
              disabled={c === 'h264'} // always produced
              onChange={() => patch({ codecs: toggleIn(cfg.codecs, c) })}
            />
          ))}
        </div>
      </div>

      {heavy && (
        <p className="rounded-lg border border-warn/40 bg-warn/10 px-3 py-2 font-mono text-[11px] text-warn">
          HEVC/AV1/VP9 ve 1440p/2160p, GPU'suz tek sunucuda CPU yoğundur ve yavaş kodlanır — bulk
          kuyrukta H.264 oynatımını engellemeden çalışır.
        </p>
      )}

      {/* Delivery toggles */}
      <div className="space-y-3">
        <Toggle
          label="MP4 fallback (≤1080p)"
          hint="HLS desteklemeyen eski cihazlar için ilerlemeli MP4 üretir"
          checked={cfg.mp4Fallback}
          onChange={(v) => patch({ mp4Fallback: v })}
        />
        <Toggle
          label="Orijinal indirmeye izin ver"
          hint="orijinal dosya için imzalı indirme bağlantısı sunar"
          checked={cfg.allowDownload}
          onChange={(v) => patch({ allowDownload: v })}
        />
        <Toggle
          label="Early-Play"
          hint="kodlama biterken orijinali oynatır — orijinal dosyayı herkese açık hale getirir"
          checked={cfg.earlyPlay}
          onChange={(v) => patch({ earlyPlay: v })}
        />
        <Toggle
          label="Çoklu ses kanalı"
          hint="kaynaktaki tüm ses parçalarını ayrı dil olarak kodlar"
          checked={cfg.multiAudio}
          onChange={(v) => patch({ multiAudio: v })}
        />
      </div>

      {/* Watermark */}
      <div className="space-y-3 rounded-xl border border-edge/60 p-4">
        <Toggle
          label="Filigran"
          hint="yüklenen videolara filigran gömülür; kodlamadan sonra kaldırılamaz"
          checked={cfg.watermark.enabled}
          onChange={(v) => patchWm({ enabled: v })}
        />
        {cfg.watermark.enabled && (
          <>
            <div className="flex items-center gap-3">
              <button
                type="button"
                onClick={() => fileRef.current?.click()}
                className="btn-ghost text-xs"
              >
                <Upload className="h-3.5 w-3.5" /> Görsel yükle (PNG)
              </button>
              {cfg.watermark.object && (
                <>
                  <span className="font-mono text-[11px] text-haze">yüklendi ✓</span>
                  <button
                    type="button"
                    onClick={removeWatermark}
                    className="text-bad hover:opacity-80"
                    title="Filigranı kaldır"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </>
              )}
              <input
                ref={fileRef}
                type="file"
                accept="image/png,image/jpeg,image/webp"
                className="hidden"
                onChange={(e) => e.target.files?.[0] && onWatermarkFile(e.target.files[0])}
              />
            </div>
            <div className="grid grid-cols-3 gap-3">
              <div>
                <label className="eyebrow mb-1 block">konum</label>
                <select
                  value={cfg.watermark.position}
                  onChange={(e) => patchWm({ position: e.target.value })}
                  className="input w-full"
                >
                  {POSITIONS.map(([id, label]) => (
                    <option key={id} value={id}>
                      {label}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label className="eyebrow mb-1 block">opaklık</label>
                <input
                  type="number"
                  step={0.1}
                  min={0.1}
                  max={1}
                  value={cfg.watermark.opacity}
                  onChange={(e) => patchWm({ opacity: Number(e.target.value) })}
                  className="input w-full"
                />
              </div>
              <div>
                <label className="eyebrow mb-1 block">kenar boşluğu (px)</label>
                <input
                  type="number"
                  min={0}
                  max={512}
                  value={cfg.watermark.margin}
                  onChange={(e) => patchWm({ margin: Number(e.target.value) })}
                  className="input w-full"
                />
              </div>
            </div>
          </>
        )}
      </div>

      {err && <p className="font-mono text-xs text-bad">{err}</p>}

      <button onClick={save} disabled={busy} className="btn-primary w-full justify-center text-sm">
        {busy ? <LoaderCircle className="h-4 w-4 animate-spin" /> : saved ? <Check className="h-4 w-4" /> : null}
        {saved ? 'Kaydedildi' : 'Kaydet'}
      </button>
    </Card>
  )
}

function Checkbox({
  label,
  checked,
  disabled,
  onChange,
}: {
  label: string
  checked: boolean
  disabled?: boolean
  onChange: () => void
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onChange}
      className={`flex items-center gap-2 rounded-lg border px-3 py-2 text-left text-xs transition ${
        checked ? 'border-signal/60 bg-signal/10' : 'border-edge bg-transparent'
      } ${disabled ? 'cursor-not-allowed opacity-60' : 'hover:border-signal/40'}`}
    >
      <span
        className={`grid h-4 w-4 shrink-0 place-items-center rounded border ${
          checked ? 'border-signal bg-signal text-ink' : 'border-edge'
        }`}
      >
        {checked && <Check className="h-3 w-3" />}
      </span>
      <span className="min-w-0 truncate font-medium">{label}</span>
    </button>
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
    <button
      type="button"
      onClick={() => onChange(!checked)}
      className="flex w-full items-center gap-3 text-left"
    >
      <span
        className={`relative h-5 w-9 shrink-0 rounded-full transition ${checked ? 'bg-signal' : 'bg-edge'}`}
      >
        <span
          className={`absolute top-0.5 h-4 w-4 rounded-full bg-chalk transition-all ${checked ? 'left-4' : 'left-0.5'}`}
        />
      </span>
      <span className="min-w-0">
        <span className="block text-sm font-medium">{label}</span>
        {hint && <span className="block font-mono text-[10px] text-haze">{hint}</span>}
      </span>
    </button>
  )
}

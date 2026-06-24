import { useEffect, useState } from 'react'
import { LoaderCircle, Check, KeyRound, Cpu } from 'lucide-react'
import { api, type SearchSettings as Settings, type SearchProvider } from '../api'
import { Card } from './ui'

// Library-wide in-video-search settings: master switch, embedding provider
// (local / Gemini / Voyage), model, API key (write-only), chunk window, and the
// in-player search toggle. Mirrors the PlayerSettings save pattern.
export function SearchSettings({ onSaved }: { onSaved?: () => void }) {
  const [cfg, setCfg] = useState<Settings | null>(null)
  const [providers, setProviders] = useState<SearchProvider[]>([])
  const [apiKey, setApiKey] = useState('') // write-only; blank keeps the stored key
  const [initialProvider, setInitialProvider] = useState('')
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [err, setErr] = useState('')

  useEffect(() => {
    api
      .getSearchSettings()
      .then((r) => {
        setCfg(r.config)
        setProviders(r.providers)
        setInitialProvider(r.config.provider)
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

  const provMeta = providers.find((p) => p.id === cfg.provider)
  const needsKey = provMeta?.needsApiKey ?? false
  const needsBaseUrl = provMeta?.needsBaseUrl ?? false
  const providerChanged = cfg.provider !== initialProvider
  const patch = (p: Partial<Settings>) => setCfg((c) => (c ? { ...c, ...p } : c))

  function onProviderChange(id: string) {
    const meta = providers.find((p) => p.id === id)
    patch({ provider: id, model: meta?.defaultModel ?? cfg!.model })
    setApiKey('')
  }

  async function save() {
    if (!cfg) return
    setBusy(true)
    setSaved(false)
    setErr('')
    try {
      const updated = await api.setSearchSettings({
        enabled: cfg.enabled,
        provider: cfg.provider,
        model: cfg.model,
        apiKey: apiKey || undefined,
        baseUrl: cfg.baseUrl,
        chunkSeconds: cfg.chunkSeconds,
        showInPlayer: cfg.showInPlayer,
      })
      setCfg(updated)
      setInitialProvider(updated.provider)
      setApiKey('')
      setSaved(true)
      onSaved?.()
      setTimeout(() => setSaved(false), 2000)
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'kayıt başarısız')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card className="space-y-5 p-5">
      <div className="flex items-center gap-2">
        <Cpu className="h-4 w-4 text-signal" />
        <h4 className="font-display text-sm font-semibold">Arama ayarları</h4>
      </div>

      {/* Master switch */}
      <Toggle
        label="Video içinde aramayı etkinleştir"
        hint="altyazı transkriptleri indekslenir ve aranabilir hale gelir"
        checked={cfg.enabled}
        onChange={(v) => patch({ enabled: v })}
      />

      {/* Provider */}
      <div>
        <label className="eyebrow mb-1 block">embedding sağlayıcı</label>
        <select
          value={cfg.provider}
          onChange={(e) => onProviderChange(e.target.value)}
          className="input w-full"
        >
          {providers.map((p) => (
            <option key={p.id} value={p.id}>
              {p.id === 'local'
                ? 'Yerel (CPU, ücretsiz)'
                : p.id === 'gemini'
                  ? 'Google Gemini'
                  : p.id === 'voyage'
                    ? 'Voyage AI'
                    : 'Özel (OpenAI uyumlu / OpenWebUI)'}
            </option>
          ))}
        </select>
      </div>

      {/* Base URL (custom provider only) */}
      {needsBaseUrl && (
        <div>
          <label className="eyebrow mb-1 block">endpoint URL</label>
          <input
            value={cfg.baseUrl}
            onChange={(e) => patch({ baseUrl: e.target.value })}
            placeholder="https://host:port/api/v1/embeddings"
            className="input w-full font-mono text-xs"
          />
        </div>
      )}

      {/* Model */}
      <div>
        <label className="eyebrow mb-1 block">model</label>
        <input
          value={cfg.model}
          onChange={(e) => patch({ model: e.target.value })}
          className="input w-full font-mono text-xs"
        />
      </div>

      {/* API key (remote providers only) */}
      {needsKey && (
        <div>
          <label className="eyebrow mb-1 flex items-center gap-1.5">
            <KeyRound className="h-3 w-3" /> API anahtarı
          </label>
          <input
            type="password"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            placeholder={cfg.hasApiKey ? '•••••••• (kayıtlı — değiştirmek için yaz)' : 'API anahtarını yapıştır'}
            className="input w-full font-mono text-xs"
          />
        </div>
      )}

      {/* Chunk window */}
      <div>
        <label className="eyebrow mb-1 block">parça uzunluğu (saniye)</label>
        <input
          type="number"
          min={10}
          max={120}
          value={cfg.chunkSeconds}
          onChange={(e) => patch({ chunkSeconds: Number(e.target.value) })}
          className="input w-32"
        />
      </div>

      {/* In-player search */}
      <Toggle
        label="Oynatıcıda arama kutusu göster"
        hint="izleyiciler embed oynatıcıda video içinde arayabilir"
        checked={cfg.showInPlayer}
        onChange={(v) => patch({ showInPlayer: v })}
      />

      {providerChanged && (
        <p className="rounded-lg border border-warn/40 bg-warn/10 px-3 py-2 font-mono text-[11px] text-warn">
          Sağlayıcı değişti — mevcut videoların yeniden indekslenmesi gerekir (farklı modellerin
          vektörleri kıyaslanamaz). Kaydettikten sonra her videoda "Yeniden indeksle" çalıştır.
        </p>
      )}

      {err && <p className="font-mono text-xs text-bad">{err}</p>}

      <button onClick={save} disabled={busy} className="btn-primary w-full justify-center text-sm">
        {busy ? <LoaderCircle className="h-4 w-4 animate-spin" /> : saved ? <Check className="h-4 w-4" /> : null}
        {saved ? 'Kaydedildi' : 'Kaydet'}
      </button>
    </Card>
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

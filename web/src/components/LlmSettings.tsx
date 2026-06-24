import { useEffect, useState } from 'react'
import { LoaderCircle, Check, KeyRound, Sparkles } from 'lucide-react'
import { api, type LlmSettings as Settings } from '../api'
import { Card } from './ui'

// Library-wide LLM router settings for AI content generation (description, tags,
// chapters from the transcript). OpenAI-compatible /chat/completions endpoint.
// Mirrors the SearchSettings save pattern.
export function LlmSettings({ onSaved }: { onSaved?: () => void }) {
  const [cfg, setCfg] = useState<Settings | null>(null)
  const [apiKey, setApiKey] = useState('') // write-only; blank keeps the stored key
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [err, setErr] = useState('')

  useEffect(() => {
    api
      .getLlmSettings()
      .then((r) => setCfg(r.config))
      .catch(() => {})
  }, [])

  if (!cfg) {
    return (
      <Card className="grid place-items-center p-10">
        <LoaderCircle className="h-5 w-5 animate-spin text-signal" />
      </Card>
    )
  }

  const patch = (p: Partial<Settings>) => setCfg((c) => (c ? { ...c, ...p } : c))

  async function save() {
    if (!cfg) return
    setBusy(true)
    setSaved(false)
    setErr('')
    try {
      const updated = await api.setLlmSettings({
        enabled: cfg.enabled,
        baseUrl: cfg.baseUrl,
        model: cfg.model,
        apiKey: apiKey || undefined,
        temperature: cfg.temperature,
        maxTokens: cfg.maxTokens,
      })
      setCfg(updated)
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
        <Sparkles className="h-4 w-4 text-signal" />
        <h4 className="font-display text-sm font-semibold">AI içerik ayarları</h4>
      </div>

      <Toggle
        label="AI içerik üretimini etkinleştir"
        hint="transkriptten otomatik özet, etiket ve bölümler"
        checked={cfg.enabled}
        onChange={(v) => patch({ enabled: v })}
      />

      <div>
        <label className="eyebrow mb-1 block">endpoint URL (/chat/completions)</label>
        <input
          value={cfg.baseUrl}
          onChange={(e) => patch({ baseUrl: e.target.value })}
          placeholder="https://host:port/api/chat/completions"
          className="input w-full font-mono text-xs"
        />
      </div>

      <div>
        <label className="eyebrow mb-1 block">model</label>
        <input
          value={cfg.model}
          onChange={(e) => patch({ model: e.target.value })}
          placeholder="örn. gpt-4o-mini / llama3.1:8b"
          className="input w-full font-mono text-xs"
        />
      </div>

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

      <div className="flex gap-3">
        <div className="flex-1">
          <label className="eyebrow mb-1 block">sıcaklık</label>
          <input
            type="number"
            step={0.1}
            min={0}
            max={2}
            value={cfg.temperature}
            onChange={(e) => patch({ temperature: Number(e.target.value) })}
            className="input w-full"
          />
        </div>
        <div className="flex-1">
          <label className="eyebrow mb-1 block">maks. token</label>
          <input
            type="number"
            min={64}
            max={32000}
            value={cfg.maxTokens}
            onChange={(e) => patch({ maxTokens: Number(e.target.value) })}
            className="input w-full"
          />
        </div>
      </div>

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
    <button type="button" onClick={() => onChange(!checked)} className="flex w-full items-center gap-3 text-left">
      <span className={`relative h-5 w-9 shrink-0 rounded-full transition ${checked ? 'bg-signal' : 'bg-edge'}`}>
        <span className={`absolute top-0.5 h-4 w-4 rounded-full bg-chalk transition-all ${checked ? 'left-4' : 'left-0.5'}`} />
      </span>
      <span className="min-w-0">
        <span className="block text-sm font-medium">{label}</span>
        {hint && <span className="block font-mono text-[10px] text-haze">{hint}</span>}
      </span>
    </button>
  )
}

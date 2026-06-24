import { useState } from 'react'
import { motion } from 'framer-motion'
import { Radio, ArrowRight, LoaderCircle } from 'lucide-react'
import { api, ApiError } from '../api'

export function Login({ onAuthed }: { onAuthed: () => void }) {
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await api.login(password)
      onAuthed()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'giriş başarısız')
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-full items-center justify-center px-6">
      <motion.div
        initial={{ opacity: 0, y: 16 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.5, ease: [0.16, 1, 0.3, 1] }}
        className="w-full max-w-sm"
      >
        <div className="mb-8 flex items-center gap-3">
          <span className="grid h-10 w-10 place-items-center rounded-lg bg-signal/15 ring-1 ring-signal/30">
            <Radio className="h-5 w-5 text-signal" />
          </span>
          <div className="leading-tight">
            <div className="font-display text-lg font-semibold tracking-tight">
              vodstack<span className="text-signal">/</span>stream
            </div>
            <div className="eyebrow">control deck</div>
          </div>
        </div>

        <form
          onSubmit={submit}
          className="rounded-2xl border border-edge bg-panel/70 p-6 shadow-deck backdrop-blur"
        >
          <label className="eyebrow mb-2 block">erişim anahtarı</label>
          <input
            type="password"
            autoFocus
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="••••••••••"
            className="w-full rounded-lg border border-edge bg-ink/70 px-3.5 py-3 font-mono text-sm text-chalk outline-none transition focus:border-signal/60 focus:ring-2 focus:ring-signal/20"
          />

          {error && (
            <p className="mt-3 font-mono text-xs text-bad">⚠ {error}</p>
          )}

          <button
            type="submit"
            disabled={busy || !password}
            className="group mt-5 flex w-full items-center justify-center gap-2 rounded-lg bg-signal px-4 py-3 font-semibold text-ink transition hover:bg-signal-soft disabled:opacity-40"
          >
            {busy ? (
              <LoaderCircle className="h-4 w-4 animate-spin" />
            ) : (
              <>
                panele gir
                <ArrowRight className="h-4 w-4 transition group-hover:translate-x-0.5" />
              </>
            )}
          </button>
        </form>
        <p className="mt-4 text-center font-mono text-[10px] uppercase tracking-[0.2em] text-haze/60">
          self-hosted vod · hls · signed
        </p>
      </motion.div>
    </div>
  )
}

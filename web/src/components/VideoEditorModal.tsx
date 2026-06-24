import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type PointerEvent as ReactPointerEvent,
} from 'react'
import { motion } from 'framer-motion'
import {
  Scissors,
  Crop as CropIcon,
  RotateCcw,
  RotateCw,
  FlipHorizontal,
  FlipVertical,
  Trash2,
  X,
  Check,
  Upload,
  Play,
  Pause,
  SkipBack,
} from 'lucide-react'
import {
  type Edl,
  type Segment,
  type Crop,
  type Flip,
  FULL_CROP,
  buildEdl,
  validateEdl,
  isIdentityEdl,
} from '../lib/edl'

export interface VideoEditorModalProps {
  file: File
  onConfirm: (edl: Edl) => void // proceed to upload WITH this EDL
  onSkip: () => void // upload unedited (identity)
  onCancel: () => void // abandon this file entirely
}

// ---------------------------------------------------------------------------
// Aspect-ratio presets for the crop box. `null` ratio == unconstrained.
// ---------------------------------------------------------------------------
interface AspectPreset {
  key: string
  label: string
  ratio: number | null // width / height in *pixel* (rendered) space
}
const ASPECTS: AspectPreset[] = [
  { key: 'free', label: 'Serbest', ratio: null },
  { key: '16:9', label: '16:9', ratio: 16 / 9 },
  { key: '9:16', label: '9:16', ratio: 9 / 16 },
  { key: '1:1', label: '1:1', ratio: 1 },
  { key: '4:3', label: '4:3', ratio: 4 / 3 },
]

type Handle = 'nw' | 'n' | 'ne' | 'e' | 'se' | 's' | 'sw' | 'w'
const HANDLES: { id: Handle; cursor: string; style: CSSProperties }[] = [
  { id: 'nw', cursor: 'nwse-resize', style: { left: 0, top: 0 } },
  { id: 'n', cursor: 'ns-resize', style: { left: '50%', top: 0 } },
  { id: 'ne', cursor: 'nesw-resize', style: { left: '100%', top: 0 } },
  { id: 'e', cursor: 'ew-resize', style: { left: '100%', top: '50%' } },
  { id: 'se', cursor: 'nwse-resize', style: { left: '100%', top: '100%' } },
  { id: 's', cursor: 'ns-resize', style: { left: '50%', top: '100%' } },
  { id: 'sw', cursor: 'nesw-resize', style: { left: 0, top: '100%' } },
  { id: 'w', cursor: 'ew-resize', style: { left: 0, top: '50%' } },
]

const MIN_SEG = 0.3 // minimum kept-segment length, seconds
const MIN_CROP = 0.06 // minimum crop dimension, normalized

// VideoEditorModal is the pre-upload, in-browser CapCut-style editor. It plays
// the selected file locally (no upload, no encode) so an admin can trim ranges
// on a filmstrip timeline, draw a crop rectangle, and rotate/flip. On confirm
// it emits an EDL the backend applies during transcode.
export function VideoEditorModal({ file, onConfirm, onSkip, onCancel }: VideoEditorModalProps): JSX.Element {
  const [url] = useState(() => URL.createObjectURL(file))
  const videoRef = useRef<HTMLVideoElement>(null)
  const stageRef = useRef<HTMLDivElement>(null)
  const trackRef = useRef<HTMLDivElement>(null)

  const [duration, setDuration] = useState(0)
  const [currentTime, setCurrentTime] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [segments, setSegments] = useState<Segment[]>([])
  const [selected, setSelected] = useState(0)

  const [cropOn, setCropOn] = useState(false)
  const [crop, setCrop] = useState<Crop>({ ...FULL_CROP })
  const [aspect, setAspect] = useState<string>('free')
  const [stageRatio, setStageRatio] = useState(16 / 9) // rendered px ratio of the video box

  const [rotate, setRotate] = useState<0 | 90 | 180 | 270>(0)
  const [flip, setFlip] = useState<Flip>('none')
  const [error, setError] = useState<string | null>(null)
  const [hint, setHintState] = useState<string | null>(null)

  const [thumbs, setThumbs] = useState<string[]>([])
  const [thumbCount, setThumbCount] = useState(0)
  const [trackW, setTrackW] = useState(0)

  // Revoke the object URL when the modal unmounts.
  useEffect(() => () => URL.revokeObjectURL(url), [url])

  // Transient hint shown near the transport; auto-clears after ~3.5s. The timer
  // is tracked in a ref so a later hint (or unmount) cancels the pending clear.
  const hintTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const setHint = useCallback((msg: string | null) => {
    setHintState(msg)
    if (hintTimer.current) clearTimeout(hintTimer.current)
    hintTimer.current = null
    if (msg) {
      hintTimer.current = setTimeout(() => {
        setHintState(null)
        hintTimer.current = null
      }, 3500)
    }
  }, [])
  useEffect(
    () => () => {
      if (hintTimer.current) clearTimeout(hintTimer.current)
    },
    [],
  )

  // ---- metadata --------------------------------------------------------------
  function onLoadedMetadata() {
    const v = videoRef.current
    const d = v?.duration ?? 0
    if (Number.isFinite(d) && d > 0) {
      setDuration(d)
      setSegments([{ start: 0, end: d }])
      setSelected(0)
    }
    if (v && v.videoWidth > 0 && v.videoHeight > 0) {
      setStageRatio(v.videoWidth / v.videoHeight)
    }
  }

  // ---- playback wiring -------------------------------------------------------
  // Keep the latest kept-segments in a ref so the rAF playback loop can skip
  // removed regions without re-subscribing on every edit.
  const segmentsRef = useRef(segments)
  useEffect(() => {
    segmentsRef.current = segments
  }, [segments])

  useEffect(() => {
    const v = videoRef.current
    if (!v) return
    let raf = 0
    // During playback, jump over removed regions so the preview plays only the
    // kept clips — a live preview of the final edited output (CapCut-style).
    const skipGaps = (): boolean => {
      const segs = segmentsRef.current
      if (segs.length === 0) return false
      const t = v.currentTime
      const inKept = segs.some((s) => t >= s.start - 0.06 && t <= s.end + 0.02)
      if (inKept) return false
      const nextSeg = segs.find((s) => s.start > t)
      if (nextSeg) {
        v.currentTime = nextSeg.start // jump to the next kept clip
        return true
      }
      // Past the last kept clip: stop at its end (don't play a trimmed tail).
      v.pause()
      v.currentTime = segs[segs.length - 1].end
      return true
    }
    const tick = () => {
      skipGaps()
      setCurrentTime(v.currentTime)
      raf = requestAnimationFrame(tick)
    }
    const onPlay = () => {
      setPlaying(true)
      raf = requestAnimationFrame(tick)
    }
    const onPause = () => {
      setPlaying(false)
      cancelAnimationFrame(raf)
      setCurrentTime(v.currentTime)
    }
    const onTime = () => setCurrentTime(v.currentTime)
    v.addEventListener('play', onPlay)
    v.addEventListener('pause', onPause)
    v.addEventListener('timeupdate', onTime)
    return () => {
      v.removeEventListener('play', onPlay)
      v.removeEventListener('pause', onPause)
      v.removeEventListener('timeupdate', onTime)
      cancelAnimationFrame(raf)
    }
  }, [])

  const togglePlay = useCallback(() => {
    const v = videoRef.current
    if (!v) return
    if (v.paused) void v.play()
    else v.pause()
  }, [])

  const seekTo = useCallback(
    (t: number) => {
      const v = videoRef.current
      if (!v) return
      const clamped = Math.max(0, Math.min(duration || t, t))
      v.currentTime = clamped
      setCurrentTime(clamped)
    },
    [duration],
  )

  // ---- track width tracking --------------------------------------------------
  useLayoutEffect(() => {
    const el = trackRef.current
    if (!el) return
    const measure = () => setTrackW(el.getBoundingClientRect().width)
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  // ---- filmstrip extraction (offscreen, sequential, cancellable) -------------
  useEffect(() => {
    if (!duration || trackW <= 0) return
    let cancelled = false
    const n = Math.min(40, Math.max(16, Math.floor(trackW / 80)))
    setThumbCount(n)

    const v = document.createElement('video')
    v.src = url
    v.muted = true
    v.preload = 'auto'
    v.playsInline = true

    const canvas = document.createElement('canvas')
    const ctx = canvas.getContext('2d')

    const seekTo = (t: number) =>
      new Promise<void>((resolve) => {
        let done = false
        const finish = () => {
          if (done) return
          done = true
          v.removeEventListener('seeked', finish)
          resolve()
        }
        v.addEventListener('seeked', finish)
        // guard against a seek that never fires (some codecs near EOF)
        setTimeout(finish, 1500)
        try {
          v.currentTime = t
        } catch {
          finish()
        }
      })

    const run = async () => {
      await new Promise<void>((resolve) => {
        if (v.readyState >= 2) return resolve()
        v.addEventListener('loadeddata', () => resolve(), { once: true })
        setTimeout(resolve, 4000)
      })
      if (cancelled || !ctx) return
      const aw = v.videoWidth || 16
      const ah = v.videoHeight || 9
      const cw = 160
      const ch = Math.max(1, Math.round((cw * ah) / aw))
      canvas.width = cw
      canvas.height = ch
      const collected: string[] = []
      for (let i = 0; i < n; i++) {
        if (cancelled) return
        const t = (duration * (i + 0.5)) / n // sample at frame centers
        // eslint-disable-next-line no-await-in-loop
        await seekTo(Math.min(t, Math.max(0, duration - 0.05)))
        if (cancelled) return
        try {
          ctx.drawImage(v, 0, 0, cw, ch)
          collected.push(canvas.toDataURL('image/jpeg', 0.6))
        } catch {
          collected.push('')
        }
        if (!cancelled) setThumbs([...collected])
      }
    }

    void run()
    return () => {
      cancelled = true
      v.removeAttribute('src')
      try {
        v.load()
      } catch {
        /* noop */
      }
    }
    // re-run only when source or duration change (trackW change keeps existing strip)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url, duration])

  // ---- keyboard --------------------------------------------------------------
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement | null)?.tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA') return
      if (e.code === 'Space') {
        e.preventDefault()
        togglePlay()
      } else if (e.key === 'Escape') {
        e.preventDefault()
        onCancel()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [togglePlay, onCancel])

  // ---- segment operations ----------------------------------------------------
  function findSegmentAt(t: number): number {
    return segments.findIndex((s) => t >= s.start && t <= s.end)
  }

  function splitAtPlayhead() {
    const t = currentTime
    const i = segments.findIndex((s) => t > s.start + MIN_SEG && t < s.end - MIN_SEG)
    if (i < 0) {
      setHint('Bölmek için oynatma çizgisini bir klibin ortasına getir.')
      return
    }
    const s = segments[i]
    const next = [...segments]
    next.splice(i, 1, { start: s.start, end: t }, { start: t, end: s.end })
    setSegments(next)
    setSelected(i + 1) // select the right half so Sil removes the tail
    setHint('Klip bölündü — bir klibi seçip Sil ile çıkarabilir ya da kenarından sürükleyebilirsin.')
  }

  function removeSelected() {
    setSegments((segs) => (segs.length <= 1 ? segs : segs.filter((_, idx) => idx !== selected)))
    setSelected((s) => Math.max(0, s - (s === segments.length - 1 ? 1 : 0)))
  }

  // ---- timeline drag (playhead + trim handles) -------------------------------
  type TLDrag =
    | { kind: 'playhead' }
    | { kind: 'trim'; seg: number; edge: 'start' | 'end' }
    | { kind: 'move'; seg: number; grab: number } // grab = pointerTime - seg.start
  const tlDrag = useRef<TLDrag | null>(null)

  const pxToTime = useCallback(
    (clientX: number): number => {
      const r = trackRef.current?.getBoundingClientRect()
      if (!r || r.width === 0 || !duration) return 0
      const frac = (clientX - r.left) / r.width
      return Math.max(0, Math.min(duration, frac * duration))
    },
    [duration],
  )

  const onTLMove = useCallback(
    (e: PointerEvent) => {
      const d = tlDrag.current
      if (!d) return
      const t = pxToTime(e.clientX)
      if (d.kind === 'playhead') {
        seekTo(t)
        return
      }
      if (d.kind === 'move') {
        // Slide the whole clip (both ends) without changing its length, clamped
        // so it never overlaps neighbors or leaves [0, duration].
        setSegments((segs) => {
          const s = segs[d.seg]
          if (!s) return segs
          const len = s.end - s.start
          const prev = segs[d.seg - 1]
          const next = segs[d.seg + 1]
          const lo = prev ? prev.end : 0
          const hi = (next ? next.start : duration) - len
          const start = Math.max(lo, Math.min(hi, t - d.grab))
          const copy = [...segs]
          copy[d.seg] = { start, end: start + len }
          return copy
        })
        return
      }
      setSegments((segs) => {
        const s = segs[d.seg]
        if (!s) return segs
        const prev = segs[d.seg - 1]
        const next = segs[d.seg + 1]
        const copy = [...segs]
        if (d.edge === 'start') {
          const lo = prev ? prev.end : 0
          const start = Math.max(lo, Math.min(t, s.end - MIN_SEG))
          copy[d.seg] = { ...s, start }
        } else {
          const hi = next ? next.start : duration
          const end = Math.min(hi, Math.max(t, s.start + MIN_SEG))
          copy[d.seg] = { ...s, end }
        }
        return copy
      })
    },
    [pxToTime, seekTo, duration],
  )

  const endTLDrag = useCallback(() => {
    tlDrag.current = null
    window.removeEventListener('pointermove', onTLMove)
    window.removeEventListener('pointerup', endTLDrag)
  }, [onTLMove])

  const startTLDrag = useCallback(
    (e: ReactPointerEvent, d: TLDrag) => {
      e.preventDefault()
      e.stopPropagation()
      tlDrag.current = d
      window.addEventListener('pointermove', onTLMove)
      window.addEventListener('pointerup', endTLDrag)
    },
    [onTLMove, endTLDrag],
  )

  function onTrackPointerDown(e: ReactPointerEvent) {
    // clicking the empty track scrubs + starts a playhead drag
    seekTo(pxToTime(e.clientX))
    startTLDrag(e, { kind: 'playhead' })
  }

  useEffect(() => () => endTLDrag(), [endTLDrag])

  // ---- crop drag/resize ------------------------------------------------------
  const cropDrag = useRef<{
    mode: 'move' | Handle
    startX: number
    startY: number
    orig: Crop
    rect: DOMRect
  } | null>(null)

  const activeRatio = useMemo(() => ASPECTS.find((a) => a.key === aspect)?.ratio ?? null, [aspect])

  const onCropMove = useCallback(
    (e: PointerEvent) => {
      const d = cropDrag.current
      if (!d) return
      const dx = (e.clientX - d.startX) / d.rect.width
      const dy = (e.clientY - d.startY) / d.rect.height
      const o = d.orig

      if (d.mode === 'move') {
        const x = clamp(o.x + dx, 0, 1 - o.w)
        const y = clamp(o.y + dy, 0, 1 - o.h)
        setCrop({ x, y, w: o.w, h: o.h })
        return
      }

      let { x, y, w, h } = o
      const right = o.x + o.w
      const bottom = o.y + o.h
      const m = d.mode
      if (m.includes('w')) {
        x = clamp(o.x + dx, 0, right - MIN_CROP)
        w = right - x
      }
      if (m.includes('e')) {
        w = clamp(o.w + dx, MIN_CROP, 1 - o.x)
      }
      if (m.includes('n')) {
        y = clamp(o.y + dy, 0, bottom - MIN_CROP)
        h = bottom - y
      }
      if (m.includes('s')) {
        h = clamp(o.h + dy, MIN_CROP, 1 - o.y)
      }

      // Apply aspect-ratio constraint (ratio is in rendered-pixel space).
      if (activeRatio) {
        // ratio_norm = w_px/h_px = (w*W)/(h*H) -> w = h * ratio * (H/W)
        const k = activeRatio / stageRatio // w_norm / h_norm
        // Decide which dimension drives based on the handle.
        const horizDriven = m === 'e' || m === 'w'
        if (horizDriven) {
          h = w / k
          if (m.includes('n')) y = bottom - h
        } else {
          w = h * k
          if (m.includes('w')) x = right - w
        }
        // clamp into bounds, shrinking the pair if needed
        if (x < 0) {
          x = 0
          w = right - x
          h = w / k
          if (m.includes('n')) y = bottom - h
        }
        if (y < 0) {
          y = 0
          h = bottom - y
          w = h * k
          if (m.includes('w')) x = right - w
        }
        if (x + w > 1) {
          w = 1 - x
          h = w / k
          if (m.includes('n')) y = bottom - h
        }
        if (y + h > 1) {
          h = 1 - y
          w = h * k
          if (m.includes('w')) x = right - w
        }
        // Final in-frame reconciliation: an aspect-locked resize near a frame
        // edge can still leave x+w>1 / y+h>1 after the pair adjustments above.
        // Clamp the box fully inside the frame (keeping at least MIN_CROP).
        x = clamp(x, 0, 1 - MIN_CROP)
        y = clamp(y, 0, 1 - MIN_CROP)
        w = Math.max(MIN_CROP, Math.min(w, 1 - x))
        h = Math.max(MIN_CROP, Math.min(h, 1 - y))
      }

      setCrop({
        x: clamp(x, 0, 1),
        y: clamp(y, 0, 1),
        w: clamp(w, MIN_CROP, 1),
        h: clamp(h, MIN_CROP, 1),
      })
    },
    [activeRatio, stageRatio],
  )

  const endCropDrag = useCallback(() => {
    cropDrag.current = null
    window.removeEventListener('pointermove', onCropMove)
    window.removeEventListener('pointerup', endCropDrag)
  }, [onCropMove])

  function startCropDrag(e: ReactPointerEvent, mode: 'move' | Handle) {
    e.preventDefault()
    e.stopPropagation()
    const rect = stageRef.current?.getBoundingClientRect()
    if (!rect) return
    cropDrag.current = { mode, startX: e.clientX, startY: e.clientY, orig: crop, rect }
    window.addEventListener('pointermove', onCropMove)
    window.addEventListener('pointerup', endCropDrag)
  }
  useEffect(() => () => endCropDrag(), [endCropDrag])

  function applyAspect(key: string) {
    setAspect(key)
    const ratio = ASPECTS.find((a) => a.key === key)?.ratio ?? null
    if (!ratio) return
    // Re-fit the current box center to the new ratio (px-space ratio -> norm).
    const k = ratio / stageRatio
    setCrop((c) => {
      const cx = c.x + c.w / 2
      const cy = c.y + c.h / 2
      let w = c.w
      let h = w / k
      if (h > 1) {
        h = 1
        w = h * k
      }
      let x = clamp(cx - w / 2, 0, 1 - w)
      let y = clamp(cy - h / 2, 0, 1 - h)
      return { x, y, w, h }
    })
  }

  function toggleCrop() {
    setCropOn((on) => {
      const next = !on
      if (next && crop.w >= 0.999 && crop.h >= 0.999) {
        setCrop({ x: 0.1, y: 0.1, w: 0.8, h: 0.8 })
      }
      return next
    })
  }
  function resetCrop() {
    setCrop({ ...FULL_CROP })
    setAspect('free')
  }

  // ---- confirm ---------------------------------------------------------------
  function confirm() {
    const eps = 0.05
    const isFullRange =
      segments.length === 1 && segments[0].start <= eps && segments[0].end >= duration - eps
    const segs = isFullRange ? [] : segments
    const edl = buildEdl({
      segments: segs,
      crop: cropOn ? crop : { ...FULL_CROP },
      rotate,
      flip,
    })
    const msg = validateEdl(edl, duration)
    if (msg) {
      setError(msg)
      return
    }
    setError(null)
    if (isIdentityEdl(edl)) onSkip()
    else onConfirm(edl)
  }

  // ---- derived ---------------------------------------------------------------
  const effectiveCrop = cropOn ? crop : FULL_CROP
  const longForm = duration >= 3600
  const keptTotal = segments.reduce((a, s) => a + (s.end - s.start), 0)
  const playheadInGap = findSegmentAt(currentTime) < 0

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      className="fixed inset-0 z-50 flex flex-col bg-ink/90 backdrop-blur"
    >
      <motion.div
        initial={{ opacity: 0, scale: 0.985 }}
        animate={{ opacity: 1, scale: 1 }}
        transition={{ duration: 0.24, ease: [0.16, 1, 0.3, 1] }}
        className="flex min-h-0 flex-1 flex-col"
      >
        {/* ---- Header ---- */}
        <header className="flex items-center gap-3 border-b border-edge bg-panel/60 px-5 py-3">
          <span className="grid h-8 w-8 shrink-0 place-items-center rounded-lg bg-signal/12 ring-1 ring-signal/25">
            <Scissors className="h-4 w-4 text-signal" />
          </span>
          <div className="min-w-0 flex-1">
            <div className="eyebrow">yüklemeden önce düzenle</div>
            <h3 className="truncate font-mono text-sm text-chalk" title={file.name}>
              {file.name}
            </h3>
          </div>
          <span className="hidden font-mono text-[11px] text-haze sm:block">
            {mmss(keptTotal, longForm)} <span className="text-haze/60">tutulacak</span>
          </span>
          <button
            onClick={onCancel}
            aria-label="Kapat"
            title="Kapat (Esc)"
            className="grid h-9 w-9 place-items-center rounded-lg text-haze transition hover:bg-edge hover:text-chalk"
          >
            <X className="h-5 w-5" />
          </button>
        </header>

        {/* ---- Main stage ---- */}
        <div className="flex min-h-0 flex-1">
          {/* preview */}
          <div className="grid min-w-0 flex-1 place-items-center overflow-hidden bg-ink p-6">
            <div
              ref={stageRef}
              className="relative inline-block max-h-full max-w-full select-none"
            >
              <video
                ref={videoRef}
                src={url}
                playsInline
                onLoadedMetadata={onLoadedMetadata}
                onClick={togglePlay}
                style={{ transform: cssTransform(rotate, flip) }}
                className="block max-h-[64vh] max-w-full rounded-lg bg-black shadow-deck"
              />

              {cropOn && (
                <div className="pointer-events-none absolute inset-0">
                  {/* dim mask outside the crop box (4 bands) */}
                  <CropMask crop={effectiveCrop} />
                  {/* crop box */}
                  <div
                    onPointerDown={(e) => startCropDrag(e, 'move')}
                    className="pointer-events-auto absolute cursor-move ring-1 ring-signal"
                    style={{
                      left: `${effectiveCrop.x * 100}%`,
                      top: `${effectiveCrop.y * 100}%`,
                      width: `${effectiveCrop.w * 100}%`,
                      height: `${effectiveCrop.h * 100}%`,
                    }}
                  >
                    {/* rule-of-thirds guides */}
                    <div className="pointer-events-none absolute inset-0">
                      <div className="absolute left-1/3 top-0 h-full w-px bg-signal/30" />
                      <div className="absolute left-2/3 top-0 h-full w-px bg-signal/30" />
                      <div className="absolute left-0 top-1/3 h-px w-full bg-signal/30" />
                      <div className="absolute left-0 top-2/3 h-px w-full bg-signal/30" />
                    </div>
                    <span className="pointer-events-none absolute left-1 top-1 rounded bg-ink/80 px-1.5 py-0.5 font-mono text-[9px] text-signal">
                      {Math.round(effectiveCrop.w * 100)}×{Math.round(effectiveCrop.h * 100)}%
                    </span>
                    {HANDLES.map((h) => (
                      <button
                        key={h.id}
                        onPointerDown={(e) => startCropDrag(e, h.id)}
                        aria-label={`Kırpma tutamacı ${h.id}`}
                        className="absolute h-3 w-3 -translate-x-1/2 -translate-y-1/2 rounded-[3px] border border-ink bg-signal shadow"
                        style={{ ...h.style, cursor: h.cursor }}
                      />
                    ))}
                  </div>
                </div>
              )}
            </div>
          </div>

          {/* tools panel */}
          <aside className="flex w-[280px] shrink-0 flex-col gap-5 overflow-y-auto border-l border-edge bg-panel/40 p-5">
            {/* Crop */}
            <section>
              <ToolHeader
                icon={<CropIcon className="h-3.5 w-3.5" />}
                label="Kırpma"
                action={
                  <Toggle on={cropOn} onClick={toggleCrop} label="Kırpmayı aç/kapat" />
                }
              />
              <div className={cropOn ? '' : 'pointer-events-none opacity-40'}>
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {ASPECTS.map((a) => (
                    <button
                      key={a.key}
                      onClick={() => applyAspect(a.key)}
                      aria-pressed={aspect === a.key}
                      className={`rounded-full border px-2.5 py-1 font-mono text-[11px] transition ${
                        aspect === a.key
                          ? 'border-signal bg-signal/15 text-signal'
                          : 'border-edge text-haze hover:border-signal/40 hover:text-chalk'
                      }`}
                    >
                      {a.label}
                    </button>
                  ))}
                </div>
                <button
                  onClick={resetCrop}
                  className="mt-2 inline-flex items-center gap-1.5 rounded-lg border border-edge px-2.5 py-1.5 font-mono text-[11px] text-haze transition hover:border-signal/40 hover:text-chalk"
                >
                  <RotateCcw className="h-3.5 w-3.5" /> Sıfırla
                </button>
              </div>
            </section>

            <div className="h-px bg-edge" />

            {/* Rotate */}
            <section>
              <ToolHeader
                icon={<RotateCw className="h-3.5 w-3.5" />}
                label="Döndür"
                action={<span className="font-mono text-[11px] text-signal">{rotate}°</span>}
              />
              <div className="mt-2 flex gap-2">
                <IconBtn
                  onClick={() => setRotate((r) => (((r + 270) % 360) as 0 | 90 | 180 | 270))}
                  label="90° sola döndür"
                >
                  <RotateCcw className="h-4 w-4" />
                </IconBtn>
                <IconBtn
                  onClick={() => setRotate((r) => (((r + 90) % 360) as 0 | 90 | 180 | 270))}
                  label="90° sağa döndür"
                >
                  <RotateCw className="h-4 w-4" />
                </IconBtn>
              </div>
            </section>

            <div className="h-px bg-edge" />

            {/* Flip */}
            <section>
              <ToolHeader icon={<FlipHorizontal className="h-3.5 w-3.5" />} label="Çevir" />
              <div className="mt-2 flex gap-2">
                <IconBtn
                  active={flip === 'h'}
                  onClick={() => setFlip((f) => (f === 'h' ? 'none' : 'h'))}
                  label="Yatay çevir"
                >
                  <FlipHorizontal className="h-4 w-4" />
                </IconBtn>
                <IconBtn
                  active={flip === 'v'}
                  onClick={() => setFlip((f) => (f === 'v' ? 'none' : 'v'))}
                  label="Dikey çevir"
                >
                  <FlipVertical className="h-4 w-4" />
                </IconBtn>
              </div>
            </section>
          </aside>
        </div>

        {/* ---- Transport bar ---- */}
        <div className="flex items-center gap-3 border-t border-edge bg-panel/60 px-5 py-2.5">
          <IconBtn onClick={() => seekTo(0)} label="Başa sar">
            <SkipBack className="h-4 w-4" />
          </IconBtn>
          <button
            onClick={togglePlay}
            aria-label={playing ? 'Duraklat' : 'Oynat'}
            title={playing ? 'Duraklat (Space)' : 'Oynat (Space)'}
            className="grid h-10 w-10 place-items-center rounded-full bg-signal text-ink transition hover:bg-signal-soft"
          >
            {playing ? <Pause className="h-5 w-5" /> : <Play className="ml-0.5 h-5 w-5" />}
          </button>

          <div className="font-mono text-sm tabular-nums">
            <span className="text-chalk">{mmss(currentTime, longForm)}</span>
            <span className="text-haze"> / {mmss(duration, longForm)}</span>
          </div>

          {hint && (
            <p className="hidden min-w-0 truncate text-xs text-haze md:block" title={hint}>
              {hint}
            </p>
          )}

          <div className="ml-auto flex items-center gap-2">
            <button
              onClick={splitAtPlayhead}
              title="Oynatma konumunda böl"
              className="inline-flex items-center gap-1.5 rounded-lg border border-edge bg-panel/50 px-3 py-1.5 text-sm text-chalk transition hover:border-signal/50"
            >
              <Scissors className="h-4 w-4" /> Böl
            </button>
            <button
              onClick={removeSelected}
              disabled={segments.length <= 1}
              title="Seçili segmenti sil"
              className="inline-flex items-center gap-1.5 rounded-lg border border-edge bg-panel/50 px-3 py-1.5 text-sm text-chalk transition hover:border-bad/60 hover:text-bad disabled:cursor-not-allowed disabled:opacity-30"
            >
              <Trash2 className="h-4 w-4" /> Sil
            </button>
          </div>
        </div>

        {/* ---- Timeline ---- */}
        <div className="border-t border-edge bg-ink px-5 pb-5 pt-3">
          {/* ruler */}
          <Ruler duration={duration} longForm={longForm} />

          {/* track */}
          <div
            ref={trackRef}
            onPointerDown={onTrackPointerDown}
            className="relative mt-1.5 h-20 cursor-pointer select-none overflow-hidden rounded-lg border border-edge bg-black"
          >
            {/* filmstrip */}
            <Filmstrip thumbs={thumbs} count={thumbCount} />

            {duration > 0 && (
              <>
                {/* removed-region overlay (everything not in a kept segment) */}
                <GapOverlay segments={segments} duration={duration} />

                {/* kept segments rendered as distinct CapCut-style clip blocks */}
                {segments.map((s, i) => {
                  const left = (s.start / duration) * 100
                  const width = ((s.end - s.start) / duration) * 100
                  const isSel = i === selected
                  return (
                    <div
                      key={i}
                      onPointerDown={(e) => {
                        e.stopPropagation()
                        setSelected(i)
                        // Grab the clip body to slide the whole clip along the timeline.
                        startTLDrag(e, { kind: 'move', seg: i, grab: pxToTime(e.clientX) - s.start })
                      }}
                      title="Klibi sürükleyerek kaydır"
                      className="absolute top-0 z-10 h-full cursor-grab touch-none active:cursor-grabbing"
                      style={{ left: `${left}%`, width: `${width}%` }}
                    >
                      {/* inner clip block, inset so adjacent clips show a clear gap */}
                      <div
                        className={`pointer-events-none absolute inset-x-[2px] inset-y-[3px] rounded-md ${
                          isSel
                            ? 'bg-signal/15 ring-2 ring-signal'
                            : 'bg-signal/[0.04] ring-1 ring-signal/30'
                        }`}
                      >
                        {/* duration label */}
                        <span className="absolute left-1.5 top-1 rounded bg-ink/70 px-1 font-mono text-[9px] leading-tight text-signal">
                          {mmss(s.end - s.start, longForm)}
                        </span>
                      </div>
                      {/* left trim */}
                      <TrimHandle
                        side="start"
                        onPointerDown={(e) => {
                          e.stopPropagation()
                          setSelected(i)
                          startTLDrag(e, { kind: 'trim', seg: i, edge: 'start' })
                        }}
                      />
                      {/* right trim */}
                      <TrimHandle
                        side="end"
                        onPointerDown={(e) => {
                          e.stopPropagation()
                          setSelected(i)
                          startTLDrag(e, { kind: 'trim', seg: i, edge: 'end' })
                        }}
                      />
                    </div>
                  )
                })}

                {/* split markers at internal touching boundaries */}
                <ClipDividers segments={segments} duration={duration} />

                {/* playhead — bold white line + grab knob, highly visible over the strip */}
                <div
                  className="pointer-events-none absolute inset-y-0 z-30"
                  style={{ left: `${(currentTime / duration) * 100}%` }}
                >
                  {/* bright vertical line with glow */}
                  <div className="absolute inset-y-0 left-0 w-[2px] -translate-x-1/2 bg-white shadow-[0_0_8px_2px_rgba(255,255,255,0.55)]" />
                  {/* grabbable top knob */}
                  <div
                    onPointerDown={(e) => {
                      e.stopPropagation()
                      startTLDrag(e, { kind: 'playhead' })
                    }}
                    title="Oynatma çizgisi — sürükleyerek konumlandır"
                    className={`pointer-events-auto absolute left-0 top-0 h-4 w-4 -translate-x-1/2 cursor-ew-resize rounded-b-md border border-ink bg-white shadow-md ${
                      playheadInGap ? 'ring-2 ring-bad' : ''
                    }`}
                  />
                  {/* wide invisible hit strip for easy grabbing */}
                  <div
                    onPointerDown={(e) => {
                      e.stopPropagation()
                      startTLDrag(e, { kind: 'playhead' })
                    }}
                    className="pointer-events-auto absolute inset-y-0 left-0 w-4 -translate-x-1/2 cursor-ew-resize"
                  />
                </div>
              </>
            )}
          </div>
        </div>

        {/* ---- Footer ---- */}
        <div className="flex flex-wrap items-center gap-2 border-t border-edge bg-panel/60 px-5 py-3">
          {error && (
            <p className="mr-auto rounded-lg border border-bad/40 bg-bad/10 px-3 py-1.5 font-mono text-xs text-bad">
              ⚠ {error}
            </p>
          )}
          <button onClick={onCancel} className="btn-ghost ml-auto text-sm" title="Bu dosyayı bırak">
            İptal
          </button>
          <button onClick={onSkip} className="btn-ghost text-sm" title="Düzenlemeden yükle">
            <Upload className="h-4 w-4" /> Düzenlemeden Yükle
          </button>
          <button onClick={confirm} className="btn-primary text-sm" title="Düzenlemeleri uygula ve yükle">
            <Check className="h-4 w-4" /> Onayla &amp; Yükle
          </button>
        </div>
      </motion.div>
    </motion.div>
  )
}

// ===========================================================================
// Subcomponents
// ===========================================================================

function ToolHeader({
  icon,
  label,
  action,
}: {
  icon: JSX.Element
  label: string
  action?: JSX.Element
}): JSX.Element {
  return (
    <div className="flex items-center justify-between">
      <div className="flex items-center gap-1.5 text-haze">
        <span className="text-signal">{icon}</span>
        <span className="text-[10px] font-semibold uppercase tracking-[0.18em] text-haze">{label}</span>
      </div>
      {action}
    </div>
  )
}

function Toggle({ on, onClick, label }: { on: boolean; onClick: () => void; label: string }): JSX.Element {
  return (
    <button
      role="switch"
      aria-checked={on}
      aria-label={label}
      onClick={onClick}
      className={`relative h-5 w-9 rounded-full border transition ${
        on ? 'border-signal bg-signal/30' : 'border-edge bg-ink'
      }`}
    >
      <span
        className={`absolute top-0.5 h-3.5 w-3.5 rounded-full transition-all ${
          on ? 'left-[18px] bg-signal' : 'left-0.5 bg-haze'
        }`}
      />
    </button>
  )
}

function IconBtn({
  children,
  onClick,
  label,
  active,
}: {
  children: JSX.Element
  onClick: () => void
  label: string
  active?: boolean
}): JSX.Element {
  return (
    <button
      onClick={onClick}
      aria-label={label}
      aria-pressed={active}
      title={label}
      className={`grid h-9 w-9 place-items-center rounded-lg border transition ${
        active
          ? 'border-signal bg-signal/15 text-signal'
          : 'border-edge bg-panel/50 text-haze hover:border-signal/40 hover:text-chalk'
      }`}
    >
      {children}
    </button>
  )
}

function TrimHandle({
  side,
  onPointerDown,
}: {
  side: 'start' | 'end'
  onPointerDown: (e: ReactPointerEvent) => void
}): JSX.Element {
  return (
    <button
      onPointerDown={onPointerDown}
      aria-label={side === 'start' ? 'Başlangıç tutamacı' : 'Bitiş tutamacı'}
      className={`group absolute top-0 z-10 flex h-full w-3 cursor-ew-resize items-center justify-center bg-signal ${
        side === 'start' ? 'left-0 rounded-l-md' : 'right-0 rounded-r-md'
      }`}
    >
      <span className="h-7 w-0.5 rounded-full bg-ink/70" />
    </button>
  )
}

function Filmstrip({ thumbs, count }: { thumbs: string[]; count: number }): JSX.Element {
  const slots = Math.max(count, 1)
  return (
    <div className="pointer-events-none absolute inset-0 flex">
      {Array.from({ length: slots }).map((_, i) => {
        const src = thumbs[i]
        return (
          <div key={i} className="relative h-full flex-1 overflow-hidden border-r border-black/30">
            {src ? (
              <img
                src={src}
                alt=""
                draggable={false}
                className="h-full w-full object-cover"
              />
            ) : (
              <div className="h-full w-full animate-pulse bg-gradient-to-br from-edge/40 to-panel/40" />
            )}
          </div>
        )
      })}
    </div>
  )
}

// GapOverlay darkens/desaturates regions that are NOT inside any kept segment,
// with a subtle diagonal hatch so a removed range reads as "cut".
function GapOverlay({ segments, duration }: { segments: Segment[]; duration: number }): JSX.Element {
  const gaps: { start: number; end: number }[] = []
  const sorted = [...segments].sort((a, b) => a.start - b.start)
  let cursor = 0
  for (const s of sorted) {
    if (s.start > cursor) gaps.push({ start: cursor, end: s.start })
    cursor = Math.max(cursor, s.end)
  }
  if (cursor < duration) gaps.push({ start: cursor, end: duration })

  return (
    <>
      {gaps.map((g, i) => (
        <div
          key={i}
          className="pointer-events-none absolute top-0 z-10 h-full bg-ink/70 backdrop-grayscale"
          style={{
            left: `${(g.start / duration) * 100}%`,
            width: `${((g.end - g.start) / duration) * 100}%`,
            backgroundImage:
              'repeating-linear-gradient(45deg, rgba(255,255,255,0.05) 0 6px, transparent 6px 12px)',
          }}
        />
      ))}
    </>
  )
}

// ClipDividers renders a prominent vertical cut marker at every INTERNAL
// boundary where two kept clips touch (segments[i].end ≈ segments[i+1].start).
// Boundaries with a real gap are already shown by GapOverlay, so they're skipped.
function ClipDividers({ segments, duration }: { segments: Segment[]; duration: number }): JSX.Element {
  if (!duration) return <></>
  const sorted = [...segments].sort((a, b) => a.start - b.start)
  const eps = 0.001 * duration
  const cuts: number[] = []
  for (let i = 0; i < sorted.length - 1; i++) {
    if (Math.abs(sorted[i].end - sorted[i + 1].start) <= eps) cuts.push(sorted[i].end)
  }
  return (
    <>
      {cuts.map((t, i) => (
        <div
          key={i}
          className="pointer-events-none absolute top-0 z-20 h-full -translate-x-1/2"
          style={{ left: `${(t / duration) * 100}%` }}
        >
          <div className="absolute left-1/2 top-0 h-full w-0.5 -translate-x-1/2 bg-signal" />
          <span className="absolute -top-0.5 left-1/2 grid h-3.5 w-3.5 -translate-x-1/2 place-items-center rounded-sm bg-signal text-ink shadow">
            <Scissors className="h-2.5 w-2.5" />
          </span>
        </div>
      ))}
    </>
  )
}

// CropMask renders four dim bands around the crop box (overlay-friendly).
function CropMask({ crop }: { crop: Crop }): JSX.Element {
  const band = 'absolute bg-ink/55'
  return (
    <>
      <div className={band} style={{ left: 0, top: 0, width: '100%', height: `${crop.y * 100}%` }} />
      <div
        className={band}
        style={{ left: 0, top: `${(crop.y + crop.h) * 100}%`, width: '100%', bottom: 0 }}
      />
      <div
        className={band}
        style={{ left: 0, top: `${crop.y * 100}%`, width: `${crop.x * 100}%`, height: `${crop.h * 100}%` }}
      />
      <div
        className={band}
        style={{
          left: `${(crop.x + crop.w) * 100}%`,
          top: `${crop.y * 100}%`,
          right: 0,
          height: `${crop.h * 100}%`,
        }}
      />
    </>
  )
}

function Ruler({ duration, longForm }: { duration: number; longForm: boolean }): JSX.Element {
  // Choose a "nice" label interval targeting ~6 labels.
  const ticks = useMemo(() => {
    if (!duration) return [] as { t: number; major: boolean }[]
    const targets = [1, 2, 5, 10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600]
    const ideal = duration / 6
    const step = targets.find((s) => s >= ideal) ?? targets[targets.length - 1]
    const minor = step / 5
    const out: { t: number; major: boolean }[] = []
    for (let t = 0; t <= duration + 0.001; t += minor) {
      const major = Math.abs(t % step) < minor / 2 || Math.abs((t % step) - step) < minor / 2
      out.push({ t, major })
    }
    return out
  }, [duration])

  return (
    <div className="relative h-5">
      {ticks.map((tk, i) => {
        const left = (tk.t / duration) * 100
        return (
          <div key={i} className="absolute bottom-0 -translate-x-1/2" style={{ left: `${left}%` }}>
            <div className={`mx-auto w-px ${tk.major ? 'h-2.5 bg-haze' : 'h-1.5 bg-edge'}`} />
            {tk.major && (
              <div className="mt-0.5 whitespace-nowrap font-mono text-[9px] text-haze">
                {mmss(tk.t, longForm)}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}

// ===========================================================================
// Helpers
// ===========================================================================

// cssTransform builds the live CSS preview transform for the <video> element.
function cssTransform(rotate: number, flip: Flip): string {
  const r = `rotate(${rotate}deg)`
  const f = flip === 'h' ? ' scaleX(-1)' : flip === 'v' ? ' scaleY(-1)' : ''
  return r + f
}

// mmss formats a second count as mm:ss (or h:mm:ss when the clip is ≥1h).
function mmss(sec: number, longForm = false): string {
  if (!Number.isFinite(sec) || sec < 0) sec = 0
  const s = Math.floor(sec % 60)
  const m = Math.floor((sec / 60) % 60)
  const h = Math.floor(sec / 3600)
  const pad = (n: number) => String(n).padStart(2, '0')
  return h > 0 || longForm ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`
}

function clamp(n: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, n))
}

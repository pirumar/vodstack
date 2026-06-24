export interface Segment { start: number; end: number }
export interface Crop { x: number; y: number; w: number; h: number }
export type Flip = 'none' | 'h' | 'v'
export interface Edl {
  version: 1
  segments: Segment[]
  crop: Crop
  rotate: 0 | 90 | 180 | 270
  flip: Flip
}

export const FULL_CROP: Crop = { x: 0, y: 0, w: 1, h: 1 }

export function identityEdl(_durationSeconds: number = 0): Edl {
  return { version: 1, segments: [], crop: { ...FULL_CROP }, rotate: 0, flip: 'none' }
}

function hasCrop(c: Crop): boolean {
  return !(c.x === 0 && c.y === 0 && c.w === 1 && c.h === 1)
}

export function isIdentityEdl(e: Edl): boolean {
  if (hasCrop(e.crop) || e.rotate !== 0 || (e.flip && e.flip !== 'none')) return false
  return e.segments.length === 0
}

// validateEdl returns a human-readable (Turkish) error string, or null if valid.
export function validateEdl(e: Edl, durationSeconds: number): string | null {
  if (![0, 90, 180, 270].includes(e.rotate)) return 'Geçersiz döndürme'
  if (!['none', 'h', 'v'].includes(e.flip)) return 'Geçersiz yansıtma'
  const c = e.crop
  if (
    !Number.isFinite(c.x) || !Number.isFinite(c.y) || !Number.isFinite(c.w) || !Number.isFinite(c.h) ||
    c.w <= 0 || c.h <= 0 || c.x < 0 || c.y < 0 || c.x + c.w > 1.0001 || c.y + c.h > 1.0001
  ) return 'Kırpma alanı sınırların dışında'
  const segs = [...e.segments].sort((a, b) => a.start - b.start)
  let prevEnd = -1
  for (const s of segs) {
    if (!Number.isFinite(s.start) || !Number.isFinite(s.end) || s.start < 0 || s.end <= s.start)
      return 'Segment başlangıcı bitişten küçük olmalı'
    if (s.end > durationSeconds + 0.5) return 'Segment video süresini aşıyor'
    if (s.start < prevEnd) return 'Segmentler çakışıyor (overlap)'
    prevEnd = s.end
  }
  return null
}

// buildEdl normalizes editor state into a server-ready EDL (sorted segments).
export function buildEdl(input: Omit<Edl, 'version'>): Edl {
  return { version: 1, ...input, segments: [...input.segments].sort((a, b) => a.start - b.start) }
}

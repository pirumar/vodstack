import { describe, it, expect } from 'vitest'
import { identityEdl, isIdentityEdl, validateEdl, buildEdl, type Edl } from './edl'

describe('edl', () => {
  it('identity for a full-range untouched clip', () => {
    const e = identityEdl(120)
    expect(isIdentityEdl(e)).toBe(true)
    expect(validateEdl(e, 120)).toBeNull()
  })
  it('flags overlapping segments', () => {
    const e: Edl = { version: 1, segments: [{ start: 0, end: 10 }, { start: 5, end: 15 }], crop: { x: 0, y: 0, w: 1, h: 1 }, rotate: 0, flip: 'none' }
    expect(validateEdl(e, 120)).toMatch(/overlap|çakış/i)
  })
  it('flags segment past duration', () => {
    const e: Edl = { version: 1, segments: [{ start: 0, end: 999 }], crop: { x: 0, y: 0, w: 1, h: 1 }, rotate: 0, flip: 'none' }
    expect(validateEdl(e, 120)).not.toBeNull()
  })
  it('single trimmed segment is NOT identity', () => {
    const e: Edl = { version: 1, segments: [{ start: 5, end: 100 }], crop: { x: 0, y: 0, w: 1, h: 1 }, rotate: 0, flip: 'none' }
    expect(isIdentityEdl(e)).toBe(false)
  })
  it('empty segments is valid (whole video)', () => {
    const e: Edl = { version: 1, segments: [], crop: { x: 0, y: 0, w: 1, h: 1 }, rotate: 0, flip: 'none' }
    expect(validateEdl(e, 120)).toBeNull()
  })
  it('multi-segment is non-identity', () => {
    const e: Edl = { version: 1, segments: [{ start: 0, end: 10 }, { start: 20, end: 30 }], crop: { x: 0, y: 0, w: 1, h: 1 }, rotate: 0, flip: 'none' }
    expect(isIdentityEdl(e)).toBe(false)
  })
  it('crop or rotate makes it non-identity', () => {
    const cropped: Edl = { version: 1, segments: [{ start: 0, end: 10 }], crop: { x: 0, y: 0, w: 0.5, h: 1 }, rotate: 0, flip: 'none' }
    expect(isIdentityEdl(cropped)).toBe(false)
    const rotated: Edl = { version: 1, segments: [{ start: 0, end: 10 }], crop: { x: 0, y: 0, w: 1, h: 1 }, rotate: 90, flip: 'none' }
    expect(isIdentityEdl(rotated)).toBe(false)
  })
  it('buildEdl sorts segments and stamps version', () => {
    const e = buildEdl({ segments: [{ start: 20, end: 30 }, { start: 0, end: 10 }], crop: { x: 0, y: 0, w: 1, h: 1 }, rotate: 0, flip: 'none' })
    expect(e.version).toBe(1)
    expect(e.segments[0].start).toBe(0)
  })
})

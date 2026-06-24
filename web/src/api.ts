// Same-origin client for the admin BFF (/admin/*). Cookies carry the session,
// so credentials must be included on every call.

import * as tus from 'tus-js-client'

export type VideoStatus = 0 | 1 | 2 | 3 | 4 | 5

export interface Video {
  videoId: string
  libraryId: string
  title: string
  description?: string
  tags?: string[]
  folderId?: string | null
  status: VideoStatus
  length?: number
  width?: number
  height?: number
  storageSize?: number
  availableResolutions?: string
  thumbnailFileName?: string
  encodeProgress: number
  errorMessage?: string
  hlsUrl?: string
  posterUrl?: string
}

export interface Chapter {
  start: number
  title: string
}

export interface Caption {
  lang: string
  label: string
  url?: string
}

export interface PlayData {
  videoId: string
  isReady: boolean
  status: VideoStatus
  length?: number
  hlsUrl?: string
  posterUrl?: string
  thumbnailsUrl?: string
  chaptersUrl?: string
  chapters?: Chapter[]
  captions?: Caption[]
  mp4Url?: string // progressive MP4 fallback (HLS-less clients)
  downloadUrl?: string // signed original download (when AllowDownload)
  earlyPlay?: boolean // original is playable before encoding finishes
  earlyPlayUrl?: string
}

export interface Folder {
  id: string
  libraryId: string
  parentId: string | null
  name: string
  createdAt: string
}

export interface Captions {
  color: string
  background: string
  fontSize: number
}

export interface PlayerConfig {
  language: string
  fontFamily: string
  primaryColor: string
  captions: Captions
  controls: string[]
  playbackSpeeds: number[]
  defaultSpeed: number
  customCSS: string
  showHeatmap: boolean
  resumePlayback: boolean
  compactControls: boolean
}

// --- Encoding settings (library-wide; Bunny "Encoding Tier" controls) ---
export interface WatermarkConfig {
  enabled: boolean
  object: string
  position: string
  opacity: number
  margin: number
}

export interface EncodingConfig {
  resolutions: string[]
  codecs: string[]
  mp4Fallback: boolean
  allowDownload: boolean
  earlyPlay: boolean
  multiAudio: boolean
  watermark: WatermarkConfig
}

export interface TrashItem {
  videoId: string
  title: string
  status: VideoStatus
  length?: number
  storageSize?: number
  availableResolutions?: string
  deletedAt?: string
  purgeAt?: string
}

export interface ApiKey {
  id: string
  libraryId: string
  name: string
  scopes: string[]
  createdAt: string
  lastUsedAt?: string
  revokedAt?: string
}

// Returned ONCE at creation — `key` is the plaintext, unrecoverable afterwards.
export interface ApiKeyCreated {
  id: string
  name: string
  scopes: string[]
  key: string
}

export interface WebhookEndpoint {
  id: string
  libraryId: string
  url: string
  events: string[]
  active: boolean
  createdAt: string
}

// Returned ONCE at creation — `secret` is the HMAC signing secret.
export interface WebhookCreated {
  id: string
  url: string
  events: string[]
  secret: string
}

// CountryStat is one row of the per-country engagement breakdown.
export interface CountryStat {
  country: string // ISO-3166 alpha-2 (from CF-IPCountry)
  starts: number
  sessions: number
  watchSeconds: number
}

export interface VideoAnalytics {
  sessions: number
  starts: number
  avgStartupMs: number
  rebuffers: number
  errors: number
  completions: number
  totalWatchSeconds: number
  avgWatchSeconds: number
  estBandwidthBytes: number // ESTIMATE (segments served by the CDN, not metered here)
  byCountry: CountryStat[]
}

export interface TopVideo {
  videoId: string
  title: string
  sessions: number
  starts: number
}

export interface DailyPoint {
  date: string
  sessions: number
  starts: number
}

export interface LibraryAnalytics extends VideoAnalytics {
  topVideos: TopVideo[]
  daily: DailyPoint[]
}

// Time window for the analytics screens. Mirrors the backend's ?range= values.
export type AnalyticsRange = '7d' | '30d' | '90d' | 'all'

// ViewerProgressRow is one viewer's saved state for a video (resume point +
// completion). Returned per-video (viewerId set) or per-viewer history (title set).
export interface ViewerProgressRow {
  videoId: string
  title?: string
  viewerId?: string
  position: number
  duration?: number
  watchedPercent: number
  completed: boolean
  lastWatchedAt: string
}

export type Visibility = 'public' | 'signed' | 'private'

export interface AccessPolicy {
  visibility: Visibility
  allowedReferrers?: string[]
  expiresAt?: string | null
}

// Known webhook event types the backend dispatches. These strings are part of
// the public API contract (internal/webhooks/webhooks.go) — keep them in sync.
export const WEBHOOK_EVENTS = [
  'video.created',
  'video.uploaded',
  'video.encoded',
  'video.av1_ready',
  'video.captioned',
  'video.indexed',
  'video.enriched',
  'video.encrypted',
  'video.failed',
  'video.deleted',
] as const

// --- In-video search ---
export interface SearchHit {
  videoId: string
  title: string
  lang: string
  startSec: number
  endSec: number
  snippet: string
  score: number
}

export interface SearchSettings {
  enabled: boolean
  provider: string
  model: string
  baseUrl: string
  chunkSeconds: number
  showInPlayer: boolean
  hasApiKey: boolean
}

export interface SearchProvider {
  id: string
  defaultModel: string
  needsApiKey: boolean
  needsBaseUrl: boolean
}

// Payload for saving settings — apiKey is write-only (blank keeps the stored one).
export interface SearchSettingsUpdate {
  enabled: boolean
  provider: string
  model: string
  apiKey?: string
  baseUrl: string
  chunkSeconds: number
  showInPlayer: boolean
}

// --- AI content (LLM router) ---
export interface LlmSettings {
  enabled: boolean
  baseUrl: string
  model: string
  temperature: number
  maxTokens: number
  hasApiKey: boolean
}

export interface LlmSettingsUpdate {
  enabled: boolean
  baseUrl: string
  model: string
  apiKey?: string
  temperature: number
  maxTokens: number
}

export type AiContentKind = 'summary' | 'tags' | 'chapters'

// Backend operation kinds (video_operations.kind) and their lifecycle states.
export type OperationKind = 'av1' | 'hevc' | 'vp9' | 'caption' | 'ai_content' | 'encrypt' | 'search_index' | 'poster'
export type OperationStatus = 'queued' | 'running' | 'done' | 'failed'
export interface VideoOperation {
  kind: OperationKind
  status: OperationStatus
  error?: string
  updatedAt: string
}

const JSON_HEADERS = { 'Content-Type': 'application/json' }

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, { credentials: 'include', ...init })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new ApiError(res.status, body?.error || res.statusText)
  }
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

export const api = {
  me: () =>
    req<{ authenticated: boolean; libraryId: string; embedBaseUrl: string }>('/admin/me'),

  login: (password: string) =>
    req<{ authenticated: boolean }>('/admin/login', {
      method: 'POST',
      headers: JSON_HEADERS,
      body: JSON.stringify({ password }),
    }),

  logout: () => req<unknown>('/admin/logout', { method: 'POST' }),

  // folderId: undefined -> all videos; 'root' -> library root; a UUID -> folder.
  listVideos: (folderId?: string) =>
    req<Video[]>(`/admin/videos${folderId ? `?folderId=${encodeURIComponent(folderId)}` : ''}`),

  createVideo: (title: string, folderId?: string | null, editSpec?: unknown) =>
    req<{ videoId: string; status: VideoStatus }>('/admin/videos', {
      method: 'POST',
      headers: JSON_HEADERS,
      body: JSON.stringify({ title, folderId: folderId ?? undefined, editSpec }),
    }),

  // --- Folders ---
  listFolders: () => req<Folder[]>('/admin/folders'),

  createFolder: (name: string, parentId?: string | null) =>
    req<Folder>('/admin/folders', {
      method: 'POST',
      headers: JSON_HEADERS,
      body: JSON.stringify({ name, parentId: parentId ?? null }),
    }),

  renameFolder: (id: string, name: string) =>
    req<Folder>(`/admin/folders/${id}`, {
      method: 'PUT',
      headers: JSON_HEADERS,
      body: JSON.stringify({ name }),
    }),

  moveFolder: (id: string, parentId: string | null) =>
    req<Folder>(`/admin/folders/${id}`, {
      method: 'PUT',
      headers: JSON_HEADERS,
      body: JSON.stringify({ parentId }),
    }),

  deleteFolder: (id: string) => req<unknown>(`/admin/folders/${id}`, { method: 'DELETE' }),

  moveVideo: (id: string, folderId: string | null) =>
    req<{ videoId: string; folderId: string | null }>(`/admin/videos/${id}/folder`, {
      method: 'PUT',
      headers: JSON_HEADERS,
      body: JSON.stringify({ folderId }),
    }),

  // --- Player settings (library-wide) ---
  getPlayerSettings: () =>
    req<{ config: PlayerConfig; allControls: string[] }>('/admin/player-settings'),

  setPlayerSettings: (config: PlayerConfig) =>
    req<PlayerConfig>('/admin/player-settings', {
      method: 'PUT',
      headers: JSON_HEADERS,
      body: JSON.stringify(config),
    }),

  // --- Encoding settings (library-wide) ---
  getEncodingSettings: () =>
    req<{ config: EncodingConfig; allResolutions: string[]; allCodecs: string[] }>(
      '/admin/encoding-settings',
    ),

  setEncodingSettings: (config: EncodingConfig) =>
    req<EncodingConfig>('/admin/encoding-settings', {
      method: 'PUT',
      headers: JSON_HEADERS,
      body: JSON.stringify(config),
    }),

  uploadWatermark: (file: File) =>
    req<EncodingConfig>('/admin/encoding-settings/watermark', {
      method: 'PUT',
      headers: { 'Content-Type': file.type || 'image/png' },
      body: file,
    }),

  deleteWatermark: () =>
    req<EncodingConfig>('/admin/encoding-settings/watermark', { method: 'DELETE' }),

  // --- In-video search ---
  search: (q: string, videoId?: string) =>
    req<{ enabled: boolean; results: SearchHit[] }>(
      `/admin/search?q=${encodeURIComponent(q)}${videoId ? `&videoId=${encodeURIComponent(videoId)}` : ''}`,
    ),

  getSearchSettings: () =>
    req<{ config: SearchSettings; providers: SearchProvider[] }>('/admin/search-settings'),

  setSearchSettings: (cfg: SearchSettingsUpdate) =>
    req<SearchSettings>('/admin/search-settings', {
      method: 'PUT',
      headers: JSON_HEADERS,
      body: JSON.stringify(cfg),
    }),

  reindexVideo: (id: string, lang?: string) =>
    req<{ videoId: string; status: string }>(
      `/admin/videos/${id}/reindex${lang ? `?lang=${encodeURIComponent(lang)}` : ''}`,
      { method: 'POST' },
    ),

  // --- AI content (LLM) ---
  getLlmSettings: () => req<{ config: LlmSettings }>('/admin/llm-settings'),

  setLlmSettings: (cfg: LlmSettingsUpdate) =>
    req<LlmSettings>('/admin/llm-settings', {
      method: 'PUT',
      headers: JSON_HEADERS,
      body: JSON.stringify(cfg),
    }),

  generateAiContent: (id: string, kinds?: AiContentKind[]) =>
    req<{ videoId: string; status: string }>(
      `/admin/videos/${id}/ai-content${kinds && kinds.length ? `?kinds=${kinds.join(',')}` : ''}`,
      { method: 'POST' },
    ),

  importUrl: (url: string, title?: string) =>
    req<{ videoId: string; status: VideoStatus }>('/admin/videos/import', {
      method: 'POST',
      headers: JSON_HEADERS,
      body: JSON.stringify({ url, title }),
    }),

  getVideo: (id: string) => req<Video & { captions: Caption[] }>(`/admin/videos/${id}`),

  // Patch editable metadata. Only the provided fields are changed server-side.
  updateVideo: (id: string, patch: { title?: string; description?: string; tags?: string[] }) =>
    req<Video>(`/admin/videos/${id}`, {
      method: 'PATCH',
      headers: JSON_HEADERS,
      body: JSON.stringify(patch),
    }),

  // Full signed play payload (hls + thumbnails + captions + chapters). Also used
  // to refresh an expired token mid-playback.
  play: (id: string) => req<PlayData>(`/admin/videos/${id}/play`),

  setChapters: (id: string, chapters: Chapter[]) =>
    req<{ chapters: Chapter[] }>(`/admin/videos/${id}/chapters`, {
      method: 'PUT',
      headers: JSON_HEADERS,
      body: JSON.stringify(chapters),
    }),

  uploadCaption: (id: string, lang: string, label: string, content: string) =>
    req<{ lang: string; label: string }>(
      `/admin/videos/${id}/captions?lang=${encodeURIComponent(lang)}&label=${encodeURIComponent(label)}`,
      { method: 'POST', headers: { 'Content-Type': 'text/vtt' }, body: content },
    ),

  deleteCaption: (id: string, lang: string) =>
    req<unknown>(`/admin/videos/${id}/captions/${lang}`, { method: 'DELETE' }),

  // Soft-delete -> trash (recoverable). Hard purge via purge().
  deleteVideo: (id: string) => req<unknown>(`/admin/videos/${id}`, { method: 'DELETE' }),

  listTrash: () => req<{ retentionDays: number; videos: TrashItem[] }>('/admin/trash'),

  restore: (id: string) => req<unknown>(`/admin/videos/${id}/restore`, { method: 'POST' }),

  purge: (id: string) => req<unknown>(`/admin/videos/${id}/purge`, { method: 'DELETE' }),

  // --- API keys (admin library) ---
  listApiKeys: () => req<ApiKey[]>('/admin/api-keys'),

  createApiKey: (name: string) =>
    req<ApiKeyCreated>('/admin/api-keys', {
      method: 'POST',
      headers: JSON_HEADERS,
      body: JSON.stringify({ name }),
    }),

  revokeApiKey: (id: string) => req<unknown>(`/admin/api-keys/${id}`, { method: 'DELETE' }),

  // --- Webhooks (admin library) ---
  listWebhooks: () => req<WebhookEndpoint[]>('/admin/webhooks'),

  createWebhook: (url: string, events: string[]) =>
    req<WebhookCreated>('/admin/webhooks', {
      method: 'POST',
      headers: JSON_HEADERS,
      body: JSON.stringify({ url, events }),
    }),

  deleteWebhook: (id: string) => req<unknown>(`/admin/webhooks/${id}`, { method: 'DELETE' }),

  // --- Analytics ---
  getLibraryAnalytics: (range: AnalyticsRange = '30d') =>
    req<LibraryAnalytics>(`/admin/analytics?range=${range}`),

  getVideoAnalytics: (id: string, range: AnalyticsRange = '30d') =>
    req<VideoAnalytics>(`/admin/videos/${id}/analytics?range=${range}`),

  getVideoViewers: (id: string) => req<ViewerProgressRow[]>(`/admin/videos/${id}/viewers`),

  // --- Custom poster ---
  // Upload an image as the poster (synchronous). Raw body like uploadWatermark.
  uploadPoster: (id: string, file: File) =>
    req<{ videoId: string; posterUrl: string }>(`/admin/videos/${id}/poster`, {
      method: 'PUT',
      headers: { 'Content-Type': file.type || 'image/jpeg' },
      body: file,
    }),

  // Grab a frame at `seconds` as the poster (worker job; poll operations for done).
  setPosterFromFrame: (id: string, seconds: number) =>
    req<{ videoId: string; atSeconds: number }>(
      `/admin/videos/${id}/poster/frame?t=${encodeURIComponent(seconds)}`,
      { method: 'POST' },
    ),

  // --- Advanced per-video operations (bulk lanes) ---
  generateAV1: (id: string) =>
    req<{ videoId: string; codec: string }>(`/admin/videos/${id}/av1`, { method: 'POST' }),

  // Opt a video into an extra-codec backfill: 'av1' | 'hevc' | 'vp9'.
  generateCodec: (id: string, codec: 'av1' | 'hevc' | 'vp9') =>
    req<{ videoId: string; codec: string }>(`/admin/videos/${id}/codecs/${codec}`, {
      method: 'POST',
    }),

  // Signed original-file download URL (when AllowDownload is on for the video).
  download: (id: string) =>
    req<{ videoId?: string; downloadUrl?: string; error?: string }>(
      `/admin/videos/${id}/download`,
    ),

  autoCaption: (id: string, lang?: string) =>
    req<{ videoId: string; lang: string }>(
      `/admin/videos/${id}/captions/auto${lang ? `?lang=${encodeURIComponent(lang)}` : ''}`,
      { method: 'POST' },
    ),

  encrypt: (id: string) =>
    req<{ videoId: string; encryptionMode: string }>(`/admin/videos/${id}/encrypt`, {
      method: 'POST',
    }),

  // Live status of advanced operations so the UI can show queued/running/done/
  // failed instead of letting the user re-trigger a job that's already running.
  getVideoOperations: (id: string) =>
    req<{ operations: VideoOperation[] }>(`/admin/videos/${id}/operations`),

  setAccess: (id: string, policy: AccessPolicy) =>
    req<{ videoId: string; visibility: Visibility }>(`/admin/videos/${id}/access`, {
      method: 'PUT',
      headers: JSON_HEADERS,
      body: JSON.stringify(policy),
    }),

  // Resumable upload via tus. Chunks survive a refresh/disconnect: tus-js-client
  // fingerprints the file and resumes from the last acked offset if the same
  // file is picked again. Same-origin to /tus/ so the session cookie applies.
  // Returns the Upload so the caller can abort if needed.
  uploadSource(
    id: string,
    file: File,
    onProgress: (pct: number) => void,
  ): Promise<void> {
    return new Promise((resolve, reject) => {
      const upload = new tus.Upload(file, {
        endpoint: '/tus/',
        retryDelays: [0, 1000, 3000, 5000, 10000],
        chunkSize: 8 * 1024 * 1024, // 8 MiB chunks
        removeFingerprintOnSuccess: true,
        metadata: {
          filename: file.name,
          filetype: file.type || 'application/octet-stream',
          videoId: id,
        },
        onError: (err) => reject(new ApiError(0, err.message || 'upload failed')),
        onProgress: (sent, total) =>
          onProgress(total > 0 ? Math.round((sent / total) * 100) : 0),
        onSuccess: () => resolve(),
      })
      // Resume a prior interrupted upload of the same file if one exists.
      upload.findPreviousUploads().then((prev) => {
        if (prev.length > 0) upload.resumeFromPreviousUpload(prev[0])
        upload.start()
      })
    })
  },
}

export const STATUS_LABEL: Record<VideoStatus, string> = {
  0: 'created',
  1: 'uploaded',
  2: 'processing',
  3: 'transcoding',
  4: 'ready',
  5: 'failed',
}

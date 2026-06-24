/**
 * @vodstack/stream-sdk — a thin TypeScript client for the vodstack video API.
 *
 * Uses the global `fetch` (Node 18+ / browsers). Authenticates with a per-library
 * API key. See deploy/openapi.yaml for the full surface.
 *
 *   const fz = new Vodstack({ baseUrl: "https://stream.example.com", libraryId: "default", apiKey: "vds_..." })
 *   const { videoId } = await fz.createVideo("My lesson")
 *   await fz.uploadFile(videoId, fileBytes)   // create → presigned PUT → complete
 *   const play = await fz.play(videoId)        // signed hlsUrl, captions, poster…
 */

export interface VodstackOptions {
  baseUrl: string
  libraryId: string
  apiKey: string
  fetch?: typeof fetch
}

export type VideoStatus = 0 | 1 | 2 | 3 | 4 | 5

export interface Video {
  videoId: string
  libraryId: string
  title: string
  status: VideoStatus
  length?: number
  width?: number
  height?: number
  storageSize?: number
  availableResolutions?: string
  encodeProgress: number
  errorMessage?: string
}

export interface Caption {
  lang: string
  label: string
  url: string
}

export interface PlayResponse {
  videoId: string
  isReady: boolean
  status: VideoStatus
  length?: number
  hlsUrl?: string
  posterUrl?: string
  thumbnailsUrl?: string
  chaptersUrl?: string
  captions?: Caption[]
  mp4Url?: string // progressive MP4 fallback (HLS-less clients)
  downloadUrl?: string // signed original download (when AllowDownload is enabled)
  earlyPlay?: boolean // original is playable before encoding finishes
  earlyPlayUrl?: string
}

export interface UploadURL {
  url: string
  method: string
  object: string
  expires: number
}

export interface Folder {
  id: string
  libraryId: string
  parentId: string | null
  name: string
  createdAt: string
}

export interface SearchHit {
  videoId: string
  title: string
  lang: string
  startSec: number
  endSec: number
  snippet: string
  score: number
}

export interface SearchResponse {
  enabled: boolean
  results: SearchHit[]
}

export class VodstackError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
    this.name = 'VodstackError'
  }
}

export class Vodstack {
  private readonly base: string
  private readonly libraryId: string
  private readonly apiKey: string
  private readonly _fetch: typeof fetch

  constructor(opts: VodstackOptions) {
    this.base = opts.baseUrl.replace(/\/$/, '')
    this.libraryId = opts.libraryId
    this.apiKey = opts.apiKey
    this._fetch = opts.fetch ?? fetch
  }

  private libPath(suffix = ''): string {
    return `${this.base}/api/library/${encodeURIComponent(this.libraryId)}${suffix}`
  }

  private async req<T>(path: string, init: RequestInit = {}): Promise<T> {
    const res = await this._fetch(path, {
      ...init,
      headers: {
        AccessKey: this.apiKey,
        ...(init.body ? { 'Content-Type': 'application/json' } : {}),
        ...(init.headers ?? {}),
      },
    })
    if (!res.ok) {
      const body = (await res.json().catch(() => ({}))) as { error?: string }
      throw new VodstackError(res.status, body?.error || res.statusText)
    }
    if (res.status === 204) return undefined as T
    return (await res.json()) as T
  }

  /** Create a video record (status 0, awaiting upload). */
  createVideo(
    title: string,
    opts: { collectionId?: string; folderId?: string } = {},
  ): Promise<{ videoId: string; libraryId: string; status: VideoStatus }> {
    return this.req(this.libPath('/videos'), {
      method: 'POST',
      body: JSON.stringify({ title, collectionId: opts.collectionId, folderId: opts.folderId }),
    })
  }

  // --- Folders (nested organization) ---

  /** List every folder in the library (assemble the tree from parentId). */
  listFolders(): Promise<Folder[]> {
    return this.req(this.libPath('/folders'))
  }

  /** Create a folder; parentId null/omitted creates it at the root. */
  createFolder(name: string, parentId: string | null = null): Promise<Folder> {
    return this.req(this.libPath('/folders'), {
      method: 'POST',
      body: JSON.stringify({ name, parentId }),
    })
  }

  /** Rename a folder. */
  renameFolder(folderId: string, name: string): Promise<Folder> {
    return this.req(this.libPath(`/folders/${folderId}`), {
      method: 'PUT',
      body: JSON.stringify({ name }),
    })
  }

  /** Reparent a folder; parentId null moves it to the root. */
  moveFolder(folderId: string, parentId: string | null): Promise<Folder> {
    return this.req(this.libPath(`/folders/${folderId}`), {
      method: 'PUT',
      body: JSON.stringify({ parentId }),
    })
  }

  /** Delete a folder (sub-folders cascade; videos fall back to the root). */
  deleteFolder(folderId: string): Promise<void> {
    return this.req(this.libPath(`/folders/${folderId}`), { method: 'DELETE' })
  }

  /** Move a video into a folder; folderId null moves it to the library root. */
  moveVideo(videoId: string, folderId: string | null): Promise<{ videoId: string; folderId: string | null }> {
    return this.req(this.libPath(`/videos/${videoId}/folder`), {
      method: 'PUT',
      body: JSON.stringify({ folderId }),
    })
  }

  /** Get a presigned PUT URL for the raw source. */
  uploadUrl(videoId: string): Promise<UploadURL> {
    return this.req(this.libPath(`/videos/${videoId}/upload-url`), { method: 'POST' })
  }

  /** Mark the upload complete and enqueue transcoding. */
  complete(videoId: string): Promise<{ videoId: string; status: VideoStatus }> {
    return this.req(this.libPath(`/videos/${videoId}/complete`), { method: 'POST' })
  }

  /** Ingest from a source URL (migrate an existing Bunny/Vimeo/any MP4). */
  fetchUrl(videoId: string, url: string): Promise<{ videoId: string; status: VideoStatus }> {
    return this.req(this.libPath(`/videos/${videoId}/fetch`), {
      method: 'POST',
      body: JSON.stringify({ url }),
    })
  }

  getVideo(videoId: string): Promise<Video> {
    return this.req(this.libPath(`/videos/${videoId}`))
  }

  /** Mint a signed playback payload (hlsUrl, poster, captions, chapters). */
  play(videoId: string): Promise<PlayResponse> {
    return this.req(this.libPath(`/videos/${videoId}/play`))
  }

  /** Soft-delete (recoverable trash). */
  deleteVideo(videoId: string): Promise<void> {
    return this.req(this.libPath(`/videos/${videoId}`), { method: 'DELETE' })
  }

  /**
   * Hybrid (semantic + lexical) in-video search over the library's transcripts.
   * Returns transcript moments (videoId + startSec + snippet); pass videoId to
   * restrict to a single video. Requires search to be enabled for the library.
   */
  search(query: string, opts: { videoId?: string } = {}): Promise<SearchResponse> {
    const params = new URLSearchParams({ q: query })
    if (opts.videoId) params.set('videoId', opts.videoId)
    return this.req(this.libPath(`/search?${params.toString()}`))
  }

  /** Build the embeddable iframe URL for a video. */
  embedUrl(videoId: string): string {
    return `${this.base}/embed/${encodeURIComponent(this.libraryId)}/${encodeURIComponent(videoId)}`
  }

  /**
   * Convenience: create (optional) → presigned PUT → complete. Pass a videoId to
   * upload into an existing record, or omit to create one from `title`.
   */
  async uploadFile(
    videoIdOrTitle: { videoId: string } | { title: string },
    body: BodyInit,
    contentType = 'application/octet-stream',
  ): Promise<{ videoId: string }> {
    let videoId: string
    if ('videoId' in videoIdOrTitle) {
      videoId = videoIdOrTitle.videoId
    } else {
      videoId = (await this.createVideo(videoIdOrTitle.title)).videoId
    }
    const up = await this.uploadUrl(videoId)
    const putRes = await this._fetch(up.url, {
      method: 'PUT',
      headers: { 'Content-Type': contentType },
      body,
    })
    if (!putRes.ok) {
      throw new VodstackError(putRes.status, 'raw upload failed')
    }
    await this.complete(videoId)
    return { videoId }
  }
}

export default Vodstack

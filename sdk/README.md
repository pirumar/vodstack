# @vodstack/stream-sdk

A thin TypeScript client for the [vodstack](../README.md) video API. Uses the
global `fetch` (Node 18+ or browsers). The full HTTP surface is documented in
[`deploy/openapi.yaml`](../deploy/openapi.yaml).

## Install

```bash
npm install @vodstack/stream-sdk
```

## Usage

```ts
import { Vodstack } from '@vodstack/stream-sdk'

const fz = new Vodstack({
  baseUrl: 'https://stream.example.com',
  libraryId: 'default',
  apiKey: process.env.VODSTACK_API_KEY!, // never ship this to the browser
})

// Create + upload a file in one call (create → presigned PUT → complete).
const { videoId } = await fz.uploadFile({ title: 'My lesson' }, fileBytes, 'video/mp4')

// Poll until ready.
let v = await fz.getVideo(videoId)
while (v.status < 4) {
  await new Promise((r) => setTimeout(r, 2000))
  v = await fz.getVideo(videoId)
}

// Get a signed playback payload (hlsUrl, captions, poster, chapters).
const play = await fz.play(videoId)
console.log(play.hlsUrl)

// Or just embed it.
console.log(fz.embedUrl(videoId)) // -> https://.../embed/default/<id>
```

### Migrate from a URL

```ts
const { videoId } = await fz.createVideo('Imported clip')
await fz.fetchUrl(videoId, 'https://old-cdn.example.com/lesson.mp4')
```

## API

| Method | Description |
| --- | --- |
| `createVideo(title, collectionId?)` | Create a video record |
| `uploadUrl(videoId)` | Presigned PUT URL for the raw source |
| `complete(videoId)` | Mark uploaded → enqueue transcode |
| `fetchUrl(videoId, url)` | Ingest from a source URL |
| `getVideo(videoId)` | Metadata + status |
| `play(videoId)` | Signed playback payload |
| `deleteVideo(videoId)` | Soft-delete (recoverable) |
| `embedUrl(videoId)` | Build the iframe embed URL |
| `uploadFile(idOrTitle, body, contentType?)` | create → PUT → complete helper |

Errors throw `VodstackError` with `.status` and `.message`.

> The API key authorizes one library; keep it server-side. For browser playback
> use the signed `hlsUrl` from `play()` or the `embedUrl()` iframe — neither
> exposes the key.

import { useEffect, useState } from 'react'
import { SlidersHorizontal } from 'lucide-react'
import { api } from '../api'
import { useLibrary } from '../LibraryContext'
import { PageHeader } from '../components/ui'
import { PlayerSettings } from '../components/PlayerSettings'

export function PlayerSettingsPage() {
  const { embedBaseUrl, libraryId } = useLibrary()
  const [previewVideoId, setPreviewVideoId] = useState<string | undefined>()

  useEffect(() => {
    api
      .listVideos()
      .then((vs) => setPreviewVideoId(vs.find((v) => v.status === 4)?.videoId))
      .catch(() => {})
  }, [])

  return (
    <div>
      <PageHeader
        eyebrow="oynatıcı"
        title="Player Ayarları"
        icon={<SlidersHorizontal className="h-5 w-5" />}
      />
      <PlayerSettings embedBaseUrl={embedBaseUrl} libraryId={libraryId} previewVideoId={previewVideoId} />
    </div>
  )
}

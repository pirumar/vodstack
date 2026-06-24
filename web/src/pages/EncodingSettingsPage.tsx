import { Film } from 'lucide-react'
import { PageHeader } from '../components/ui'
import { EncodingSettings } from '../components/EncodingSettings'

export function EncodingSettingsPage() {
  return (
    <div>
      <PageHeader
        eyebrow="kodlama"
        title="Kodlama Ayarları"
        icon={<Film className="h-5 w-5" />}
      />
      <div className="mx-auto max-w-2xl">
        <EncodingSettings />
      </div>
    </div>
  )
}

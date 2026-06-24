import { Trash2 } from 'lucide-react'
import { PageHeader } from '../components/ui'
import { TrashView } from '../components/TrashView'

export function TrashPage() {
  return (
    <div>
      <PageHeader eyebrow="geri dönüşüm" title="Çöp" icon={<Trash2 className="h-5 w-5" />} />
      <TrashView />
    </div>
  )
}

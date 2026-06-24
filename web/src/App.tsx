import { useEffect, useState } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { api } from './api'
import { LibraryProvider } from './LibraryContext'
import { Login } from './components/Login'
import { Layout } from './components/Layout'
import { OverviewPage } from './pages/OverviewPage'
import { LibraryPage } from './pages/LibraryPage'
import { AnalyticsPage } from './pages/AnalyticsPage'
import { VideoStatsPage } from './pages/VideoStatsPage'
import { ApiKeysPage } from './pages/ApiKeysPage'
import { WebhooksPage } from './pages/WebhooksPage'
import { PlayerSettingsPage } from './pages/PlayerSettingsPage'
import { EncodingSettingsPage } from './pages/EncodingSettingsPage'
import { SearchPage } from './pages/SearchPage'
import { TrashPage } from './pages/TrashPage'
import { DocsPage } from './pages/DocsPage'

type Auth =
  | { state: 'loading' }
  | { state: 'out' }
  | { state: 'in'; libraryId: string; embedBaseUrl: string }

export default function App() {
  const [auth, setAuth] = useState<Auth>({ state: 'loading' })

  async function check() {
    try {
      const me = await api.me()
      setAuth({ state: 'in', libraryId: me.libraryId, embedBaseUrl: me.embedBaseUrl })
    } catch {
      setAuth({ state: 'out' })
    }
  }

  useEffect(() => {
    check()
  }, [])

  async function logout() {
    await api.logout().catch(() => {})
    setAuth({ state: 'out' })
  }

  if (auth.state === 'loading') {
    return (
      <div className="grid min-h-full place-items-center">
        <div className="h-6 w-6 animate-spin rounded-full border-2 border-edge border-t-signal" />
      </div>
    )
  }

  if (auth.state === 'out') {
    return <Login onAuthed={check} />
  }

  return (
    <LibraryProvider value={{ libraryId: auth.libraryId, embedBaseUrl: auth.embedBaseUrl, logout }}>
      <BrowserRouter>
        <Routes>
          <Route element={<Layout />}>
            <Route index element={<Navigate to="/overview" replace />} />
            <Route path="/overview" element={<OverviewPage />} />
            <Route path="/library" element={<LibraryPage />} />
            <Route path="/library/:videoId" element={<LibraryPage />} />
            <Route path="/search" element={<SearchPage />} />
            <Route path="/analytics" element={<AnalyticsPage />} />
            <Route path="/videos/:videoId/stats" element={<VideoStatsPage />} />
            <Route path="/api-keys" element={<ApiKeysPage />} />
            <Route path="/webhooks" element={<WebhooksPage />} />
            <Route path="/docs" element={<DocsPage />} />
            <Route path="/player" element={<PlayerSettingsPage />} />
            <Route path="/encoding" element={<EncodingSettingsPage />} />
            <Route path="/trash" element={<TrashPage />} />
            <Route path="*" element={<Navigate to="/overview" replace />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </LibraryProvider>
  )
}

import { useState } from 'react'
import { NavLink, Outlet } from 'react-router-dom'
import {
  Radio,
  LayoutDashboard,
  Library,
  Search,
  BarChart3,
  KeyRound,
  Webhook,
  SlidersHorizontal,
  Film,
  Trash2,
  BookOpen,
  LogOut,
  Menu,
  X,
  Sun,
  Moon,
} from 'lucide-react'
import { useLibrary } from '../LibraryContext'
import { useTheme } from '../ThemeContext'

const NAV = [
  { to: '/overview', label: 'Genel Bakış', icon: LayoutDashboard },
  { to: '/library', label: 'Kütüphane', icon: Library },
  { to: '/search', label: 'Arama', icon: Search },
  { to: '/analytics', label: 'Analitik', icon: BarChart3 },
  { to: '/api-keys', label: 'API Anahtarları', icon: KeyRound },
  { to: '/webhooks', label: "Webhook'lar", icon: Webhook },
  { to: '/docs', label: 'Entegrasyon', icon: BookOpen },
  { to: '/player', label: 'Player', icon: SlidersHorizontal },
  { to: '/encoding', label: 'Kodlama', icon: Film },
  { to: '/trash', label: 'Çöp', icon: Trash2 },
]

export function Layout() {
  const { libraryId, logout } = useLibrary()
  const [open, setOpen] = useState(false)

  return (
    <div className="deck-grid grain min-h-full">
      {/* Mobile top bar */}
      <header className="sticky top-0 z-40 flex items-center gap-3 border-b border-edge/70 bg-ink/85 px-4 py-3 backdrop-blur-md sm:hidden">
        <button
          onClick={() => setOpen((v) => !v)}
          className="grid h-9 w-9 place-items-center rounded-lg border border-edge text-haze"
          aria-label="Menü"
        >
          {open ? <X className="h-4 w-4" /> : <Menu className="h-4 w-4" />}
        </button>
        <Brand libraryId={libraryId} />
        <div className="ml-auto">
          <ThemeToggle />
        </div>
      </header>

      <div className="flex">
        {/* Sidebar */}
        <aside
          className={`fixed inset-y-0 left-0 z-30 w-60 transform border-r border-edge/70 bg-graphite/95 backdrop-blur-md transition-transform sm:sticky sm:top-0 sm:h-screen sm:translate-x-0 ${
            open ? 'translate-x-0' : '-translate-x-full'
          }`}
        >
          <div className="flex h-full flex-col">
            <div className="hidden items-center gap-3 px-5 py-5 sm:flex">
              <Brand libraryId={libraryId} />
            </div>

            <nav className="flex-1 space-y-1 overflow-y-auto px-3 py-4 sm:py-2">
              {NAV.map(({ to, label, icon: Icon }) => (
                <NavLink
                  key={to}
                  to={to}
                  onClick={() => setOpen(false)}
                  className={({ isActive }) =>
                    `flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition ${
                      isActive
                        ? 'bg-signal/15 text-signal ring-1 ring-signal/30'
                        : 'text-haze hover:bg-edge/50 hover:text-chalk'
                    }`
                  }
                >
                  <Icon className="h-4 w-4" />
                  {label}
                </NavLink>
              ))}
            </nav>

            <div className="space-y-1 border-t border-edge/70 p-3">
              <ThemeToggle variant="row" />
              <button
                onClick={logout}
                className="flex w-full items-center gap-2 rounded-lg px-3 py-2.5 text-sm font-medium text-haze transition hover:bg-bad/15 hover:text-bad"
              >
                <LogOut className="h-4 w-4" /> Çıkış
              </button>
            </div>
          </div>
        </aside>

        {/* Backdrop for mobile drawer */}
        {open && (
          <div
            onClick={() => setOpen(false)}
            className="fixed inset-0 z-20 bg-ink/60 backdrop-blur-sm sm:hidden"
          />
        )}

        {/* Content */}
        <main className="min-w-0 flex-1 px-5 py-8 sm:px-8 lg:px-10 2xl:px-12">
          <Outlet />
        </main>
      </div>
    </div>
  )
}

function ThemeToggle({ variant = 'icon' }: { variant?: 'icon' | 'row' }) {
  const { theme, toggleTheme } = useTheme()
  const isDark = theme === 'dark'
  const Icon = isDark ? Sun : Moon
  const label = isDark ? 'Açık tema' : 'Koyu tema'

  if (variant === 'row') {
    return (
      <button
        onClick={toggleTheme}
        className="flex w-full items-center gap-2 rounded-lg px-3 py-2.5 text-sm font-medium text-haze transition hover:bg-edge/50 hover:text-chalk"
      >
        <Icon className="h-4 w-4" /> {label}
      </button>
    )
  }

  return (
    <button
      onClick={toggleTheme}
      className="grid h-9 w-9 place-items-center rounded-lg border border-edge text-haze transition hover:border-signal/40 hover:text-chalk"
      aria-label={label}
      title={label}
    >
      <Icon className="h-4 w-4" />
    </button>
  )
}

function Brand({ libraryId }: { libraryId: string }) {
  return (
    <div className="flex items-center gap-3">
      <span className="grid h-9 w-9 place-items-center rounded-lg bg-signal/15 ring-1 ring-signal/30">
        <Radio className="h-4.5 w-4.5 text-signal" />
      </span>
      <div className="leading-tight">
        <div className="font-display text-[15px] font-semibold tracking-tight">
          vodstack<span className="text-signal">/</span>stream
        </div>
        <div className="font-mono text-[10px] uppercase tracking-[0.22em] text-haze">
          lib · {libraryId}
        </div>
      </div>
    </div>
  )
}

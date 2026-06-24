/** @type {import('tailwindcss').Config} */

// Colors resolve to CSS custom properties so a single `.light`/`.dark` class on
// <html> repaints the whole palette. RGB channels + <alpha-value> keep Tailwind
// opacity modifiers (e.g. bg-ink/70) working. Channel values live in index.css.
const v = (name) => `rgb(var(${name}) / <alpha-value>)`

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      screens: {
        '3xl': '1920px',
      },
      colors: {
        // Broadcast-deck palette: graphite/ink base, warm amber "on-air" signal.
        ink: v('--c-ink'),
        graphite: v('--c-graphite'),
        panel: v('--c-panel'),
        edge: v('--c-edge'),
        haze: v('--c-haze'),
        chalk: v('--c-chalk'),
        signal: {
          DEFAULT: v('--c-signal'),
          soft: v('--c-signal-soft'),
          deep: v('--c-signal-deep'),
        },
        ok: v('--c-ok'),
        warn: v('--c-warn'),
        bad: v('--c-bad'),
        idle: v('--c-idle'),
      },
      fontFamily: {
        display: ['"Bricolage Grotesque"', 'system-ui', 'sans-serif'],
        sans: ['"Schibsted Grotesk"', 'system-ui', 'sans-serif'],
        mono: ['"JetBrains Mono"', 'ui-monospace', 'monospace'],
      },
      boxShadow: {
        // Theme-aware: heavy black drop in dark, soft slate drop in light.
        deck: 'var(--shadow-deck)',
        glow: '0 0 0 1px rgba(255,158,44,0.4), 0 8px 30px -8px rgba(255,158,44,0.35)',
      },
      keyframes: {
        'pulse-rec': {
          '0%,100%': { opacity: '1' },
          '50%': { opacity: '0.35' },
        },
        shimmer: {
          '100%': { transform: 'translateX(100%)' },
        },
      },
      animation: {
        'pulse-rec': 'pulse-rec 1.4s ease-in-out infinite',
        shimmer: 'shimmer 1.6s infinite',
      },
    },
  },
  plugins: [],
}

import type { Config } from 'tailwindcss'

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        border: 'hsl(214 32% 91%)',
        background: 'hsl(210 40% 98%)',
        foreground: 'hsl(222 47% 11%)',
        muted: 'hsl(215 16% 47%)',
        primary: 'hsl(166 72% 28%)',
        accent: 'hsl(24 94% 50%)'
      },
      boxShadow: {
        panel: '0 18px 48px rgba(15, 23, 42, 0.08)'
      }
    }
  },
  plugins: []
} satisfies Config

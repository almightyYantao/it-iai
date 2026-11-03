/** @type {import('tailwindcss').Config} */
// Vercel-style theme. Every colour is a CSS variable (RGB channels) defined in
// index.css for light and `.dark`, so the semantic names stay theme-agnostic.
const v = (name) => `rgb(var(--${name}) / <alpha-value>)`;
export default {
  darkMode: "class",
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      fontFamily: {
        sans: ['Geist', 'Inter', 'ui-sans-serif', 'system-ui', '-apple-system', 'sans-serif'],
        mono: ['"Geist Mono"', '"JetBrains Mono"', 'ui-monospace', 'SFMono-Regular', 'Menlo', 'monospace'],
      },
      colors: {
        canvas: { base: v('bg'), surface: v('bg-2'), raised: v('bg-3'), inset: v('bg-2') },
        line: { subtle: v('line-1'), DEFAULT: v('line-2'), strong: v('line-3') },
        ink: { faint: v('fg-4'), muted: v('fg-3'), DEFAULT: v('fg-2'), strong: v('fg-1') },
        brand: { DEFAULT: v('accent'), hover: v('accent-hover'), ring: v('accent') },
        status: {
          ok:     { bg: v('ok-bg'),   fg: v('ok-fg'),   dot: v('ok-dot') },
          flight: { bg: v('info-bg'), fg: v('info-fg'), dot: v('info-dot') },
          warn:   { bg: v('warn-bg'), fg: v('warn-fg'), dot: v('warn-dot') },
          fail:   { bg: v('fail-bg'), fg: v('fail-fg'), dot: v('fail-dot') },
          idle:   { bg: v('bg-3'),    fg: v('fg-3'),    dot: v('fg-4') },
        },
      },
      ringColor: { brand: 'rgb(var(--accent) / 0.2)' },
      borderRadius: { md: '6px', lg: '8px', xl: '10px', '2xl': '12px' },
      boxShadow: {
        soft: '0 0 0 1px rgb(var(--line-1)), 0 1px 2px rgb(0 0 0 / 0.04)',
        raised: '0 0 0 1px rgb(var(--line-1)), 0 8px 24px -8px rgb(0 0 0 / 0.16)',
      },
      keyframes: {
        'fade-in': { from: { opacity: 0, transform: 'translateY(4px)' }, to: { opacity: 1, transform: 'none' } },
      },
      animation: { 'fade-in': 'fade-in .18s ease-out' },
    },
  },
  plugins: [],
};

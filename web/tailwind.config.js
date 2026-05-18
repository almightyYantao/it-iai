/** @type {import('tailwindcss').Config} */
// Light theme. Tinted neutrals on hue 250 (cool slate), single sky-blue accent.
// The "canvas/line/ink" semantic names are deliberately theme-agnostic so the
// rest of the app doesn't change when we swap palettes.
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      fontFamily: {
        sans: ['Inter', 'ui-sans-serif', 'system-ui', '-apple-system', 'sans-serif'],
        mono: ['"JetBrains Mono"', 'ui-monospace', 'SFMono-Regular', 'Menlo', 'Monaco', 'monospace'],
      },
      colors: {
        canvas: {
          base:    'oklch(99%   0.003 250)',  /* page bg — near-white tinted */
          surface: 'oklch(97.5% 0.005 250)',  /* cards, panels */
          raised:  'oklch(94.5% 0.008 250)',  /* hover, popovers */
          inset:   'oklch(96%   0.005 250)',  /* log pane (slightly recessed) */
        },
        line: {
          subtle:  'oklch(92% 0.008 250)',
          DEFAULT: 'oklch(86% 0.012 250)',
          strong:  'oklch(72% 0.018 250)',
        },
        ink: {
          faint:   'oklch(58% 0.012 250)',
          muted:   'oklch(46% 0.015 250)',
          DEFAULT: 'oklch(28% 0.010 250)',
          strong:  'oklch(15% 0.005 250)',
        },
        brand: {
          DEFAULT: 'oklch(54% 0.18 235)',
          hover:   'oklch(48% 0.20 235)',
          ring:    'oklch(54% 0.18 235 / 0.20)',
        },
        status: {
          ok:     { bg: 'oklch(94% 0.07 155 / 0.60)', fg: 'oklch(36% 0.18 155)', dot: 'oklch(60% 0.20 155)' },
          flight: { bg: 'oklch(94% 0.07 235 / 0.55)', fg: 'oklch(40% 0.18 235)', dot: 'oklch(58% 0.18 235)' },
          warn:   { bg: 'oklch(94% 0.10 75  / 0.55)', fg: 'oklch(42% 0.17 75)',  dot: 'oklch(66% 0.18 75)' },
          fail:   { bg: 'oklch(95% 0.07 25  / 0.55)', fg: 'oklch(46% 0.22 25)',  dot: 'oklch(62% 0.22 25)' },
          idle:   { bg: 'oklch(94% 0.005 250 / 0.7)', fg: 'oklch(46% 0.012 250)', dot: 'oklch(66% 0.015 250)' },
        },
      },
      ringColor: { brand: 'oklch(54% 0.18 235 / 0.20)' },
      boxShadow: {
        // Light theme shadows — restrained, mostly used on the floating
        // "jump to bottom" pill and dropdowns.
        soft:   '0 1px 2px 0 oklch(0% 0 0 / 0.05), 0 1px 3px 0 oklch(0% 0 0 / 0.05)',
        raised: '0 4px 12px -4px oklch(0% 0 0 / 0.10)',
      },
    },
  },
  plugins: [],
};

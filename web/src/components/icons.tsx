// Inline SVG icons — keeps bundle small, no lucide dep. Stroke-based, currentColor.
type P = React.SVGProps<SVGSVGElement>;

const base = (children: React.ReactNode, p: P) => (
  <svg
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth={1.75}
    strokeLinecap="round"
    strokeLinejoin="round"
    aria-hidden
    {...p}
  >
    {children}
  </svg>
);

export const BrandMark = (p: P) => base(<>
  <path d="M12 3 4 8v8l8 5 8-5V8l-8-5Z" />
  <path d="m4 8 8 5 8-5" />
  <path d="M12 13v8" opacity={0.55} />
</>, p);

export const ActivityIcon = (p: P) => base(<>
  <path d="M3 12h3l3-9 4 18 3-9h5" />
</>, p);

export const LayersIcon = (p: P) => base(<>
  <path d="m12 2 9 5-9 5-9-5 9-5Z" />
  <path d="m3 12 9 5 9-5" />
  <path d="m3 17 9 5 9-5" />
</>, p);

export const ScrollIcon = (p: P) => base(<>
  <path d="M8 2h11a2 2 0 0 1 2 2v3a2 2 0 0 1-2 2H8" />
  <path d="M8 22V2" />
  <path d="M3 7v11a3 3 0 0 0 3 3h12" />
</>, p);

export const ExternalIcon = (p: P) => base(<>
  <path d="M7 17 17 7" />
  <path d="M8 7h9v9" />
</>, p);

export const ChevronRight = (p: P) => base(<><path d="m9 6 6 6-6 6" /></>, p);

export const AlertIcon = (p: P) => base(<>
  <path d="M12 9v4" />
  <path d="M12 17h.01" />
  <circle cx={12} cy={12} r={10} />
</>, p);

export const ClipboardIcon = (p: P) => base(<>
  <rect x={9} y={3} width={6} height={4} rx={1} />
  <path d="M9 5H7a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2h-2" />
</>, p);

export const CheckIcon = (p: P) => base(<><path d="m5 13 4 4L19 7" /></>, p);

export const ArrowDownIcon = (p: P) => base(<><path d="M12 5v14" /><path d="m19 12-7 7-7-7" /></>, p);

export const FilterIcon = (p: P) => base(<><path d="M3 5h18" /><path d="M6 12h12" /><path d="M10 19h4" /></>, p);

export const KeycloakIcon = (p: P) => base(<>
  <circle cx={12} cy={12} r={9} />
  <path d="M12 7v10" />
  <path d="m7 12 5-5 5 5-5 5Z" opacity={0.55} />
</>, p);

export const LogoutIcon = (p: P) => base(<>
  <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
  <path d="m16 17 5-5-5-5" />
  <path d="M21 12H9" />
</>, p);

export const RefreshIcon = (p: P) => base(<>
  <path d="M3 12a9 9 0 0 1 15-6.7L21 8" />
  <path d="M21 3v5h-5" />
  <path d="M21 12a9 9 0 0 1-15 6.7L3 16" />
  <path d="M3 21v-5h5" />
</>, p);

export const UsersIcon = (p: P) => base(<>
  <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
  <circle cx={9} cy={7} r={4} />
  <path d="M22 21v-2a4 4 0 0 0-3-3.87" />
  <path d="M16 3.13a4 4 0 0 1 0 7.75" />
</>, p);

export const BookIcon = (p: P) => base(<>
  <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20" />
  <path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2Z" />
</>, p);

export const TerminalIcon = (p: P) => base(<>
  <path d="m4 17 6-6-6-6" />
  <path d="M12 19h8" />
</>, p);

export const ChatIcon = (p: P) => base(<>
  <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2Z" />
</>, p);

export const SparklesIcon = (p: P) => base(<>
  <path d="M12 3v3" />
  <path d="M12 18v3" />
  <path d="M3 12h3" />
  <path d="M18 12h3" />
  <path d="m5.6 5.6 2.1 2.1" />
  <path d="m16.3 16.3 2.1 2.1" />
  <path d="m5.6 18.4 2.1-2.1" />
  <path d="m16.3 7.7 2.1-2.1" />
</>, p);

export const SettingsIcon = (p: P) => base(<>
  <circle cx={12} cy={12} r={3} />
  <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06A2 2 0 1 1 4.21 16.97l.06-.06A1.65 1.65 0 0 0 4.6 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06A2 2 0 1 1 7.04 4.29l.06.06A1.65 1.65 0 0 0 9 4.68 1.65 1.65 0 0 0 10 3.17V3a2 2 0 0 1 4 0v.09A1.65 1.65 0 0 0 15 4.6a1.65 1.65 0 0 0 1.82-.33l.06-.06A2 2 0 1 1 19.71 7.04l-.06.06A1.65 1.65 0 0 0 19.32 9c.1.36.39.62.76.71H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z" />
</>, p);

import type { ReactNode } from 'react'

export interface IconProps {
  className?: string
}

// NavIcon is the shared inline-SVG wrapper for the admin-module nav icons. Each
// icon is a 24×24 stroke outline that inherits currentColor, so the active
// (brand) and inactive nav states color it automatically. It follows the
// EmptyState inline-SVG pattern — no icon library (there is none in
// package.json). Icons stay tiny and legible at ~20px.
export function NavIcon({
  className,
  children,
}: IconProps & { children: ReactNode }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className={className}
    >
      {children}
    </svg>
  )
}

// Übersicht — a dashboard grid of four panels.
export function IconUebersicht({ className }: IconProps) {
  return (
    <NavIcon className={className}>
      <rect x="3" y="3" width="7" height="7" rx="1.5" />
      <rect x="14" y="3" width="7" height="7" rx="1.5" />
      <rect x="3" y="14" width="7" height="7" rx="1.5" />
      <rect x="14" y="14" width="7" height="7" rx="1.5" />
    </NavIcon>
  )
}

// Benutzer — a single person (head + shoulders).
export function IconBenutzer({ className }: IconProps) {
  return (
    <NavIcon className={className}>
      <circle cx="12" cy="8" r="4" />
      <path d="M4 20c1.2-3.2 4.2-5 8-5s6.8 1.8 8 5" />
    </NavIcon>
  )
}

// Benutzergruppen — two people (a group).
export function IconBenutzergruppen({ className }: IconProps) {
  return (
    <NavIcon className={className}>
      <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
      <circle cx="9" cy="7" r="4" />
      <path d="M22 21v-2a4 4 0 0 0-3-3.87" />
      <path d="M16 3.13a4 4 0 0 1 0 7.75" />
    </NavIcon>
  )
}

// Rollen — a key (access/role).
export function IconRollen({ className }: IconProps) {
  return (
    <NavIcon className={className}>
      <circle cx="7" cy="12" r="4.5" />
      <path d="M11 12h10" />
      <path d="M16 12v-3" />
      <path d="M19 12v-2" />
    </NavIcon>
  )
}

// Qualifikationen — a certificate medallion with a hanging ribbon.
export function IconQualifikationen({ className }: IconProps) {
  return (
    <NavIcon className={className}>
      <circle cx="12" cy="10" r="5" />
      <path d="M15 13.5 17.5 21 12 18.5 6.5 21 9 13.5" />
    </NavIcon>
  )
}

// Werkzeuge — a wrench.
export function IconWerkzeuge({ className }: IconProps) {
  return (
    <NavIcon className={className}>
      <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" />
    </NavIcon>
  )
}

// Einstellungen — sliders (three horizontal tracks with knobs).
export function IconEinstellungen({ className }: IconProps) {
  return (
    <NavIcon className={className}>
      <path d="M21 4h-7" />
      <path d="M10 4H3" />
      <path d="M21 12h-9" />
      <path d="M8 12H3" />
      <path d="M21 20h-5" />
      <path d="M12 20H3" />
      <path d="M14 2v4" />
      <path d="M8 10v4" />
      <path d="M16 18v4" />
    </NavIcon>
  )
}

// DSGVO — a shield with a checkmark (data protection).
export function IconDsgvo({ className }: IconProps) {
  return (
    <NavIcon className={className}>
      <path d="M12 3 5 6v6c0 4.5 3 7.5 7 9 4-1.5 7-4.5 7-9V6l-7-3z" />
      <path d="m9 12 2 2 4-4" />
    </NavIcon>
  )
}
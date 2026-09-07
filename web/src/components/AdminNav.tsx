import { useCallback, useEffect, useRef, useState } from 'react'
import { NavLink } from 'react-router-dom'
import type { AdminNavEntry } from '../auth/permissions.ts'
import styles from './AdminNav.module.css'

interface AdminNavProps {
  entries: AdminNavEntry[]
}

// AdminNav is the responsive, permission-filtered sub-navigation of the admin
// module (Story 2.3, UX-DR10/UX-DR6/AD-6). It receives only the entries the
// caller's resolved permission set exposes (the caller computes filteredAdminNav
// so the nav itself never enumerates hidden surfaces, FR-19).
//
// Responsive behaviour:
//   - ≤640px: a hamburger toggle collapses/expands the vertical list
//   - 641–1024px: a persistent vertical sidebar
//   - >1024px: a full vertical sidebar
//
// Mobile-menu accessibility (UX-DR9): pressing Escape closes the menu and
// returns focus to the toggle; clicking a link closes it; clicking/tapping
// outside the nav closes it. Every target is ≥48px, the active entry carries
// aria-current="page", the toggle is keyboard operable with a visible focus
// ring, and the nav is exposed as a landmark (nav/main, heading hierarchy).
export function AdminNav({ entries }: AdminNavProps) {
  const [open, setOpen] = useState(false)
  const navRef = useRef<HTMLElement>(null)
  const toggleRef = useRef<HTMLButtonElement>(null)

  const close = useCallback((): void => setOpen(false), [])

  useEffect(() => {
    if (!open) return

    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key === 'Escape') {
        close()
        toggleRef.current?.focus()
      }
    }
    const onMouseDown = (event: MouseEvent): void => {
      if (navRef.current && !navRef.current.contains(event.target as Node)) {
        close()
      }
    }
    document.addEventListener('keydown', onKeyDown)
    document.addEventListener('mousedown', onMouseDown)
    return () => {
      document.removeEventListener('keydown', onKeyDown)
      document.removeEventListener('mousedown', onMouseDown)
    }
  }, [open, close])

  return (
    <nav ref={navRef} className={styles.nav} aria-label="Verwaltung">
      <button
        ref={toggleRef}
        type="button"
        className={styles.toggle}
        aria-expanded={open}
        aria-controls="admin-nav-list"
        onClick={() => setOpen((v) => !v)}
      >
        Verwaltung
        <span className={styles.toggleIcon} aria-hidden="true">
          {open ? '▴' : '▾'}
        </span>
      </button>
      <ul
        id="admin-nav-list"
        className={`${styles.list} ${open ? styles.open : ''}`}
      >
        {entries.map((entry) => (
          <li key={entry.key}>
            <NavLink
              to={entry.route}
              end={entry.route === '/admin'}
              onClick={close}
              className={({ isActive }) =>
                `${styles.link} ${isActive ? styles.active : ''}`
              }
            >
              {entry.label}
            </NavLink>
          </li>
        ))}
      </ul>
    </nav>
  )
}
import { useState } from 'react'
import type { AdminUserSummary, UserStatusFilter } from '../auth/users.ts'
import { USER_STATUS_FILTERS, userStatusLabel } from '../auth/users.ts'
import styles from './UserTable.module.css'

// The sortable columns of the spreadsheet. Each maps to a summary field; the
// Status column sorts on the raw state code.
type SortKey = 'vorname' | 'nachname' | 'email' | 'status'
type SortDir = 'asc' | 'desc'

interface SortState {
  key: SortKey
  dir: SortDir
}

const COLUMNS: Array<{ key: SortKey; label: string }> = [
  { key: 'vorname', label: 'Vorname' },
  { key: 'nachname', label: 'Nachname' },
  { key: 'email', label: 'E-Mail' },
  { key: 'status', label: 'Status' },
]

interface UserTableProps {
  users: AdminUserSummary[]
  selectedFilter: UserStatusFilter
  onSelectFilter: (filter: UserStatusFilter) => void
  onOpenUser: (user: AdminUserSummary) => void
}

// UserTable is the compact spreadsheet-style user list (Effort 2, UX-DR6/8/10):
// a real <table> with sortable columns (Vorname · Nachname · E-Mail · Status,
// aria-sortable asc/desc/off), status filter chips (default aktiv) driving
// ?status= on ListUsers, and the user-group memberships as inline tags beneath
// the name. One compact row per user — not tall cards. A row click opens the
// user detail.
export function UserTable({ users, selectedFilter, onSelectFilter, onOpenUser }: UserTableProps) {
  const [sort, setSort] = useState<SortState | null>(null)

  function cycleSort(key: SortKey) {
    setSort((prev) => {
      if (prev === null || prev.key !== key) {
        return { key, dir: 'asc' }
      }
      if (prev.dir === 'asc') {
        return { key, dir: 'desc' }
      }
      return null
    })
  }

  function ariaSort(key: SortKey): 'ascending' | 'descending' | 'none' {
    if (sort === null || sort.key !== key) return 'none'
    return sort.dir === 'asc' ? 'ascending' : 'descending'
  }

  const sorted = [...users].sort((a, b) => {
    if (sort === null) return 0
    const av = String(a[sort.key])
    const bv = String(b[sort.key])
    const cmp = av.localeCompare(bv, 'de', { sensitivity: 'base' })
    return sort.dir === 'asc' ? cmp : -cmp
  })

  const emptyText =
    selectedFilter === 'all'
      ? 'Noch keine Benutzer vorhanden.'
      : `Keine ${selectedFilter === 'active' ? 'aktiven' : selectedFilter === 'pending_approval' ? 'ausstehenden' : 'deaktivierten'} Benutzer.`

  return (
    <div className={styles.wrapper}>
      <nav aria-label="Statusfilter" className={styles.chips}>
        {USER_STATUS_FILTERS.map((opt) => {
          const isActive = opt.value === selectedFilter
          return (
            <button
              key={opt.value}
              type="button"
              className={`${styles.chip} ${isActive ? styles.chipActive : ''}`}
              aria-pressed={isActive}
              onClick={() => onSelectFilter(opt.value)}
            >
              {opt.label}
            </button>
          )
        })}
      </nav>

      {sorted.length === 0 ? (
        <p className={styles.empty}>{emptyText}</p>
      ) : (
        <table className={styles.table}>
          <thead>
            <tr>
              {COLUMNS.map((col) => (
                <th key={col.key} scope="col" aria-sort={ariaSort(col.key)}>
                  <button
                    type="button"
                    className={styles.sortButton}
                    onClick={() => cycleSort(col.key)}
                    aria-label={`Nach ${col.label} sortieren${ariaSort(col.key) === 'ascending' ? ' (absteigend)' : ariaSort(col.key) === 'descending' ? ' (aufsteigend)' : ''}`}
                  >
                    {col.label}
                    <span className={styles.sortIndicator} aria-hidden="true">
                      {ariaSort(col.key) === 'ascending' ? '▲' : ariaSort(col.key) === 'descending' ? '▼' : ''}
                    </span>
                  </button>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {sorted.map((user) => (
              <tr
                key={user.id}
                className={styles.row}
                onClick={() => onOpenUser(user)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    onOpenUser(user)
                  }
                }}
                tabIndex={0}
                aria-label={`Benutzer ${user.vorname} ${user.nachname} öffnen`}
              >
                <td className={styles.nameCell}>{user.vorname}</td>
                <td className={styles.nameCell}>
                  {user.nachname}
                  {(user.user_groups ?? []).length > 0 && (
                    <ul className={styles.groupTags} aria-label="Benutzergruppen">
                      {(user.user_groups ?? []).map((group) => (
                        <li key={group} className={styles.groupTag}>
                          {group}
                        </li>
                      ))}
                    </ul>
                  )}
                </td>
                <td className={styles.nameCell}>{user.email}</td>
                <td>
                  <span className={`${styles.statusBadge} ${styles[`status-${user.status}`]}`}>
                    {userStatusLabel(user.status)}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

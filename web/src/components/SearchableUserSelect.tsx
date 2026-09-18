import { useEffect, useMemo, useRef, useState } from 'react'
import type { AdminUserSummary } from '../auth/users.ts'
import styles from './SearchableUserSelect.module.css'

// SearchableUserSelect is an accessible combobox over a user list (DSGVO
// pickers, UX-DR9): typing filters the users by name or email and the arrow
// keys / Enter select from the matching listbox. It replaces a native <select>
// where the option count makes a dropdown unusable (e.g. a large fleet of
// accounts). The label is a visible <label> wired via htmlFor/id; selection is
// shown in the input and cleared by editing or the clear button. German
// microcopy throughout; every interactive target is ≥48px.

interface SearchableUserSelectProps {
  users: AdminUserSummary[]
  value: string
  onChange: (id: string) => void
  label: string
  id?: string
  disabled?: boolean
}

// userLabel renders the option text of one user: "Vorname Nachname (email)",
// falling back to the email when no name is set.
function userLabel(u: AdminUserSummary): string {
  const name = `${u.vorname} ${u.nachname}`.trim()
  return name !== '' ? `${name} (${u.email})` : u.email
}

// matchesUser is the case-insensitive substring filter over the display name
// and the email (the same fields the old select showed).
function matchesUser(u: AdminUserSummary, query: string): boolean {
  const q = query.trim().toLocaleLowerCase('de')
  if (q === '') return true
  const name = `${u.vorname} ${u.nachname}`.trim()
  return `${name} ${u.email}`.toLocaleLowerCase('de').includes(q)
}

export function SearchableUserSelect({
  users,
  value,
  onChange,
  label,
  id,
  disabled,
}: SearchableUserSelectProps) {
  const [query, setQuery] = useState('')
  const [open, setOpen] = useState(false)
  const [activeIndex, setActiveIndex] = useState(0)
  const containerRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const inputId = id ?? 'user-select'
  const listboxId = `${inputId}-listbox`

  const selected = users.find((u) => u.id === value)

  // matches is the filtered list shown in the listbox. While a user is
  // selected the input shows the label and the list is closed; the query only
  // filters once the selection is cleared (editing reopens it).
  const matches = useMemo(
    () => users.filter((u) => (selected ? true : matchesUser(u, query))),
    [users, query, selected],
  )

  function openForQuery(next: string) {
    setQuery(next)
    setOpen(true)
    setActiveIndex(0)
    if (value !== '') onChange('')
  }

  function select(id: string) {
    onChange(id)
    setQuery('')
    setOpen(false)
    inputRef.current?.blur()
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setOpen(true)
      setActiveIndex((i) => Math.min(i + 1, Math.max(matches.length - 1, 0)))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setOpen(true)
      setActiveIndex((i) => Math.max(i - 1, 0))
    } else if (e.key === 'Enter') {
      if (open && matches.length > 0 && matches[activeIndex]) {
        e.preventDefault()
        select(matches[activeIndex].id)
      } else if (!open && selected) {
        e.preventDefault()
        setOpen(true)
      }
    } else if (e.key === 'Escape') {
      setOpen(false)
      setActiveIndex(0)
    } else if (e.key === 'Tab') {
      setOpen(false)
    }
  }

  // Close the listbox when a click lands outside the component.
  useEffect(() => {
    function onDocClick(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', onDocClick)
    return () => document.removeEventListener('mousedown', onDocClick)
  }, [])

  const inputValue = selected ? userLabel(selected) : query

  return (
    <div className={styles.combobox} ref={containerRef}>
      <label className={styles.label} htmlFor={inputId}>
        {label}
      </label>
      <div className={styles.control}>
        <input
          ref={inputRef}
          id={inputId}
          className={styles.input}
          type="text"
          role="combobox"
          aria-expanded={open}
          aria-controls={listboxId}
          aria-autocomplete="list"
          aria-activedescendant={
            open && matches[activeIndex] ? `${inputId}-opt-${matches[activeIndex].id}` : undefined
          }
          autoComplete="off"
          value={inputValue}
          disabled={disabled}
          placeholder="Benutzer suchen…"
          onChange={(e) => openForQuery(e.target.value)}
          onFocus={() => {
            if (selected) return
            setOpen(true)
            setActiveIndex(0)
          }}
          onKeyDown={onKeyDown}
        />
        {selected !== undefined && !disabled && (
          <button
            type="button"
            className={styles.clearButton}
            aria-label="Auswahl aufheben"
            onClick={() => {
              onChange('')
              setQuery('')
              setOpen(false)
              inputRef.current?.focus()
            }}
          >
            ×
          </button>
        )}
      </div>
      {open && (
        <ul id={listboxId} role="listbox" className={styles.listbox} aria-label={label}>
          {matches.length === 0 ? (
            <li className={styles.emptyNote} role="note">
              Keine Benutzer gefunden.
            </li>
          ) : (
            matches.map((u, index) => (
              <li
                key={u.id}
                id={`${inputId}-opt-${u.id}`}
                role="option"
                aria-selected={u.id === value}
                className={index === activeIndex ? `${styles.option} ${styles.optionActive}` : styles.option}
                onMouseDown={(e) => {
                  e.preventDefault()
                  select(u.id)
                }}
                onMouseEnter={() => setActiveIndex(index)}
              >
                {userLabel(u)}
              </li>
            ))
          )}
        </ul>
      )}
    </div>
  )
}
import { FILTER_OPTIONS, STATUS_CODES, STATUS_VOCAB, type StatusCode } from '../types/filters.ts'
import styles from './FilterChips.module.css'

// FilterChips (Story 6.1, FR-16/UX-DR5): the MULTI-SELECT status filter. The
// selected set is keyed on the stable status CODE (`'oos'|'red'|'orange'|'green'`),
// never the German label; the chips RENDER the German labels from the shared
// vocabulary. "Alle" is ACTIVE when the set is EMPTY and clears the selection
// via onClear. Each chip carries aria-pressed so assistive tech reads the
// active state.
interface FilterChipsProps {
  selectedFilters: ReadonlySet<StatusCode>
  onToggleFilter: (code: StatusCode) => void
  onClear: () => void
}

export function FilterChips({ selectedFilters, onToggleFilter, onClear }: FilterChipsProps) {
  const allActive = selectedFilters.size === 0
  return (
    <nav aria-label="Statusfilter" className={styles.container}>
      <button
        type="button"
        className={`${styles.chip} ${allActive ? styles.chipActive : ''}`}
        onClick={onClear}
        aria-pressed={allActive}
      >
        {FILTER_OPTIONS[0]}
      </button>
      {STATUS_CODES.map((code) => {
        const label = STATUS_VOCAB[code].label
        const isActive = selectedFilters.has(code)
        return (
          <button
            key={code}
            type="button"
            className={`${styles.chip} ${isActive ? styles.chipActive : ''}`}
            onClick={() => onToggleFilter(code)}
            aria-pressed={isActive}
          >
            {label}
          </button>
        )
      })}
    </nav>
  )
}

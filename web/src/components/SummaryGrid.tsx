import styles from './SummaryGrid.module.css'
import { STATUS_VOCAB, type StatusCode } from '../types/filters.ts'

// SummaryCounts are the per-status totals of the CURRENT tool list (Story
// 6.1): derived from the server-returned statuses on the dashboard read.
export interface SummaryCounts {
  einsatzbereit?: number
  ausstehend?: number
  ueberfaellig?: number
  ausserBetrieb?: number
}

interface SummaryGridProps {
  counts?: SummaryCounts
  // onToggleFilter (Story 6.1, FR-16/UX-DR5): tapping a NON-ZERO count card
  // toggles the matching status filter in the dashboard, keyed on the stable
  // status CODE. Required — the dashboard always passes it (it must match
  // FilterChips' required toggle contract).
  onToggleFilter: (code: StatusCode) => void
}

export function SummaryGrid({ counts = {}, onToggleFilter }: SummaryGridProps) {
  const {
    einsatzbereit = 0,
    ausstehend = 0,
    ueberfaellig = 0,
    ausserBetrieb = 0,
  } = counts

  const items: { id: string; label: string; code: StatusCode; count: number; cardClass: string }[] = [
    {
      id: 'einsatzbereit',
      label: STATUS_VOCAB.green.label,
      code: 'green',
      count: einsatzbereit,
      cardClass: styles.cardGreen,
    },
    {
      id: 'ausstehend',
      label: STATUS_VOCAB.orange.label,
      code: 'orange',
      count: ausstehend,
      cardClass: styles.cardOrange,
    },
    {
      id: 'ueberfaellig',
      label: STATUS_VOCAB.red.label,
      code: 'red',
      count: ueberfaellig,
      cardClass: styles.cardRed,
    },
    {
      id: 'ausser-betrieb',
      label: STATUS_VOCAB.oos.label,
      code: 'oos',
      count: ausserBetrieb,
      cardClass: styles.cardOos,
    },
  ]

  return (
    <section aria-label="Statusübersicht" className={styles.grid}>
      {items.map((item) => (
        <button
          key={item.id}
          type="button"
          className={`${styles.card} ${item.cardClass}`}
          aria-label={`${item.count} ${item.label}`}
          onClick={() => onToggleFilter(item.code)}
          disabled={item.count === 0}
        >
          <span className={styles.count} aria-hidden="true">{item.count}</span>
          <span className={styles.label} aria-hidden="true">{item.label}</span>
        </button>
      ))}
    </section>
  )
}

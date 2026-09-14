import styles from './SummaryGrid.module.css'
import { OOS_STATUS } from '../types/filters.ts'

export interface SummaryCounts {
  einsatzbereit?: number
  ausstehend?: number
  ueberfaellig?: number
  ausserBetrieb?: number
}

interface SummaryGridProps {
  counts?: SummaryCounts
}

export function SummaryGrid({ counts = {} }: SummaryGridProps) {
  const {
    einsatzbereit = 0,
    ausstehend = 0,
    ueberfaellig = 0,
    ausserBetrieb = 0,
  } = counts

  const items = [
    {
      id: 'einsatzbereit',
      label: 'Einsatzbereit',
      count: einsatzbereit,
      cardClass: styles.cardGreen,
    },
    {
      id: 'ausstehend',
      label: 'Ausstehend',
      count: ausstehend,
      cardClass: styles.cardOrange,
    },
    {
      id: 'ueberfaellig',
      label: 'Überfällig',
      count: ueberfaellig,
      cardClass: styles.cardRed,
    },
    {
      id: 'ausser-betrieb',
      label: OOS_STATUS,
      count: ausserBetrieb,
      cardClass: styles.cardOos,
    },
  ]

  return (
    <section aria-label="Statusübersicht" className={styles.grid}>
      {items.map((item) => (
        <div
          key={item.id}
          className={`${styles.card} ${item.cardClass}`}
          aria-label={`${item.count} ${item.label}`}
        >
          <span className={styles.count} aria-hidden="true">{item.count}</span>
          <span className={styles.label} aria-hidden="true">{item.label}</span>
        </div>
      ))}
    </section>
  )
}

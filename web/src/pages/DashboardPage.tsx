import { useState } from 'react'
import { Header } from '../components/Header.tsx'
import { SummaryGrid } from '../components/SummaryGrid.tsx'
import { FilterChips } from '../components/FilterChips.tsx'
import { EmptyState } from '../components/EmptyState.tsx'
import type { FilterStatus } from '../types/filters.ts'
import styles from './DashboardPage.module.css'

export function DashboardPage() {
  const [selectedFilter, setSelectedFilter] = useState<FilterStatus>('Alle')

  return (
    <div className={styles.page}>
      <Header />
      <main className={styles.main}>
        <section className={styles.titleSection}>
          <h2 className={styles.pageTitle}>Übersicht</h2>
          <p className={styles.pageSubtitle}>
            G.E.A.R. (Geräte-Einsatz-Assistenz &amp; Readiness) — Geräteverwaltung &amp; Einsatzbereitschaft
          </p>
        </section>

        <SummaryGrid />

        <section className={styles.section} aria-label="Werkzeugliste">
          <FilterChips
            selectedFilter={selectedFilter}
            onSelectFilter={setSelectedFilter}
          />
          <EmptyState />
        </section>
      </main>
    </div>
  )
}

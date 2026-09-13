import styles from './PassFailChips.module.css'

// PassFailChips is the inspection result toggle (Story 5.2, UX-DR5/DR9): two
// large ≥48px tappable chips — green OK/BESTANDEN vs red FEHLER/NICHT
// BESTANDEN — built on the AdminWerkzeugePage mode-switch pattern: a
// visually-hidden radio inside a <label>, the selected state on the label, and
// a keyboard focus ring via :has(input:focus-visible). The chips are the UX
// foundation only — Stories 5.4/5.5 wire the selected value to the real
// pass/fail + checklist execution.
export type PassFailValue = 'pass' | 'fail'

interface PassFailChipsProps {
  // name scopes the radio group; pass a value unique per item so Story 5.4/5.5
  // can render one group per checklist item.
  name: string
  legend: string
  selected: PassFailValue | null
  // onSelect receives the newly chosen value, or null when the currently
  // selected chip is tapped again (deselect — a mis-click must not trap the
  // user on a single-screen save).
  onSelect: (value: PassFailValue | null) => void
  // disabled disables the radios — the page disables the chips while the
  // placeholder submit is in flight AND after it, so the recorded result cannot
  // diverge from the confirmed saved state.
  disabled?: boolean
}

// selectedClass is a STRICT literal union (the CSS module type is a loose index
// signature, so `keyof typeof styles` would degrade to `string`). A typo in
// either key below is a TYPE error instead of silently resolving to undefined.
type SelectedClass = 'chipSelectedPass' | 'chipSelectedFail'

const OPTIONS: ReadonlyArray<{ value: PassFailValue; label: string; selectedClass: SelectedClass }> = [
  { value: 'pass', label: 'OK/BESTANDEN', selectedClass: 'chipSelectedPass' },
  { value: 'fail', label: 'FEHLER/NICHT BESTANDEN', selectedClass: 'chipSelectedFail' },
]

export function PassFailChips({ name, legend, selected, onSelect, disabled = false }: PassFailChipsProps) {
  return (
    <fieldset className={styles.chipsFieldset}>
      <legend className={styles.chipsLegend}>{legend}</legend>
      <div className={styles.chipsRow}>
        {OPTIONS.map((option) => {
          const active = selected === option.value
          return (
            <label key={option.value} className={active ? `${styles.chip} ${styles[option.selectedClass]}` : styles.chip}>
              <input
                type="radio"
                name={name}
                value={option.value}
                checked={active}
                disabled={disabled}
                onClick={(event) => {
                  if (disabled) return
                  // Re-tapping the ALREADY-selected chip deselects it (a
                  // mis-click must not trap the user on a single-screen save).
                  // preventDefault stops the default activation from
                  // re-checking the radio (which would re-fire onChange and
                  // re-select), so the deselect is stable.
                  if (active) {
                    event.preventDefault()
                    onSelect(null)
                  }
                }}
                onChange={() => {
                  if (disabled) return
                  onSelect(option.value)
                }}
              />
              <span>{option.label}</span>
            </label>
          )
        })}
      </div>
    </fieldset>
  )
}
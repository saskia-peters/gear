import { useEffect, useRef, useState } from 'react'
import styles from './AttributesEditor.module.css'

// AttributesEditor is the shared key/value editor for the `attributes` JSONB
// extension surface (Story 4.4, FR-10/AD-3): add/edit/remove attribute pairs
// with inline German validation and a live UTF-8 size counter, reused as the
// "Eigene Felder" section in BOTH the ToolsTab and the ToolTypesTab. Values are
// stored as JSON: text that is a valid JSON literal (quoted string, number,
// true/false, null, array, object) becomes that value; any other plain text —
// INCLUDING leading/trailing whitespace — stays a verbatim string (never
// trimmed, never type-converted). Keys are trimmed, non-empty, ≤64 runes and
// unique. The serialized object is capped at 16 KB of UTF-8 BYTES (mirroring
// the server's `len(json.Marshal(...))`, which counts UTF-8 bytes — not UTF-16
// code units, so a client-side cap must use TextEncoder).
//
// The parent owns the effective attribute object and tracks whether the
// section was touched: onChange fires ONLY when the parsed object actually
// changes (so an untouched/empty-add never flips the "known" flag and the
// save body keeps omitting `attributes` — the absent = unchanged contract),
// while onDirty fires on every REAL interaction (a no-op empty-add to an empty
// editor fires neither). onValidityChange reports whether the current set is
// free of inline validation errors so the parent can disable Save while a
// bad key / over-size set is present (errors must never be silently dropped).

type AttrRow = { id: number; key: string; value: string }

type Analysis = { parsed: Record<string, unknown>; errors: string[]; size: number }

const MAX_ATTRIBUTE_KEY_RUNES = 64
const MAX_ATTRIBUTES_SIZE = 16 * 1024

function toRows(attrs: Record<string, unknown>): AttrRow[] {
  return Object.entries(attrs).map(([key, value], index) => ({
    id: index,
    key,
    value: JSON.stringify(value),
  }))
}

// valueFromText converts the raw value input to a stored JSON value. A JSON
// literal parses to its value; anything else (including a plain word with
// surrounding whitespace, or an empty string) is kept VERBATIM as a string —
// never trimmed, never coerced (so a stored string like `" Werkstatt "` or
// `"true"` round-trips unchanged).
function valueFromText(text: string): unknown {
  try {
    return JSON.parse(text)
  } catch {
    return text
  }
}

function analyze(rows: AttrRow[]): Analysis {
  const parsed: Record<string, unknown> = {}
  const errors: string[] = []
  const seen = new Set<string>()
  for (const row of rows) {
    const key = row.key.trim()
    if (key === '' && row.value.trim() === '') continue
    if (key === '') {
      errors.push('Bitte gib einen Schlüssel ein.')
      continue
    }
    if (Array.from(key).length > MAX_ATTRIBUTE_KEY_RUNES) {
      errors.push('Der Schlüssel ist zu lang (maximal 64 Zeichen).')
      continue
    }
    if (seen.has(key)) {
      errors.push(`Doppelter Schlüssel: „${key}“`)
      continue
    }
    seen.add(key)
    parsed[key] = valueFromText(row.value)
  }
  // The server validates UTF-8 BYTE length (len(json.Marshal)), so the client
  // must count UTF-8 bytes too — a UTF-16 code-unit count would let non-ASCII
  // values pass the client check and then 400 server-side.
  const size = new TextEncoder().encode(JSON.stringify(parsed)).length
  if (size > MAX_ATTRIBUTES_SIZE) {
    errors.push('Die Attribute sind zu groß (maximal 16 KB).')
  }
  return { parsed, errors, size }
}

interface AttributesEditorProps {
  attributes: Record<string, unknown>
  onChange: (next: Record<string, unknown>) => void
  onDirty: () => void
  onValidityChange: (valid: boolean) => void
}

export function AttributesEditor({ attributes, onChange, onDirty, onValidityChange }: AttributesEditorProps) {
  const [rows, setRows] = useState<AttrRow[]>(() => toRows(attributes))
  const nextIdRef = useRef(rows.length)
  const lastEmittedRef = useRef<string>(JSON.stringify(attributes))

  const analysis = analyze(rows)

  // Report validity so the parent can disable Save while the set is invalid.
  useEffect(() => {
    onValidityChange(analysis.errors.length === 0)
  }, [analysis.errors.length, onValidityChange])

  function emit(next: AttrRow[]) {
    const nextAnalysis = analyze(next)
    const prevAnalysis = analyze(rows)
    setRows(next)
    // onDirty only fires on a REAL interaction: the parsed object changed, or
    // the editor holds actual content — a no-op empty-add to an empty editor
    // fires neither (the "untouched" contract stays intact).
    const parsedChanged = JSON.stringify(nextAnalysis.parsed) !== JSON.stringify(prevAnalysis.parsed)
    if (parsedChanged || next.some((row) => row.key.trim() !== '' || row.value.trim() !== '')) {
      onDirty()
    }
    // Only propagate a fully VALID object: while any inline validation error
    // is present, the parent keeps the last valid set (so an invalid row can
    // never be silently submitted). When valid, onChange fires only when the
    // object actually changed (an untouched/empty-add never flips the parent's
    // "known" flag — the absent = unchanged contract).
    if (nextAnalysis.errors.length === 0) {
      const serialized = JSON.stringify(nextAnalysis.parsed)
      if (serialized !== lastEmittedRef.current) {
        lastEmittedRef.current = serialized
        onChange(nextAnalysis.parsed)
      }
    }
  }

  function updateRow(id: number, patch: Partial<AttrRow>) {
    emit(rows.map((row) => (row.id === id ? { ...row, ...patch } : row)))
  }

  function removeRow(id: number) {
    emit(rows.filter((row) => row.id !== id))
  }

  function addRow() {
    const next = [...rows, { id: nextIdRef.current++, key: '', value: '' }]
    emit(next)
  }

  return (
    <div className={styles.attributes}>
      <span className={styles.label}>Eigene Felder</span>
      <p className={styles.hint}>
        Zusätzliche Eigenschaften als Schlüssel/Wert-Paare. Werte werden als JSON
        gespeichert — z. B. „Werkstatt&quot;, 1200, [&quot;a&quot;,&quot;b&quot;].
      </p>
      {rows.length === 0 && (
        <p role="status" className={styles.emptyHint}>
          Noch keine eigenen Felder.
        </p>
      )}
      {rows.length > 0 && (
        <div className={styles.rows}>
          {rows.map((row, index) => (
            <div key={row.id} className={styles.attrRow}>
              <input
                className={styles.input}
                aria-label={`Schlüssel ${index + 1}`}
                value={row.key}
                maxLength={MAX_ATTRIBUTE_KEY_RUNES}
                onChange={(e) => updateRow(row.id, { key: e.target.value })}
                autoComplete="off"
              />
              <input
                className={styles.input}
                aria-label={`Wert ${index + 1}`}
                value={row.value}
                onChange={(e) => updateRow(row.id, { value: e.target.value })}
                autoComplete="off"
              />
              <button
                type="button"
                className={styles.iconButton}
                aria-label={`Feld entfernen: ${row.key.trim() || index + 1}`}
                onClick={() => removeRow(row.id)}
              >
                ×
              </button>
            </div>
          ))}
        </div>
      )}
      <div className={styles.addRow}>
        <button type="button" className={styles.addButton} onClick={addRow}>
          Feld hinzufügen
        </button>
      </div>
      {analysis.errors.length > 0 && (
        <ul className={styles.errors} role="alert">
          {analysis.errors.map((err) => (
            <li key={err}>{err}</li>
          ))}
        </ul>
      )}
      <p role="status" className={styles.size}>
        Nutzung: {analysis.size} Bytes / 16 KB
      </p>
    </div>
  )
}
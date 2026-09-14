// OOS_STATUS is the canonical German term for the Out-of-Service state
// (FR-14/AD-4, Story 5.3): the inspection confirmation, the dashboard status
// filter and the summary grid share it so the spelling never drifts.
export const OOS_STATUS = 'Außer Betrieb'

// FilterStatus is the user-facing DISPLAY vocabulary (FR-16/UX-DR5): the
// German labels of the filter chips and the summary cards. Selection identity
// is keyed on the stable status CODE (StatusCode), never on these labels.
export type FilterStatus = 'Alle' | 'Einsatzbereit' | 'Ausstehend' | 'Überfällig' | typeof OOS_STATUS

export const FILTER_OPTIONS: FilterStatus[] = [
  'Alle',
  'Einsatzbereit',
  'Ausstehend',
  'Überfällig',
  OOS_STATUS,
]

// STATUS_CODES are the server-authoritative derived status codes (AD-4/AD-5,
// Story 6.1), mirroring the backend ToolStatusCode verbatim. They are the
// STABLE selection identity for the dashboard filter — German labels may be
// edited, the codes never change.
export const STATUS_CODES = ['oos', 'red', 'orange', 'green'] as const
export type StatusCode = (typeof STATUS_CODES)[number]

// STATUS_VOCAB maps each derived code to the German user-facing label AND a
// stable CSS class key. The dashboard row chips and the summary cards share
// this single vocabulary: German labels stay the user-facing surface while
// the derived codes come from the server (Design Note: never store a status).
export const STATUS_VOCAB: Record<StatusCode, { label: FilterStatus; classKey: string }> = {
  green: { label: 'Einsatzbereit', classKey: 'statusGreen' },
  orange: { label: 'Ausstehend', classKey: 'statusOrange' },
  red: { label: 'Überfällig', classKey: 'statusRed' },
  oos: { label: OOS_STATUS, classKey: 'statusOos' },
}

// statusLabel resolves a derived status code to its German user-facing label
// (FR-16/UX-DR5): `green` → Einsatzbereit, `orange` → Ausstehend, `red` →
// Überfällig, `oos` → Außer Betrieb. DEFENSIVE: an unknown/empty code (e.g. a
// server value that slipped past typing) falls back to the code itself — it
// must never crash the dashboard.
export function statusLabel(code: StatusCode): string {
  return STATUS_VOCAB[code]?.label ?? code
}

// statusClassKey resolves a derived status code to its stable CSS class key
// (statusGreen / statusOrange / statusRed / statusOos), used by the row chip.
// DEFENSIVE: an unknown/empty code falls back to the green (neutral) class so
// the chip still renders legibly.
export function statusClassKey(code: StatusCode): string {
  return STATUS_VOCAB[code]?.classKey ?? STATUS_VOCAB.green.classKey
}

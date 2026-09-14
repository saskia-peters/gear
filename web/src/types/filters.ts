// OOS_STATUS is the canonical German term for the Out-of-Service state
// (FR-14/AD-4, Story 5.3): the inspection confirmation and the dashboard
// status filter share it so the spelling never drifts.
export const OOS_STATUS = 'Außer Betrieb'

export type FilterStatus = 'Alle' | 'Einsatzbereit' | 'Ausstehend' | 'Überfällig' | typeof OOS_STATUS

export const FILTER_OPTIONS: FilterStatus[] = [
  'Alle',
  'Einsatzbereit',
  'Ausstehend',
  'Überfällig',
  OOS_STATUS,
]

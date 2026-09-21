import { useEffect, useState } from 'react'
import { getSystemSettings, updateSystemSetting } from '../../auth/settings.ts'
import type { SystemSetting } from '../../auth/settings.ts'
import { InfoPopup } from '../../components/InfoPopup.tsx'
import styles from './AdminEinstellungenPage.module.css'
import type { Feedback, TabProps } from './settingsTabTypes.ts'
// ---------------------------------------------------------------------------
// System tab (Story 5-2b): the configurable system settings table. Each of the
// 22 atomic settings renders as one row: German name, formatted current value,
// a value-typed editable input (duration → whole seconds, integer → number,
// text → string) and a "?" InfoPopup explaining the setting. Saving is per-row
// with inline German feedback — the server is authoritative, so its 400s (and
// its German message) surface inline. Durations are STORED and EDITED in whole
// seconds; the current-value column shows a friendly German label derived from
// the seconds value.
// ---------------------------------------------------------------------------

interface SystemSettingMeta {
  label: string
  help: string
}

// SYSTEM_SETTING_META is the SPA-side catalog of the 22 seeded settings: German
// label + read-only help text for the "?" popup (UX-DR8). Keys mirror the
// server's seeded keys; the server remains authoritative for the value and the
// value type.
const SYSTEM_SETTING_META: Record<string, SystemSettingMeta> = {
  smtp_dial_timeout: {
    label: 'SMTP-Verbindungsaufbau-Timeout',
    help: 'Zeit in Sekunden, bis ein SMTP-Server den Verbindungsaufbau beantwortet haben muss. Erhöhe den Wert, wenn ein langsames Relay fälschlich als Fehler erscheint.',
  },
  smtp_protocol_timeout: {
    label: 'SMTP-Protokoll-Timeout',
    help: 'Maximale Dauer der gesamten SMTP-Unterhaltung in Sekunden. Das ist der größte Hebel gegen hängende E-Mail-Versuche.',
  },
  backup_dial_timeout: {
    label: 'Backup-Verbindungsaufbau-Timeout',
    help: 'Zeit in Sekunden für den TCP-Verbindungsaufbau zu einem Backup-Ziel. Entfernte Endpunkte variieren stark.',
  },
  backup_protocol_timeout: {
    label: 'Backup-Protokoll-Timeout',
    help: 'Zeit in Sekunden für die FTP-/SFTP-/S3-Unterhaltung mit einem Backup-Ziel während des Verbindungstests.',
  },
  backup_interval: {
    label: 'Backup-Intervall',
    help: 'Abstand in Sekunden zwischen zwei automatischen Backups des Backup-Jobs (Standard 86400 = 1 Tag). Der Job sichert außerdem kurz nach dem Serverstart.',
  },
  password_reset_ttl: {
    label: 'Gültigkeit Passwort-Reset-Link',
    help: 'Gültigkeitsdauer des Links zum Zurücksetzen des Passworts in Sekunden (Standard 30 Minuten). Auch der Zeitraum, in dem die E-Mail noch als „30 Minuten gültig“ angezeigt wird.',
  },
  admin_recovery_ttl: {
    label: 'Gültigkeit Dual-Admin-Wiederherstellung',
    help: 'Gültigkeitsdauer eines Wiederherstellungstokens für die Kontowiederherstellung in Sekunden (Standard 30 Minuten).',
  },
  forgot_throttle_interval: {
    label: 'Sperrintervall „Passwort vergessen“',
    help: 'Mindestabstand in Sekunden zwischen zwei „Passwort vergessen“-Anfragen für dieselbe E-Mail-Adresse (Standard 60).',
  },
  otp_ttl: {
    label: 'Gültigkeit Einmalpasswort (OTP)',
    help: 'Gültigkeitsdauer eines Einmalpassworts in Sekunden (Standard 15 Minuten). Bestimmt das operative Zeitfenster für die Eingabe.',
  },
  otp_length: {
    label: 'OTP-Länge',
    help: 'Anzahl der Zeichen eines Einmalpassworts (Standard 10). Längere Codes erhöhen die Entropie, erschweren aber die Eingabe.',
  },
  mfa_enrollment_window: {
    label: 'Gültigkeit MFA-Anmeldung',
    help: 'Zeitfenster in Sekunden, in dem ein frisch angelegter MFA-Schlüssel bestätigt werden muss (Standard 10 Minuten).',
  },
  lockout_threshold_short: {
    label: 'Sperrschwelle (kurz)',
    help: 'Anzahl der Fehlversuche, ab denen die kurze Sperre greift (Standard 3).',
  },
  lockout_threshold_long: {
    label: 'Sperrschwelle (lang)',
    help: 'Anzahl der Fehlversuche, ab denen die lange Sperre greift (Standard 4).',
  },
  lockout_duration_short: {
    label: 'Sperrdauer (kurz)',
    help: 'Dauer der kurzen Anmeldesperre in Sekunden (Standard 30).',
  },
  lockout_duration_long: {
    label: 'Sperrdauer (lang)',
    help: 'Dauer der langen Anmeldesperre in Sekunden (Standard 60).',
  },
  lockout_max_failed_count: {
    label: 'Max. Fehlversuche gesamt',
    help: 'Obergrenze der erfassten Fehlversuche pro Konto (Standard 10). Schützt vor übermäßigem Sperr-Datenwachstum.',
  },
  attribute_key_max_runes: {
    label: 'Max. Länge Attributschlüssel',
    help: 'Maximale Zeichenzahl eines benutzerdefinierten Attributschlüssels (Standard 64). Gilt einheitlich für Benutzer- und Geräteattribute.',
  },
  attributes_max_size: {
    label: 'Max. Größe Attribute',
    help: 'Maximale Gesamtgröße der Attributsammlung eines Datensatzes in Bytes (Standard 16 KB).',
  },
  inventory_prefix: {
    label: 'Präfix Inventarnummer',
    help: 'Buchstaben-Präfix der automatisch vergebenen Inventarnummern (Standard „GEAR“).',
  },
  inventory_width: {
    label: 'Breite Inventarnummer',
    help: 'Breite des numerischen Teils der Inventarnummer (Standard 9). Zusammen mit dem Präfix ergibt sich z. B. „GEAR000000001“.',
  },
  inspection_orange_window_percent: {
    label: 'Orange-Fenster Prüfung (Prozent des Prüfintervalls)',
    help: 'Prozent des Prüfintervalls, in dem ein Gerät vor der Fälligkeit auf dem Dashboard orange dargestellt wird. 25 = ein Viertel des Prüfintervalls (Standard).',
  },
  qualification_expiring_soon_window: {
    label: 'Qualifikation „bald ablaufend“',
    help: 'Zeitfenster in Sekunden vor dem Ablauf, ab dem eine Qualifikation als „bald ablaufend“ gilt (Standard 30 Tage).',
  },
}

// SYSTEM_SETTING_GROUPS is the display grouping of the System settings (user
// decision 2026-09-18): the 22 atomic settings are rendered under 8 German
// category headings instead of one flat table. Keys within a group keep the
// server's seed order (groups list them in that order). Every seeded key must
// appear in EXACTLY one group; the render falls back to an "Weitere" group for
// any future key not listed here (server-authoritative value/type unchanged).
interface SystemSettingGroup {
  label: string
  keys: readonly string[]
}

const SYSTEM_SETTING_GROUPS: readonly SystemSettingGroup[] = [
  {
    label: 'E-Mail-Versand',
    keys: ['smtp_dial_timeout', 'smtp_protocol_timeout'],
  },
  {
    label: 'Backup',
    keys: ['backup_dial_timeout', 'backup_protocol_timeout', 'backup_interval'],
  },
  {
    label: 'Passwort & Kontowiederherstellung',
    keys: ['password_reset_ttl', 'admin_recovery_ttl', 'forgot_throttle_interval'],
  },
  {
    label: 'Zwei-Faktor-Authentifizierung (MFA)',
    keys: ['otp_ttl', 'otp_length', 'mfa_enrollment_window'],
  },
  {
    label: 'Anmeldesperre',
    keys: [
      'lockout_threshold_short',
      'lockout_threshold_long',
      'lockout_duration_short',
      'lockout_duration_long',
      'lockout_max_failed_count',
    ],
  },
  {
    label: 'Attribute',
    keys: ['attribute_key_max_runes', 'attributes_max_size'],
  },
  {
    label: 'Inventarnummern',
    keys: ['inventory_prefix', 'inventory_width'],
  },
  {
    label: 'Prüfung & Qualifikation',
    keys: ['inspection_orange_window_percent', 'qualification_expiring_soon_window'],
  },
]

// settingsByGroup partitions the loaded settings into the SYSTEM_SETTING_GROUPS
// buckets (a map keyed by the group's label), keeping the server seed order
// within each group. A setting whose key is not in any group lands in the
// "Weitere Einstellungen" bucket so an unknown/drifted key is still editable,
// never hidden (matches the server-authoritative contract).
function settingsByGroup(settings: SystemSetting[]): Array<{ label: string; items: SystemSetting[] }> {
  const byLabel = new Map<string, SystemSetting[]>()
  for (const group of SYSTEM_SETTING_GROUPS) {
    byLabel.set(group.label, [])
  }
  const rest: SystemSetting[] = []
  const grouped = new Set<string>()
  for (const group of SYSTEM_SETTING_GROUPS) {
    for (const key of group.keys) {
      const setting = settings.find((s) => s.key === key)
      if (setting) {
        byLabel.get(group.label)!.push(setting)
        grouped.add(key)
      }
    }
  }
  for (const setting of settings) {
    if (!grouped.has(setting.key)) rest.push(setting)
  }
  const out: Array<{ label: string; items: SystemSetting[] }> = []
  for (const group of SYSTEM_SETTING_GROUPS) {
    const items = byLabel.get(group.label)!
    if (items.length > 0) out.push({ label: group.label, items })
  }
  if (rest.length > 0) out.push({ label: 'Weitere Einstellungen', items: rest })
  return out
}

// formatDuration renders a whole-second duration as a friendly German label
// (e.g. "30 Minuten", "15 Minuten", "30 Tage", "10 Sekunden").
function formatDuration(totalSeconds: number): string {
  const minute = 60
  const hour = 60 * minute
  const day = 24 * hour
  if (totalSeconds % day === 0 && totalSeconds >= day) {
    const d = totalSeconds / day
    return `${d} ${d === 1 ? 'Tag' : 'Tage'}`
  }
  if (totalSeconds % hour === 0 && totalSeconds >= hour) {
    const h = totalSeconds / hour
    return `${h} ${h === 1 ? 'Stunde' : 'Stunden'}`
  }
  if (totalSeconds % minute === 0 && totalSeconds >= minute) {
    const m = totalSeconds / minute
    return `${m} ${m === 1 ? 'Minute' : 'Minuten'}`
  }
  return `${totalSeconds} ${totalSeconds === 1 ? 'Sekunde' : 'Sekunden'}`
}

// formatCurrentValue renders the current-value cell per value_type: durations
// as a friendly German label, integers/text with the setting's unit appended
// (e.g. "10 Zeichen", "14 Tage", "16384 Bytes") so days and seconds never look
// alike. unit is the server-authoritative display unit.
function formatCurrentValue(setting: SystemSetting): string {
  if (setting.value_type === 'duration') {
    return formatDuration(Number(setting.value))
  }
  const unit = setting.unit ?? ''
  return `${String(setting.value)}${unit !== '' ? ` ${unit}` : ''}`
}

// SystemSettingsTab is the System surface (Story 5-2b): a table of the 22
// seeded settings — German name, formatted current value, a value-typed input
// (duration in whole seconds, integer, text) and a "?" InfoPopup per row.
// Saving is per-row with inline German feedback; the server is authoritative
// (its 400s surface inline). Skeleton loading, 403 → leave the admin module,
// 401 → login.
export function SystemSettingsTab({ onApiError }: TabProps) {
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [settings, setSettings] = useState<SystemSetting[]>([])
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [feedback, setFeedback] = useState<Record<string, Feedback>>({})
  const [busyKeys, setBusyKeys] = useState<ReadonlySet<string>>(new Set())

  useEffect(() => {
    let cancelled = false
    async function run() {
      try {
        const rows = await getSystemSettings()
        if (cancelled) return
        setSettings(rows)
        const next: Record<string, string> = {}
        for (const row of rows) {
          next[row.key] = String(row.value)
        }
        setDrafts(next)
      } catch (err) {
        if (cancelled) return
        if (!onApiError(err)) {
          setLoadError('Die System-Einstellungen konnten nicht geladen werden.')
        }
      } finally {
        if (!cancelled) setLoaded(true)
      }
    }
    void run()
    return () => {
      cancelled = true
    }
  }, [onApiError])

  function setRowFeedback(key: string, fb: Feedback) {
    setFeedback((prev) => {
      const next = { ...prev }
      if (fb) {
        next[key] = fb
      } else {
        delete next[key]
      }
      return next
    })
  }

  async function saveRow(setting: SystemSetting) {
    const raw = drafts[setting.key] ?? String(setting.value)
    // An emptied numeric field must NOT become Number('') = 0 and silently
    // persist 0 — block the empty raw input client-side with inline feedback
    // (the server is never reached).
    if (setting.value_type !== 'text' && raw.trim() === '') {
      setRowFeedback(setting.key, { kind: 'error', message: 'Bitte gib einen Wert für diese Einstellung ein.' })
      return
    }
    const numeric = Number(raw)
    // A non-finite number (e.g. 1e309) would serialize as JSON null and hit
    // the server as a type mismatch — reject it inline instead.
    if (setting.value_type !== 'text' && !Number.isFinite(numeric)) {
      setRowFeedback(setting.key, { kind: 'error', message: 'Ungültiger Wert.' })
      return
    }
    const value: number | string = setting.value_type === 'text' ? raw : numeric
    setBusyKeys((prev) => new Set(prev).add(setting.key))
    setRowFeedback(setting.key, null)
    try {
      const saved = await updateSystemSetting(setting.key, value)
      setSettings((prev) => prev.map((s) => (s.key === setting.key ? saved : s)))
      setDrafts((prev) => ({ ...prev, [setting.key]: String(saved.value) }))
      setRowFeedback(setting.key, { kind: 'success', message: saved.message })
    } catch (err) {
      if (onApiError(err)) return
      setRowFeedback(setting.key, {
        kind: 'error',
        message: err instanceof Error ? err.message : 'Die System-Einstellung konnte nicht gespeichert werden.',
      })
    } finally {
      setBusyKeys((prev) => {
        const next = new Set(prev)
        next.delete(setting.key)
        return next
      })
    }
  }

  return (
    <>
      {loadError && (
        <p role="alert" className={styles.feedbackError}>
          {loadError}
        </p>
      )}

      {!loaded ? (
        <div className={styles.skeleton} aria-busy="true" aria-label="System-Einstellungen werden geladen">
          <div className={styles.skeletonRow} aria-hidden="true" />
          <div className={styles.skeletonRow} aria-hidden="true" />
        </div>
      ) : settings.length === 0 ? (
        <p role="status" className={styles.warning}>
          Keine System-Einstellungen vorhanden.
        </p>
      ) : (
        <>
          {settingsByGroup(settings).map((group) => (
            <section key={group.label} className={styles.systemGroup} aria-label={group.label}>
              <h4 className={styles.systemGroupTitle}>{group.label}</h4>
              <table className={styles.systemTable} aria-label={group.label}>
                <thead>
                  <tr>
                    <th scope="col" className={styles.systemTh}>
                      Einstellung
                    </th>
                    <th scope="col" className={styles.systemTh}>
                      Aktueller Wert
                    </th>
                    <th scope="col" className={styles.systemTh}>
                      Neuer Wert
                    </th>
                    <th scope="col" className={styles.systemTh}>
                      <span className={styles.visuallyHidden}>Info</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {group.items.map((setting) => {
                    const meta = SYSTEM_SETTING_META[setting.key] ?? { label: setting.key, help: '' }
                    const busy = busyKeys.has(setting.key)
                    const rowFeedback = feedback[setting.key]
                    return (
                      <tr key={setting.key} className={styles.systemRow}>
                        <td className={styles.systemCell}>
                          <span className={styles.systemName}>{meta.label}</span>
                          <span className={styles.systemKey}>{setting.key}</span>
                        </td>
                        <td className={styles.systemCell}>
                          <span className={styles.systemValue}>{formatCurrentValue(setting)}</span>
                        </td>
                        <td className={styles.systemCell}>
                          <div className={styles.systemEdit}>
                            <label className={styles.visuallyHidden} htmlFor={`setting-${setting.key}`}>
                              {meta.label} bearbeiten
                            </label>
                            {setting.value_type === 'text' ? (
                              <input
                                id={`setting-${setting.key}`}
                                className={styles.input}
                                value={drafts[setting.key] ?? String(setting.value)}
                                disabled={busy}
                                onChange={(e) => {
                                  setDrafts((prev) => ({ ...prev, [setting.key]: e.target.value }))
                                  setRowFeedback(setting.key, null)
                                }}
                                maxLength={64}
                                autoComplete="off"
                              />
                            ) : (
                              <input
                                id={`setting-${setting.key}`}
                                className={styles.input}
                                type="number"
                                min={0}
                                step={1}
                                value={drafts[setting.key] ?? String(setting.value)}
                                disabled={busy}
                                onChange={(e) => {
                                  setDrafts((prev) => ({ ...prev, [setting.key]: e.target.value }))
                                  setRowFeedback(setting.key, null)
                                }}
                              />
                            )}
                            {setting.value_type === 'duration' && (
                              <span className={styles.hint}>in Sekunden</span>
                            )}
                            {rowFeedback && (
                              <p
                                role={rowFeedback.kind === 'error' ? 'alert' : 'status'}
                                className={rowFeedback.kind === 'error' ? styles.systemFeedbackError : styles.systemFeedbackSuccess}
                              >
                                {rowFeedback.message}
                              </p>
                            )}
                          </div>
                        </td>
                        <td className={styles.systemCell}>
                          <div className={styles.systemActions}>
                            <InfoPopup title={meta.label} description={meta.help} />
                            <button
                              type="button"
                              className={styles.rowButton}
                              disabled={busy}
                              onClick={() => void saveRow(setting)}
                            >
                              {busy ? 'Speichert...' : 'Speichern'}
                            </button>
                          </div>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </section>
          ))}
        </>
      )}
    </>
  )
}
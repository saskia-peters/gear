// @vitest-environment jsdom
import { render, screen, cleanup, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { AttributesEditor } from './AttributesEditor.tsx'

function renderEditor(attributes: Record<string, unknown> = {}, onChange = vi.fn()) {
  const onDirty = vi.fn()
  const onValidityChange = vi.fn()
  render(
    <AttributesEditor
      attributes={attributes}
      onChange={onChange}
      onDirty={onDirty}
      onValidityChange={onValidityChange}
    />,
  )
  return { onChange, onDirty, onValidityChange }
}

describe('AttributesEditor', () => {
  afterEach(() => {
    cleanup()
  })

  it('EMPTY: shows the empty state and the UTF-8 byte size counter', () => {
    renderEditor()
    expect(screen.getByText('Eigene Felder')).toBeInTheDocument()
    expect(screen.getByText('Noch keine eigenen Felder.')).toBeInTheDocument()
    expect(screen.getByText('Nutzung: 2 Bytes / 16 KB')).toBeInTheDocument()
  })

  it('LOAD: renders stored attributes as key/value rows (values as JSON)', () => {
    renderEditor({ standort: 'Werkstatt', leistung: 1200 })
    expect(screen.getByLabelText('Schlüssel 1')).toHaveValue('standort')
    expect(screen.getByLabelText('Wert 1')).toHaveValue('"Werkstatt"')
    expect(screen.getByLabelText('Schlüssel 2')).toHaveValue('leistung')
    expect(screen.getByLabelText('Wert 2')).toHaveValue('1200')
  })

  it('ADD_EDIT: adding a pair and filling it reports the parsed object', async () => {
    const { onChange, onDirty } = renderEditor()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Feld hinzufügen' }))
    await user.type(screen.getByLabelText('Schlüssel 1'), 'standort')
    // Plain text is stored as a string; JSON values (numbers) parse.
    await user.type(screen.getByLabelText('Wert 1'), 'Werkstatt')
    await user.click(screen.getByRole('button', { name: 'Feld hinzufügen' }))
    await user.type(screen.getByLabelText('Schlüssel 2'), 'leistung')
    await user.type(screen.getByLabelText('Wert 2'), '1200')

    expect(onChange).toHaveBeenCalledWith({ standort: 'Werkstatt', leistung: 1200 })
    expect(onDirty).toHaveBeenCalled()
  })

  it('REMOVE: removing a pair drops it from the reported object', async () => {
    const { onChange } = renderEditor({ standort: 'Werkstatt', leistung: 1200 })
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Feld entfernen: standort' }))
    expect(onChange).toHaveBeenCalledWith({ leistung: 1200 })
  })

  it('CLEAR: removing every pair reports an explicit empty object', async () => {
    const { onChange } = renderEditor({ standort: 'Werkstatt' })
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Feld entfernen: standort' }))
    expect(onChange).toHaveBeenCalledWith({})
  })

  it('KEY_ERROR: an empty key and a duplicate key show inline German validation', async () => {
    const { onChange } = renderEditor({ standort: 'Werkstatt' })
    const user = userEvent.setup()

    // Duplicate key (case-sensitively the same trimmed key).
    await user.click(screen.getByRole('button', { name: 'Feld hinzufügen' }))
    await user.type(screen.getByLabelText('Schlüssel 2'), 'standort')
    await user.type(screen.getByLabelText('Wert 2'), 'Lager')

    expect(screen.getByRole('alert')).toHaveTextContent('Doppelter Schlüssel: „standort“')
    // The invalid duplicate row is NEVER reported: no onChange call carries the
    // value typed under the duplicate key.
    const reported = onChange.mock.calls.map((call) => call[0] as Record<string, unknown>)
    expect(reported.every((obj) => !Object.values(obj).includes('Lager'))).toBe(true)

    // Clearing the duplicate key exposes the empty-key error.
    await user.clear(screen.getByLabelText('Schlüssel 2'))
    expect(screen.getByRole('alert')).toHaveTextContent('Bitte gib einen Schlüssel ein.')
  })

  it('KEY_CAP: the key input caps at 64 runes (matching the server bound)', async () => {
    // maxLength mirrors MaxAttributeKeyRunes (64): a 65th char can never be
    // typed, so the over-long error is only reachable programmatically.
    const user = userEvent.setup()
    renderEditor()
    await user.click(screen.getByRole('button', { name: 'Feld hinzufügen' }))
    await user.type(screen.getByLabelText('Schlüssel 1'), 'k'.repeat(70))
    expect(screen.getByLabelText('Schlüssel 1')).toHaveValue('k'.repeat(64))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('KEY_TOO_LONG_ERROR: an over-limit key (set programmatically) shows the inline error and reports invalid', async () => {
    const { onChange, onValidityChange } = renderEditor()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Feld hinzufügen' }))
    // fireEvent.change bypasses maxLength — the server-side bound must still be
    // surfaced inline and reported invalid (Save disabled in the parent).
    fireEvent.change(screen.getByLabelText('Schlüssel 1'), { target: { value: 'k'.repeat(65) } })
    fireEvent.change(screen.getByLabelText('Wert 1'), { target: { value: 'x' } })

    expect(screen.getByRole('alert')).toHaveTextContent('Der Schlüssel ist zu lang (maximal 64 Zeichen).')
    expect(onValidityChange).toHaveBeenLastCalledWith(false)
    // The invalid over-long row is NEVER reported.
    const reported = onChange.mock.calls.map((call) => call[0] as Record<string, unknown>)
    expect(reported.every((obj) => !('k'.repeat(65) in obj))).toBe(true)
  })

  it('SIZE: an over-16KB object shows the size error and reports invalid', async () => {
    const big = 'x'.repeat(17000)
    const { onChange, onValidityChange } = renderEditor()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Feld hinzufügen' }))
    await user.type(screen.getByLabelText('Schlüssel 1'), 'note')
    // fireEvent.change sets the whole value in one step (a multi-KB paste).
    fireEvent.change(screen.getByLabelText('Wert 1'), { target: { value: `"${big}"` } })

    expect(screen.getByRole('alert')).toHaveTextContent('Die Attribute sind zu groß (maximal 16 KB).')
    expect(onValidityChange).toHaveBeenLastCalledWith(false)
    // The over-size object is NEVER reported.
    const reported = onChange.mock.calls.map((call) => call[0] as Record<string, unknown>)
    expect(reported.every((obj) => !Object.values(obj).includes(big))).toBe(true)
  })

  it('SIZE_UTF8_BYTES: a set under the UTF-16 limit but over the UTF-8 byte limit is rejected', async () => {
    // 9000 'ä' = 9000 UTF-16 code units (~9 KB by the OLD code-unit cap) but
    // ~18 KB of UTF-8 bytes (each 'ä' is 2 bytes) — the server counts UTF-8
    // bytes, so the client must reject it too.
    const umlauts = 'ä'.repeat(9000)
    const { onValidityChange } = renderEditor()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Feld hinzufügen' }))
    fireEvent.change(screen.getByLabelText('Schlüssel 1'), { target: { value: 'note' } })
    fireEvent.change(screen.getByLabelText('Wert 1'), { target: { value: `"${umlauts}"` } })

    expect(screen.getByRole('alert')).toHaveTextContent('Die Attribute sind zu groß (maximal 16 KB).')
    expect(onValidityChange).toHaveBeenLastCalledWith(false)
  })

  it('VALIDITY: reports valid=true on a clean set and valid=false on a bad key', async () => {
    const { onValidityChange } = renderEditor({ standort: 'Werkstatt' })
    // The initial clean set reports valid.
    expect(onValidityChange).toHaveBeenCalledWith(true)

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Feld hinzufügen' }))
    // A value with an EMPTY key is invalid.
    await user.type(screen.getByLabelText('Wert 2'), 'x')
    expect(screen.getByRole('alert')).toHaveTextContent('Bitte gib einen Schlüssel ein.')
    expect(onValidityChange).toHaveBeenLastCalledWith(false)

    // Fixing the key restores validity.
    await user.type(screen.getByLabelText('Schlüssel 2'), 'standort2')
    expect(onValidityChange).toHaveBeenLastCalledWith(true)
  })

  it('ROUND_TRIP: stored string values with whitespace / ambiguous literals are displayed verbatim', () => {
    renderEditor({
      raum: ' Werkstatt ',
      flag: 'true',
      num: '1200',
      leer: 'null',
    })
    // String values are displayed as JSON string literals — including the
    // internal whitespace — so a re-edit parses them back to the SAME strings
    // (never trimmed, never type-converted to boolean/number/null).
    expect(screen.getByLabelText('Wert 1')).toHaveValue('" Werkstatt "')
    expect(screen.getByLabelText('Wert 2')).toHaveValue('"true"')
    expect(screen.getByLabelText('Wert 3')).toHaveValue('"1200"')
    expect(screen.getByLabelText('Wert 4')).toHaveValue('"null"')
  })

  it('REEDIT: a quoted ambiguous string re-edits as a STRING (no type conversion)', () => {
    const { onChange } = renderEditor({ num: '1200' })
    // Re-type the displayed JSON literal to a different value.
    fireEvent.change(screen.getByLabelText('Wert 1'), { target: { value: '"1201"' } })
    // `"1201"` (quoted) parses to the STRING '1201', never the number 1201.
    expect(onChange).toHaveBeenLastCalledWith({ num: '1201' })
  })

  it('PLAIN_TEXT_VERBATIM: unquoted plain text (incl. whitespace) stays a string untouched', async () => {
    const { onChange } = renderEditor()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Feld hinzufügen' }))
    await user.type(screen.getByLabelText('Schlüssel 1'), 'raum')
    // Leading/trailing whitespace around unquoted text must be PRESERVED.
    await user.type(screen.getByLabelText('Wert 1'), ' Werkstatt ')
    expect(onChange).toHaveBeenLastCalledWith({ raum: ' Werkstatt ' })
  })

  it('EMPTY_ADD_NO_DIRTY: an empty add fires neither onChange nor onDirty', async () => {
    // Story 4.4 + review: adding an EMPTY row to an empty editor is a no-op —
    // the "untouched" contract stays intact (no known-flag flip, no feedback
    // cleared).
    const { onChange, onDirty } = renderEditor()
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Feld hinzufügen' }))
    expect(onChange).not.toHaveBeenCalled()
    expect(onDirty).not.toHaveBeenCalled()
  })
})
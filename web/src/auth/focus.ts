import { flushSync } from 'react-dom'

// focusFirstInvalid moves keyboard/AT focus to the first field that carries the
// invalid marker within the given root, so a submission with validation errors
// lands the user directly on the first failing field (UX-DR9, SCREEN_READER:
// "focus moves to first error"). It is a no-op when no invalid field exists.
//
// The aria-invalid markers are applied by a batched state update inside the
// submit handler, so the DOM has not committed them yet when this runs — flush
// pending updates first, then query.
export function focusFirstInvalid(root: HTMLElement | null): void {
  if (!root) {
    return
  }
  flushSync(() => {})
  const firstInvalid = root.querySelector<HTMLElement>('[aria-invalid="true"]')
  firstInvalid?.focus()
}
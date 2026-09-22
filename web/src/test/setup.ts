import '@testing-library/jest-dom/vitest'

declare global {
  // Vitest's jsdom environment exposes `global`; declare it so `global.fetch`
  // stubs in component tests typecheck under `tsc -b`.
  var global: typeof globalThis
}

export {}

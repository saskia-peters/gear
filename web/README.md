# web — G.E.A.R. SPA

React + Vite + TypeScript frontend (React 19, Vite 8, TypeScript 6). Renders a
minimal shell for now; the dashboard surface arrives with Story 1.2. The SPA
contains **no business logic** — server-side authorization is the only source
of truth (AD-6); frontend visibility is supplementary.

- `just dev` serves it at http://localhost:5173 via the Vite dev server.
- `npm run build` / `npm run typecheck` / `npm run preview` for production
  and type validation.

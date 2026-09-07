import { getPermissions } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import { AdminPlaceholder } from './AdminPlaceholder.tsx'

// Werkzeuge placeholder (Story 2.3). This surface is reachable by
// `schirrmeister`, `fuehrende`, and `admin` (user decision); stories 2.4+ fill
// it with the real catalogue. Route gating happens in the route table.
export function AdminWerkzeugePage() {
  return (
    <AdminPlaceholder
      title="Werkzeuge"
      description="Geräte und Gerätetypen verwalten."
      entries={filteredAdminNav(getPermissions())}
    />
  )
}

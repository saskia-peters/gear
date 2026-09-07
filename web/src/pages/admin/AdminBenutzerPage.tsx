import { getPermissions } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import { AdminPlaceholder } from './AdminPlaceholder.tsx'

// Benutzer placeholder (Story 2.3): the admin nav entry for user management.
// Stories 2.4+ fill this surface; route gating happens in the route table.
export function AdminBenutzerPage() {
  return (
    <AdminPlaceholder
      title="Benutzer"
      description="Mitglieder verwalten und neue Anträge freigeben."
      entries={filteredAdminNav(getPermissions())}
    />
  )
}

import { getPermissions } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import { AdminPlaceholder } from './AdminPlaceholder.tsx'

// Rollen placeholder (Story 2.3). Stories 2.4+ fill this surface; route gating
// happens in the route table.
export function AdminRollenPage() {
  return (
    <AdminPlaceholder
      title="Rollen"
      description="Rollen ansehen und anpassen."
      entries={filteredAdminNav(getPermissions())}
    />
  )
}

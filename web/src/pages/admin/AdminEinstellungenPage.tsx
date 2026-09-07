import { getPermissions } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import { AdminPlaceholder } from './AdminPlaceholder.tsx'

// Einstellungen placeholder (Story 2.3). Stories 2.4+ fill this surface; route
// gating happens in the route table.
export function AdminEinstellungenPage() {
  return (
    <AdminPlaceholder
      title="Einstellungen"
      description="E-Mail- und Sicherungs-Einstellungen."
      entries={filteredAdminNav(getPermissions())}
    />
  )
}

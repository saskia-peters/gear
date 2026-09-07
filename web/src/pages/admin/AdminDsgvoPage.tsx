import { getPermissions } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import { AdminPlaceholder } from './AdminPlaceholder.tsx'

// DSGVO placeholder (Story 2.3). Stories 2.4+ fill this surface; route gating
// happens in the route table.
export function AdminDsgvoPage() {
  return (
    <AdminPlaceholder
      title="DSGVO"
      description="Datenauskünfte und Löschungen nach Datenschutz."
      entries={filteredAdminNav(getPermissions())}
    />
  )
}

import { getPermissions } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import { AdminPlaceholder } from './AdminPlaceholder.tsx'

// Qualifikationen placeholder (Story 2.3). Stories 2.4+ fill this surface; route
// gating happens in the route table.
export function AdminQualifikationenPage() {
  return (
    <AdminPlaceholder
      title="Qualifikationen"
      description="Qualifikationen pflegen, z. B. Zertifikate und Lizenzen."
      entries={filteredAdminNav(getPermissions())}
    />
  )
}

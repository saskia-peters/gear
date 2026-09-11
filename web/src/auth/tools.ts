// Tool Maintenance data module (Story 4.2, FR-8/FR-10/FR-23/AD-10): the API
// types and calls for the Werkzeuge → Typen surface — list/create/update/
// archive tool types. The server is the source of truth for the German
// microcopy. The type editor submits the WHOLE ordered checklist-item list;
// update REPLACES the stored items fully (the server never merges). Archive is
// SOFT (server-side archived_at) — the client never hard-deletes.

import { ApiError, request, authTokenHeaders } from './http.ts'

// Permission code gating the tool-type surface (AD-6, server-side source of
// truth). Kept here so the per-tab gating cannot drift from the server code.
export const TOOL_TYPES_PERMISSION = 'tool_types.manage'

export type InspectionMode = 'pass_fail' | 'checklist'

// ToolTypeChecklistItem is one ordered checklist entry of the GET payload.
export interface ToolTypeChecklistItem {
  id: string
  position: number
  label: string
}

// ToolType is the GET payload — the typed core fields plus the ordered
// checklist items. The attributes jsonb extension surface is NOT exposed in V1
// (Story 4.4 owns it); archived types never reach the active list.
export interface ToolType {
  id: string
  name: string
  default_schedule_id: string
  required_qualification_id: string
  inspection_mode: InspectionMode
  checklist_items: ToolTypeChecklistItem[]
  created_at: string
  updated_at: string
}

// ToolTypeWriteResult is the POST/PUT/archive payload: the type plus the
// server-authoritative German confirmation.
export interface ToolTypeWriteResult extends ToolType {
  message: string
}

// ToolTypeChecklistItemInput is one ordered checklist entry of the POST/PUT
// body. Only the label travels; position is implied by the array order.
export interface ToolTypeChecklistItemInput {
  label: string
}

// ToolTypeInput is the POST/PUT body (FR-8/FR-10). items is the WHOLE ordered
// checklist-item list — full replacement on update (the editor always submits
// the complete list).
export interface ToolTypeInput {
  name: string
  default_schedule_id: string
  required_qualification_id: string
  inspection_mode: InspectionMode
  items: ToolTypeChecklistItemInput[]
}

const TOOL_TYPES_URL = '/api/v1/admin/tool-types'

// listToolTypes fetches the ACTIVE tool-type catalog, oldest first, each with
// its ordered checklist items (GET_LIST_EMPTY when none exist — the server
// answers an empty array; archived rows never appear).
export async function listToolTypes(): Promise<ToolType[]> {
  const data = (await request(TOOL_TYPES_URL, { headers: authTokenHeaders() })) as ToolType[] | null
  return Array.isArray(data) ? data : []
}

// createToolType persists a new tool type with its ordered checklist items.
export async function createToolType(input: ToolTypeInput): Promise<ToolTypeWriteResult> {
  return (await request(TOOL_TYPES_URL, {
    method: 'POST',
    headers: authTokenHeaders(),
    body: JSON.stringify(buildToolTypeBody(input)),
  })) as ToolTypeWriteResult
}

// updateToolType persists a type and REPLACES its checklist items fully
// (UPDATE_REPLACE_ITEMS). Updating an archived row answers the 404 sentinel
// server-side.
export async function updateToolType(id: string, input: ToolTypeInput): Promise<ToolTypeWriteResult> {
  return (await request(`${TOOL_TYPES_URL}/${id}`, {
    method: 'PUT',
    headers: authTokenHeaders(),
    body: JSON.stringify(buildToolTypeBody(input)),
  })) as ToolTypeWriteResult
}

// archiveToolType SOFT-archives one type (the row leaves the active list;
// history is preserved server-side).
export async function archiveToolType(id: string): Promise<ToolTypeWriteResult> {
  return (await request(`${TOOL_TYPES_URL}/${id}/archive`, {
    method: 'POST',
    headers: authTokenHeaders(),
  })) as ToolTypeWriteResult
}

function buildToolTypeBody(input: ToolTypeInput): Record<string, unknown> {
  return {
    name: input.name,
    default_schedule_id: input.default_schedule_id,
    required_qualification_id: input.required_qualification_id,
    inspection_mode: input.inspection_mode,
    items: input.items.map((item) => ({ label: item.label })),
  }
}

export { ApiError }
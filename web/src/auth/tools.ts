// Tool Maintenance data module (Story 4.2 + 4.3, FR-8/FR-9/FR-10/FR-23/AD-10):
// the API types and calls for the Werkzeuge → Typen surface
// (list/create/update/archive tool types) AND the Werkzeuge → Werkzeuge surface
// (list/create/update/archive the physical tools that belong to a type, with an
// OPTIONAL per-tool schedule override). The server is the source of truth for
// the German microcopy. The type editor submits the WHOLE ordered checklist-item
// list; update REPLACES the stored items fully (the server never merges). The
// tool editor submits an empty schedule_id to CLEAR an override (the tool then
// inherits its type's default schedule, AD-5). Archive is SOFT (server-side
// archived_at) — the client never hard-deletes.

import { ApiError, request, authTokenHeaders } from './http.ts'

// Permission codes gating the two Tool surfaces (AD-6, server-side source of
// truth). Kept here so the per-tab gating cannot drift from the server code.
export const TOOL_TYPES_PERMISSION = 'tool_types.manage'
export const TOOLS_PERMISSION = 'tools.manage'

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

// ============================================================================
// Tool surface (Story 4.3, FR-9/FR-10/AD-5): the physical tools belonging to a
// type. An EMPTY schedule_id means "inherit the tool type's default schedule";
// a present one is the per-tool override (first-class FK, AD-10).
// ============================================================================

// Tool is the GET payload — the typed core fields plus the tool type's display
// name (server JOIN) and the attributes jsonb passthrough. Archived tools never
// reach the active list.
export interface Tool {
  id: string
  name: string
  tool_type_id: string
  tool_type_name: string
  schedule_id: string
  attributes: Record<string, unknown>
  created_at: string
  updated_at: string
}

// ToolWriteResult is the POST/PUT/archive payload: the tool plus the
// server-authoritative German confirmation.
export interface ToolWriteResult extends Tool {
  message: string
}

// ToolInput is the POST/PUT body (FR-9/FR-10). schedule_id is the OPTIONAL
// per-tool override: an empty value CLEARS it (the tool inherits its type's
// default schedule, AD-5).
export interface ToolInput {
  name: string
  tool_type_id: string
  schedule_id: string
  attributes: Record<string, unknown>
}

const TOOLS_URL = '/api/v1/admin/tools'

// listTools fetches the ACTIVE tool catalog, oldest first, each with its type
// display name (GET_LIST_EMPTY when none exist — the server answers an empty
// array; archived rows never appear).
export async function listTools(): Promise<Tool[]> {
  const data = (await request(TOOLS_URL, { headers: authTokenHeaders() })) as Tool[] | null
  return Array.isArray(data) ? data : []
}

// createTool persists a new tool belonging to exactly one tool type, with the
// optional schedule override (empty → inherit the type default).
export async function createTool(input: ToolInput): Promise<ToolWriteResult> {
  return (await request(TOOLS_URL, {
    method: 'POST',
    headers: authTokenHeaders(),
    body: JSON.stringify(buildToolBody(input)),
  })) as ToolWriteResult
}

// updateTool persists a tool. An EMPTY schedule_id CLEARS the stored override
// (the tool inherits its type's default again, AD-5). Updating an archived row
// answers the 404 sentinel server-side.
export async function updateTool(id: string, input: ToolInput): Promise<ToolWriteResult> {
  return (await request(`${TOOLS_URL}/${id}`, {
    method: 'PUT',
    headers: authTokenHeaders(),
    body: JSON.stringify(buildToolBody(input)),
  })) as ToolWriteResult
}

// archiveTool SOFT-archives one tool (the row leaves the active list; history
// is preserved server-side).
export async function archiveTool(id: string): Promise<ToolWriteResult> {
  return (await request(`${TOOLS_URL}/${id}/archive`, {
    method: 'POST',
    headers: authTokenHeaders(),
  })) as ToolWriteResult
}

// buildToolBody always sends attributes (even an empty {}) — it is the JSONB
// passthrough surface for Story 4.4: the server stores exactly what is sent
// and returns it on read. The V1 editor has no attributes UI, so it submits
// {}; a future Story 4.4 editor fills it in without a client contract change.
function buildToolBody(input: ToolInput): Record<string, unknown> {
  return {
    name: input.name,
    tool_type_id: input.tool_type_id,
    schedule_id: input.schedule_id,
    attributes: input.attributes ?? {},
  }
}

export { ApiError }

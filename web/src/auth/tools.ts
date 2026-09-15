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
// TOOL_EDIT_PERMISSION (Story 4-3b) is the scoped tool-EDIT code: a holder can
// view + edit tools (incl. the inventory number) but NOT create/archive those
// stays tools.manage-only. INSPECTION_SUBMIT_PERMISSION (Story 5.1) is the
// inspection-start/submit gate (all base roles hold it), mirroring the server
// const InspectionSubmitPermission.
export const TOOL_TYPES_PERMISSION = 'tool_types.manage'
export const TOOLS_PERMISSION = 'tools.manage'
export const TOOL_EDIT_PERMISSION = 'tool.edit'
export const INSPECTION_SUBMIT_PERMISSION = 'inspection.submit'
// REINSTATE_PERMISSION (Story 5.6, FR-15/AD-9) is the reinstatement gate code:
// only Fuehrung/Admin holders (the base roles seed it) see the
// "Wiederherstellen" button on an OOS row, mirroring the server const
// ToolReinstatePermission.
export const REINSTATE_PERMISSION = 'tool.reinstate'

export type InspectionMode = 'pass_fail' | 'checklist'

// ToolTypeChecklistItem is one ordered checklist entry of the GET payload.
export interface ToolTypeChecklistItem {
  id: string
  position: number
  label: string
}

// ToolType is the GET payload — the typed core fields, the ordered checklist
// items and the attributes jsonb extension surface (Story 4.4, FR-10/AD-3:
// attributes read back as a JSON object, always present, `{}` when empty).
// Archived types never reach the active list.
export interface ToolType {
  id: string
  name: string
  default_schedule_id: string
  required_qualification_id: string
  inspection_mode: InspectionMode
  attributes: Record<string, unknown>
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
// the complete list). attributes is the no-migration JSONB extension surface
// (Story 4.4): an ABSENT (undefined) field leaves the stored JSONB unchanged,
// an EXPLICIT `{}` clears it, a non-empty object replaces it wholesale. The
// buildToolTypeBody helper omits the field when it is undefined.
export interface ToolTypeInput {
  name: string
  default_schedule_id: string
  required_qualification_id: string
  inspection_mode: InspectionMode
  items: ToolTypeChecklistItemInput[]
  attributes?: Record<string, unknown>
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
  const body: Record<string, unknown> = {
    name: input.name,
    default_schedule_id: input.default_schedule_id,
    required_qualification_id: input.required_qualification_id,
    inspection_mode: input.inspection_mode,
    items: input.items.map((item) => ({ label: item.label })),
  }
  // Attributes follow the shared contract (Story 4.4): the field is OMITTED
  // when undefined (absent = unchanged), an explicit {} clears, an object
  // replaces.
  if (input.attributes !== undefined) {
    body.attributes = input.attributes
  }
  return body
}

// ============================================================================
// Tool surface (Story 4.3, FR-9/FR-10/AD-5): the physical tools belonging to a
// type. An EMPTY schedule_id means "inherit the tool type's default schedule";
// a present one is the per-tool override (first-class FK, AD-10).
// ============================================================================

// Tool is the GET payload — the typed core fields plus the tool type's display
// name (server JOIN), the inventory number (Story 4-3b: server-assigned on
// create, editable on edit) and the attributes jsonb passthrough. Archived
// tools never reach the active list.
export interface Tool {
  id: string
  name: string
  tool_type_id: string
  tool_type_name: string
  schedule_id: string
  inventory_number: string
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
// default schedule, AD-5). inventory_number is OPTIONAL and travels ONLY on
// PUT (Story 4-3b): on create the server AUTO-ASSIGNS it (a client value is
// ignored), on edit a non-empty value updates the stored number. attributes is
// the no-migration JSONB extension surface (Story 4.4): an ABSENT (undefined)
// field leaves the stored JSONB unchanged, an EXPLICIT `{}` clears it, a
// non-empty object replaces it wholesale. The buildToolBody helper omits the
// field when it is undefined.
export interface ToolInput {
  name: string
  tool_type_id: string
  schedule_id: string
  inventory_number?: string
  attributes?: Record<string, unknown>
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

// buildToolBody sends attributes ONLY when the input carries one (Story 4.4):
// an ABSENT field is omitted so the server's leave-unchanged semantics apply —
// an attributes-untouched save never wipes the stored JSONB. An EXPLICIT `{}`
// clears, a non-empty object replaces wholesale. The V1-era hardcoded
// `attributes: {}` on every save is gone. inventory_number is sent ONLY when
// the input carries one (the PUT edit path, Story 4-3b): on create the server
// auto-assigns it, so the create body NEVER includes it (CREATE_IGNORE_CLIENT).
function buildToolBody(input: ToolInput): Record<string, unknown> {
  const body: Record<string, unknown> = {
    name: input.name,
    tool_type_id: input.tool_type_id,
    schedule_id: input.schedule_id,
  }
  if (input.attributes !== undefined) {
    body.attributes = input.attributes
  }
  if (input.inventory_number) {
    body.inventory_number = input.inventory_number
  }
  return body
}

// ============================================================================
// Dashboard tool list (Story 4-3b + 6.1, dashboard.view): the GEAR-module
// (non-admin) tool surface. Unlike the admin tools above (which read
// /api/v1/admin/tools behind tools.manage), this reads /api/v1/tools — gated
// by dashboard.view on the server (all base roles hold it). It returns ONLY
// id, name, the type display name, the inventory number and the DERIVED
// status (Story 6.1 — computed on read, never stored, AD-4).
// ============================================================================

// ToolStatusCode is a derived tool status code (AD-4/AD-5,
// server-authoritative): `oos` (Out of Service), `red` (past due / never
// inspected), `orange` (due within the static window), `green` (current).
export type ToolStatusCode = 'oos' | 'red' | 'orange' | 'green'

// ToolStatusInfo is the SHARED derived-status shape (Story 6.1): the status
// code + the next-due timestamp (null for `oos` and the never-inspected
// `red`). The dashboard list (DashboardTool.status) and the inspection submit
// response (InspectionSubmitStatus) both use it so the vocabulary never drifts.
export interface ToolStatusInfo {
  status: ToolStatusCode
  next_due: string | null
}

// DashboardTool is the minimal GET /api/v1/tools payload (Story 4-3b + 6.1):
// the id, name, type display name and the inventory number (shown as row meta
// in the Werkzeugliste) plus the server-DERIVED status the SPA renders as the
// German label + color chip.
export interface DashboardTool {
  id: string
  name: string
  tool_type_id: string
  tool_type_name: string
  inventory_number: string
  status: ToolStatusInfo
}

const DASHBOARD_TOOLS_URL = '/api/v1/tools'

// listDashboardTools fetches the ACTIVE tool catalog for the Werkzeugliste
// (GET_LIST_EMPTY when none exist — the server answers an empty array; archived
// rows never appear).
export async function listDashboardTools(): Promise<DashboardTool[]> {
  const data = (await request(DASHBOARD_TOOLS_URL, { headers: authTokenHeaders() })) as DashboardTool[] | null
  return Array.isArray(data) ? data : []
}

// ============================================================================
// Inspection start (Story 5.1, FR-11/AD-7): the qualification-gated inspection
// start — the first Epic 5 surface. The server resolves the tool's type
// required_qualification_id (intra-module) and checks the caller's granted
// qualifications through the User module's port (EXPIRY-AWARE); the SPA button
// is a UX affordance only — the 403 IS the gate (AD-6, never client-side
// trust).
// ============================================================================

// InspectionStart is the eligible POST /api/v1/tools/{id}/inspection/start
// payload (Story 5.1 + 5.2): the tool plus its type's inspection_mode and —
// for checklist-mode types — the type's ordered checklist items, so the SPA
// renders the mode-appropriate surface. inventory_number is OPTIONAL: the
// dashboard carries it into the inspection route from the tool LIST (the /start
// response does NOT include it yet), so the inspection header can show the
// identifier when present and fall back to the tool name otherwise.
export interface InspectionStart {
  tool_id: string
  tool_name: string
  tool_type_id: string
  tool_type_name: string
  inspection_mode: InspectionMode
  checklist_items: ToolTypeChecklistItem[]
  inventory_number?: string
}

// startInspection POSTs the inspection start for a tool. 200 → eligible (the
// SPA navigates to /inspection/:toolId); 403 → ApiError with the server's
// German reason (missing/expired required qualification, or no
// inspection.submit); 401 → stale/revoked session (the caller logs in again).
export async function startInspection(toolId: string): Promise<InspectionStart> {
  return (await request(`${DASHBOARD_TOOLS_URL}/${encodeURIComponent(toolId)}/inspection/start`, {
    method: 'POST',
    headers: authTokenHeaders(),
  })) as InspectionStart
}

// ============================================================================
// Inspection submit (Story 5.4, FR-13/AD-4): the real pass_fail record call.
// The server persists identity/timestamp/result/notes (FR-13/AD-4) and derives
// the status from the full inspection + reinstatement history (AD-4/AD-5) — the
// confirmation names the OOS consequence ONLY from the returned status, never a
// client guess: a passing inspection does NOT clear OOS (reinstatement is the
// sole exit, FR-15), so the server's derived status is authoritative. Checklist
// mode stays on the placeholder seam until Story 5.5 (the `items` contract
// exists now so 5.5 wires it without a type change).
// ============================================================================

// InspectionResult is an inspection outcome (FR-12/FR-13), mirroring the
// server's pass|fail values verbatim.
export type InspectionResult = 'pass' | 'fail'

// InspectionItemSubmitInput is one checklist item of the submit body (Story 5.4
// contract; checklist wiring is Story 5.5): the type checklist item's id + the
// per-item result. The label/position SNAPSHOT is server-side only — the client
// never carries the item text.
export interface InspectionItemSubmitInput {
  item_id: string
  result: InspectionResult
}

// InspectionSubmitInput is the POST /api/v1/tools/{id}/inspection body
// (mirrors the server DTO). A pass_fail inspection carries the overall result +
// optional notes and an EMPTY items array (the server rejects provided items);
// a checklist inspection (5.5) carries one entry PER type checklist item.
export interface InspectionSubmitInput {
  mode: InspectionMode
  result: InspectionResult
  notes: string
  items: InspectionItemSubmitInput[]
}

// InspectionSubmitItem is one persisted snapshotted checklist result of the
// submit response (FR-12): label + position were copied from the type's
// checklist at submit time.
export interface InspectionSubmitItem {
  id: string
  item_id: string
  label: string
  position: number
  result: InspectionResult
}

// InspectionSubmitRecord is the persisted inspection record of the response:
// identity + timestamp + mode + overall result + notes (FR-13) + the ordered
// snapshot items (empty for pass_fail).
export interface InspectionSubmitRecord {
  id: string
  tool_id: string
  inspector_id: string
  mode: InspectionMode
  overall_result: InspectionResult
  notes: string
  submitted_at: string
  items: InspectionSubmitItem[]
}

// InspectionSubmitStatus is the server-derived status of the response
// (AD-4/AD-5): oos|red|orange|green plus the next-due timestamp (null for
// `oos` and the never-inspected `red`). It SHARES the ToolStatusInfo shape
// with the dashboard list (Story 6.1) so the vocabulary never drifts.
export type InspectionSubmitStatus = ToolStatusInfo

// InspectionSubmitResult is the POST /api/v1/tools/{id}/inspection payload
// (Story 5.3 contract): the persisted record + the server-authoritative
// derived status the confirmation consumes.
export interface InspectionSubmitResult {
  inspection: InspectionSubmitRecord
  status: InspectionSubmitStatus
}

// submitInspection POSTs the real inspection-record call (Story 5.4, FR-13):
// the server re-validates the inspection.submit gate AND the tool-type
// qualification on submit (never trusts the client, FR-11), persists
// identity/timestamp/result/notes and returns the record + derived status.
// 200 → the confirmation is server-driven; 400/403/404 → ApiError with the
// server's German reason; 401 → stale/revoked session (the caller logs in
// again).
export async function submitInspection(toolId: string, input: InspectionSubmitInput): Promise<InspectionSubmitResult> {
  return (await request(`${DASHBOARD_TOOLS_URL}/${encodeURIComponent(toolId)}/inspection`, {
    method: 'POST',
    headers: authTokenHeaders(),
    body: JSON.stringify(input),
  })) as InspectionSubmitResult
}

// submitInspectionPlaceholder is the Story 5.2 UX PLACEHOLDER seam. Story 5.4
// wired the pass_fail submit to the real submitInspection; the checklist
// surface keeps this placeholder until Story 5.5 wires it (+ "Alle bestanden").
// Story 5.5 must also derive the checklist OVERALL result before calling the
// client — `failedCount > 0 ? 'fail' : 'pass'` — the `result` field of the
// body is already mandatory, so no type change is needed.
// Deliberately NO fetch to a nonexistent endpoint. Kept async so the submit
// button has a real in-flight window for the double-submit guard.
export async function submitInspectionPlaceholder(): Promise<void> {
  await Promise.resolve()
}

// ============================================================================
// Reinstatement (Story 5.6, FR-15/AD-9): a Fuehrung/Admin reinstates an OOS
// tool with a MANDATORY reason — the SOLE exit from OOS. The server re-checks
// `tool.reinstate` defense-in-depth (AD-6), persists the reinstatement and
// returns the newly derived not-OOS status (next_due = reinstatement +
// interval, AD-5).
// ============================================================================

// ReinstateResult is the POST /api/v1/tools/{id}/reinstatement payload (Story
// 5.6): the server-derived status + the German confirmation. The status SHARES
// the ToolStatusInfo shape with the dashboard list so the vocabulary never
// drifts.
export interface ReinstateResult {
  status: ToolStatusInfo
  message: string
}

// reinstateTool POSTs the reinstatement for an OOS tool with the mandatory
// reason. 200 → the tool leaves OOS (the dashboard refetches); 400/403/404 →
// ApiError with the server's German reason; 401 → stale/revoked session (the
// caller logs in again).
export async function reinstateTool(toolId: string, reason: string): Promise<ReinstateResult> {
  return (await request(`${DASHBOARD_TOOLS_URL}/${encodeURIComponent(toolId)}/reinstatement`, {
    method: 'POST',
    headers: authTokenHeaders(),
    body: JSON.stringify({ reason }),
  })) as ReinstateResult
}

export { ApiError, DASHBOARD_TOOLS_URL }

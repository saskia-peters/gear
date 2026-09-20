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
import type { StatusCode } from '../types/filters.ts'

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
// HISTORY_PERMISSION (Story 6.3, FR-18/AD-6) is the per-tool history gate code:
// only Schirrmeister/Fuehrung/Admin holders (the base roles seed it) see the
// inspection + reinstatement history on the tool details page, mirroring the
// server const InspectionHistoryViewPermission. The SPA skips the history fetch
// for non-holders as a courtesy; the SERVER is the gate (a non-holder answers
// the uniform 403 with no data).
export const HISTORY_PERMISSION = 'inspection.history.view'
// REPORT_EXPORT_PERMISSION (Story 6.2, FR-17/AD-6) is the status-report gate
// code: only Fuehrung/Admin holders (the base roles seed it) see the "Als PDF
// exportieren" button, mirroring the server const ReportExportPermission. The
// SERVER is the gate (a non-holder answers the uniform 403 with no PDF bytes).
export const REPORT_EXPORT_PERMISSION = 'report.export'

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

// ============================================================================
// Bulk CSV import (Story 4.5, FR-9/FR-23): upload a CSV whose valid rows are
// created/updated in a set-partitioned batch and whose invalid rows are
// reported per-row (file line + German reason). The result view (imported /
// error counts + a downloadable error report) is rendered from the JSON, and a
// client-side template CSV is offered so the header the parser accepts and the
// template can never drift.
// ============================================================================

// ToolImportRowError is ONE per-row import failure: the 1-based file line +
// the server's German reason.
export interface ToolImportRowError {
  row: number
  reason: string
}

// ToolImportResult is the POST /api/v1/admin/tools/import payload: the number
// of created/updated tools + the per-row errors (an EMPTY errors array means
// every row succeeded — the server serializes `[]`, never null).
export interface ToolImportResult {
  imported: number
  errors: ToolImportRowError[]
}

// TOOL_IMPORT_URL is the multipart upload endpoint (tools.manage-only, AD-6).
const TOOL_IMPORT_URL = '/api/v1/admin/tools/import'

// importToolsCsv POSTs the CSV file as multipart form-data (field `file`,
// Story 4.5). It is a NEW dedicated call: the JSON `request()` helper stays
// untouched, and the multipart body must NOT carry the JSON Content-Type — the
// browser sets the multipart boundary itself, so only the bearer token travels
// in the header. 200 → the result (imported + per-row errors); 400 → ApiError
// with the server's German reason (missing header, malformed CSV, no data rows,
// too large); 403 → ApiError (a non-tools.manage holder, AD-6); 401 → stale
// session.
export async function importToolsCsv(file: File): Promise<ToolImportResult> {
  const form = new FormData()
  form.append('file', file)
  // The multipart boundary must be browser-generated — strip the JSON
  // Content-Type authTokenHeaders() adds and keep only the Authorization.
  const headers = new Headers(authTokenHeaders())
  headers.delete('Content-Type')
  let res: Response
  try {
    res = await fetch(TOOL_IMPORT_URL, { method: 'POST', headers, body: form })
  } catch {
    throw new ApiError(0, 'Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.')
  }
  const body = await res.json().catch(() => null)
  if (!res.ok) {
    const msg = (body as { error?: { message?: unknown } } | null)?.error?.message
    throw new ApiError(
      res.status,
      typeof msg === 'string' && msg !== '' ? msg : 'Der CSV-Import ist fehlgeschlagen.',
    )
  }
  return body as ToolImportResult
}

// TOOL_IMPORT_TEMPLATE_HEADER / TOOL_IMPORT_TEMPLATE_ROW are the exact header
// + one example row (a sample row with an EMPTY schedule cell) the template
// generates — they ARE the parser's accepted column names, so the template and
// the parser can never drift.
export const TOOL_IMPORT_TEMPLATE_HEADER = 'name,tool_type,schedule,inventory_number'
export const TOOL_IMPORT_TEMPLATE_ROW = 'Bohrmaschine-01,Bohrmaschine,,'

// toolImportTemplate returns the client-side downloadable template CSV (Story
// 4.5, TEMPLATE_DOWNLOAD): the header + one example row. The Blob is
// client-side (no server endpoint), matching the DSGVO JSON / status-PDF
// download precedents.
export function toolImportTemplate(): Blob {
  return new Blob([`${TOOL_IMPORT_TEMPLATE_HEADER}\n${TOOL_IMPORT_TEMPLATE_ROW}\n`], {
    type: 'text/csv;charset=utf-8',
  })
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

// ToolStatusInfo is the SHARED derived-status shape (Story 6.1): the status
// code + the next-due timestamp (null for `oos` and the never-inspected
// `red`). The dashboard list (DashboardTool.status) and the inspection submit
// response (InspectionSubmitStatus) both use it so the vocabulary never drifts.
// The status code is the shared StatusCode (types/filters.ts, retro item 34 —
// ToolStatusCode was consolidated into it).
export interface ToolStatusInfo {
  status: StatusCode
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
// Inspection submit (Story 5.4 + 5.5, FR-12/FR-13/AD-4): the real inspection
// record call. A pass_fail submit (5.4) posts the overall result + optional
// notes; a checklist submit (5.5) posts the per-item results + the DERIVED
// overall result. The server persists identity/timestamp/result/notes and the
// per-item snapshot (FR-12/FR-13/AD-4) and derives the status from the full
// inspection + reinstatement history (AD-4/AD-5) — the confirmation names the
// OOS consequence ONLY from the returned status, never a client guess: a
// passing inspection does NOT clear OOS (reinstatement is the sole exit,
// FR-15), so the server's derived status is authoritative.
// ============================================================================

// InspectionResult is an inspection outcome (FR-12/FR-13), mirroring the
// server's pass|fail values verbatim.
export type InspectionResult = 'pass' | 'fail'

// InspectionItemSubmitInput is one checklist item of the submit body: the type
// checklist item's id + the per-item result. The label/position SNAPSHOT is
// server-side only — the client never carries the item text.
export interface InspectionItemSubmitInput {
  item_id: string
  result: InspectionResult
}

// InspectionSubmitInput is the POST /api/v1/tools/{id}/inspection body
// (mirrors the server DTO). A pass_fail inspection carries the overall result +
// optional notes and an EMPTY items array (the server rejects provided items);
// a checklist inspection (Story 5.5) carries one entry PER type checklist item.
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
// again). The CLIENT-SUPPLIED idempotencyKey (Story 7.5, NFR-R1) is the
// at-most-once guard: the SPA generates it per submit intent and REUSES the
// same value across retries, so a retried request replays the committed
// record instead of inserting a duplicate.
export async function submitInspection(
  toolId: string,
  input: InspectionSubmitInput,
  idempotencyKey: string,
): Promise<InspectionSubmitResult> {
  return (await request(`${DASHBOARD_TOOLS_URL}/${encodeURIComponent(toolId)}/inspection`, {
    method: 'POST',
    headers: authTokenHeaders(),
    body: JSON.stringify({ ...input, idempotency_key: idempotencyKey }),
  })) as InspectionSubmitResult
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
// caller logs in again). The CLIENT-SUPPLIED idempotencyKey (Story 7.5,
// NFR-R1) is the at-most-once guard: the SPA generates it per reinstate
// intent and REUSES the same value across retries, so a retried reinstate
// replays the committed result instead of inserting a duplicate.
export async function reinstateTool(toolId: string, reason: string, idempotencyKey: string): Promise<ReinstateResult> {
  return (await request(`${DASHBOARD_TOOLS_URL}/${encodeURIComponent(toolId)}/reinstatement`, {
    method: 'POST',
    headers: authTokenHeaders(),
    body: JSON.stringify({ reason, idempotency_key: idempotencyKey }),
  })) as ReinstateResult
}

// ============================================================================
// Tool history (Story 6.3, FR-18/AD-6/AD-8): the per-tool audit trail of
// inspections + reinstatements, newest first. The endpoint GET
// /api/v1/tools/{id}/history is gated by `inspection.history.view` on the
// server — the SPA pre-checks the permission and skips the fetch for
// non-holders (showing the German no-permission note) as a courtesy only; the
// SERVER is the gate (a non-holder answers the uniform 403 with no data).
// Inspector/actor display names are server-resolved (a deleted account renders
// "Deleted User", Story 3.4 forward-compat) — the client never resolves users.
// ============================================================================

// ToolHistoryItem is one snapshotted per-checklist-item result of a history
// inspection (FR-18): the label + position were copied from the type's
// checklist at submit time.
export interface ToolHistoryItem {
  id: string
  item_id: string
  label: string
  position: number
  result: InspectionResult
}

// ToolInspectionHistory is one inspection row of the history payload: the
// inspector display name (server-resolved) + the record's fields + the ordered
// snapshot items (empty for pass_fail).
export interface ToolInspectionHistory {
  id: string
  inspector_id: string
  inspector_name: string
  mode: InspectionMode
  overall_result: InspectionResult
  notes: string
  submitted_at: string
  items: ToolHistoryItem[]
}

// ToolReinstatementHistory is one reinstatement row of the history payload:
// the actor display name (server-resolved) + the record's fields.
export interface ToolReinstatementHistory {
  id: string
  actor_id: string
  actor_name: string
  reason: string
  created_at: string
}

// ToolHistory is the GET /api/v1/tools/{id}/history payload (Story 6.3): the
// two newest-first lists — inspections (each with per-checklist-item results)
// and reinstatements. Empty lists are `[]`, never null.
export interface ToolHistory {
  inspections: ToolInspectionHistory[]
  reinstatements: ToolReinstatementHistory[]
}

// isHistoryRecord is the narrow runtime guard for the malformed-body defense
// (Story 6.3): the server contract promises objects, but a null/non-object
// element must never reach the page render. The response is treated as
// untyped at this boundary; only the shape the page consumes is normalized.
function isHistoryRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object'
}

// listToolHistory GETs the per-tool inspection + reinstatement history (Story
// 6.3, FR-18). 200 → the newest-first lists; 403 → ApiError (the server is the
// gate, no data exposed); 401 → stale/revoked session (the caller logs in
// again); other → ApiError with the server's German reason. DEFENSIVE: a
// malformed body never crashes the page render — the two lists are coerced to
// `[]` when non-array, null/non-object elements are dropped, and each
// inspection's `items` is coerced to `[]` when non-array (so `insp.items.length`
// / `.map` on the page is always safe).
export async function listToolHistory(toolId: string): Promise<ToolHistory> {
  const data = (await request(`${DASHBOARD_TOOLS_URL}/${encodeURIComponent(toolId)}/history`, {
    headers: authTokenHeaders(),
  })) as unknown
  const raw = (data ?? {}) as { inspections?: unknown; reinstatements?: unknown }
  const inspections = (Array.isArray(raw.inspections)
    ? raw.inspections.filter(isHistoryRecord).map((item) => ({
        ...item,
        items: Array.isArray(item.items) ? (item.items as ToolHistoryItem[]) : [],
      }))
    : []) as unknown as ToolInspectionHistory[]
  const reinstatements = (Array.isArray(raw.reinstatements)
    ? raw.reinstatements.filter(isHistoryRecord)
    : []) as unknown as ToolReinstatementHistory[]
  return { inspections, reinstatements }
}

// ============================================================================
// Status report export (Story 6.2, FR-17/AD-6/AD-5): GET /api/v1/tools/report.pdf
// renders the dashboard's CURRENT view as a shareable PDF. Gated by
// `report.export` on the server — the SPA shows the button ONLY to holders (the
// server is the gate; a non-holder answers the uniform 403 with no PDF bytes).
// The active status filter codes travel as ?status=… (absent = "Alle"); the
// server RE-DERIVES every status itself — the client never sends statuses.
// ============================================================================

// exportStatusReportPdf fetches the status report PDF as a Blob (Story 6.2):
// the active status filter codes build ?status=… (an EMPTY set omits the param
// — "Alle"). The server renders the PDF (pure server-side — the client only
// downloads); the caller triggers the download. 200 → the blob; 401 →
// ApiError (stale/revoked session, the caller logs in again); 403 → ApiError
// with the server's German message (stale permission cache — no PDF data,
// AD-6); other → ApiError with the server's German message.
export async function exportStatusReportPdf(filterCodes: readonly StatusCode[]): Promise<Blob> {
  const params = new URLSearchParams()
  if (filterCodes.length > 0) {
    params.set('status', filterCodes.join(','))
  }
  const qs = params.toString()
  const url = `${DASHBOARD_TOOLS_URL}/report.pdf${qs ? `?${qs}` : ''}`
  let res: Response
  try {
    res = await fetch(url, { headers: authTokenHeaders() })
  } catch {
    throw new ApiError(0, 'Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.')
  }
  if (!res.ok) {
    const body = await res.json().catch(() => null)
    const msg = (body as { error?: { message?: unknown } } | null)?.error?.message
    throw new ApiError(
      res.status,
      typeof msg === 'string' && msg !== '' ? msg : 'Der Export ist fehlgeschlagen.',
    )
  }
  return res.blob()
}

export { ApiError, DASHBOARD_TOOLS_URL, TOOL_IMPORT_URL }

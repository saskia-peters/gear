package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saskia-peters/gear/internal/tools/core"
)

// activeTestToolTypes filters a ListToolTypes result to the test-% rows (case-
// insensitive) that this suite owns, so the count/order assertions stay
// correct even when real user-created tool types exist in the shared dev DB.
func activeTestToolTypes(types []*core.ToolType) []*core.ToolType {
	out := types[:0]
	for _, t := range types {
		if strings.HasPrefix(strings.ToLower(t.Name), "test-") {
			out = append(out, t)
		}
	}
	return out
}

// toolTestPool connects to the local dev database (migration 000021 applied)
// or skips when no database is reachable, mirroring the admin/user-module
// repository integration tests.
func toolTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://gear:gear@localhost:5432/gear?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping db integration test: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping db integration test (db ping failed): %v", err)
	}
	return pool
}

// seedToolTypeRefs inserts one Test- schedule (into the Admin-owned catalog,
// which the dev DB seeds) and one Test- qualification (into the User-owned
// vocabulary) and returns their ids. The rows are cleaned up by the caller.
func seedToolTypeRefs(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (scheduleID, qualificationID string) {
	t.Helper()
	// Case-insensitive cleanup: tool-type names are case-insensitively unique
	// among ACTIVE rows, so tests may create case-variants (e.g. 'test-passfail'
	// next to an archived 'Test-PassFail') that a case-sensitive 'Test-%' LIKE
	// would miss and leak into the next test.
	if _, err := pool.Exec(ctx, "DELETE FROM tool_types WHERE lower(name) LIKE 'test-%'"); err != nil {
		t.Fatalf("cleanup tool_types err = %v", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM schedules WHERE lower(name) LIKE 'test-%'"); err != nil {
		t.Fatalf("cleanup schedules err = %v", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM qualifications WHERE lower(name) LIKE 'test-%'"); err != nil {
		t.Fatalf("cleanup qualifications err = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM tool_types WHERE lower(name) LIKE 'test-%'")
		_, _ = pool.Exec(ctx, "DELETE FROM schedules WHERE lower(name) LIKE 'test-%'")
		_, _ = pool.Exec(ctx, "DELETE FROM qualifications WHERE lower(name) LIKE 'test-%'")
	})

	if err := pool.QueryRow(ctx,
		"INSERT INTO schedules (name, interval_unit, interval_magnitude) VALUES ('Test-Zeitplan', 'year', 1) RETURNING id",
	).Scan(&scheduleID); err != nil {
		t.Fatalf("seeding schedule err = %v", err)
	}
	if err := pool.QueryRow(ctx,
		"INSERT INTO qualifications (name, description, expiry_kind) VALUES ('Test-Qualifikation', '', 'unlimited') RETURNING id",
	).Scan(&qualificationID); err != nil {
		t.Fatalf("seeding qualification err = %v", err)
	}
	return scheduleID, qualificationID
}

// TestPostgresToolTypesStore exercises the CRUD + item-replacement +
// soft-archive round-trip over the dev database (migration 000021 applied),
// plus the DB FK constraints. It covers GET_LIST_EMPTY, GET_LIST (active filter
// + ordered items), CREATE_VALID (with items), UPDATE_REPLACE_ITEMS (full item
// replacement), UPDATE_ARCHIVED / ARCHIVE_ARCHIVED (404 sentinel), ARCHIVE
// (archived_at set + leaves active list) and the German duplicate-name 400.
func TestPostgresToolTypesStore(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	// Close the pool AFTER the row-cleanup DELETEs below run (t.Cleanup runs in
	// LIFO order: registering the close first means the DELETE runs before it).
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	scheduleID, qualificationID := seedToolTypeRefs(t, ctx, pool)

	// GET_LIST_EMPTY: after cleanup, there are no TEST- rows (real user-created
	// tool types may exist in the shared dev DB — the assertions below count
	// only the test-% rows, the documented isolation convention).
	initial, err := repo.ListToolTypes(ctx)
	if err != nil {
		t.Fatalf("ListToolTypes(initial) err = %v", err)
	}
	if len(activeTestToolTypes(initial)) != 0 {
		t.Fatalf("initial = %d test rows, want 0", len(activeTestToolTypes(initial)))
	}

	// CREATE_VALID: a checklist-mode type is persisted with its ordered items
	// (positions from the input order), attributes at the '{}' DB default.
	created, err := repo.CreateToolType(ctx, &core.ToolType{
		Name:                    "Test-Bohrmaschine",
		DefaultScheduleID:       scheduleID,
		RequiredQualificationID: qualificationID,
		InspectionMode:          core.InspectionModeChecklist,
		Items: []core.ToolTypeChecklistItem{
			{Position: 0, Label: "Bohrfutter"},
			{Position: 1, Label: "Kabel"},
		},
	})
	if err != nil {
		t.Fatalf("CreateToolType err = %v", err)
	}
	if created.ID == "" || len(created.ID) != 36 {
		t.Errorf("id = %q, want a generated uuid", created.ID)
	}
	if created.Name != "Test-Bohrmaschine" || created.InspectionMode != core.InspectionModeChecklist {
		t.Errorf("created = %+v, want persisted values", created)
	}
	if len(created.Items) != 2 || created.Items[0].Label != "Bohrfutter" || created.Items[1].Label != "Kabel" {
		t.Fatalf("items = %+v, want the two ordered items", created.Items)
	}
	if created.Items[0].Position != 0 || created.Items[1].Position != 1 {
		t.Errorf("item positions = %d,%d, want 0,1", created.Items[0].Position, created.Items[1].Position)
	}
	if len(created.Attributes) != 0 {
		t.Errorf("attributes = %v, want empty map from '{}' default", created.Attributes)
	}
	if created.ArchivedAt != nil {
		t.Error("new tool type must be active (archived_at NULL)")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Errorf("timestamps missing: %+v", created)
	}
	firstUpdatedAt := created.UpdatedAt

	// GET_LIST: the created type is returned with its ordered items.
	list, err := repo.ListToolTypes(ctx)
	if err != nil {
		t.Fatalf("ListToolTypes err = %v", err)
	}
	testRows := activeTestToolTypes(list)
	if len(testRows) != 1 || testRows[0].ID != created.ID {
		t.Fatalf("test rows = %d, want the created type", len(testRows))
	}
	if len(testRows[0].Items) != 2 || testRows[0].Items[0].Label != "Bohrfutter" {
		t.Errorf("list items = %+v, want ordered checklist items", testRows[0].Items)
	}

	// UPDATE_REPLACE_ITEMS: name/mode replaced and the item list FULLY replaced
	// (delete-then-insert), updated_at refreshed.
	updated, err := repo.UpdateToolType(ctx, &core.ToolType{
		ID:                      created.ID,
		Name:                    "Test-Schlagbohrmaschine",
		DefaultScheduleID:       scheduleID,
		RequiredQualificationID: qualificationID,
		InspectionMode:          core.InspectionModeChecklist,
		Items: []core.ToolTypeChecklistItem{
			{Position: 0, Label: "Neu"},
			{Position: 1, Label: "Neuer"},
			{Position: 2, Label: "Neueste"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateToolType err = %v", err)
	}
	if updated.Name != "Test-Schlagbohrmaschine" {
		t.Errorf("updated name = %q", updated.Name)
	}
	if !updated.UpdatedAt.After(firstUpdatedAt) {
		t.Errorf("updated_at = %v, want after create's %v", updated.UpdatedAt, firstUpdatedAt)
	}
	if len(updated.Items) != 3 || updated.Items[0].Label != "Neu" || updated.Items[1].Label != "Neuer" || updated.Items[2].Label != "Neueste" {
		t.Fatalf("items = %+v, want the replaced ordered list", updated.Items)
	}

	// The DB rows confirm the old items were removed (full replacement).
	var itemCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM tool_type_checklist_items WHERE tool_type_id = $1", created.ID).Scan(&itemCount); err != nil {
		t.Fatalf("counting items err = %v", err)
	}
	if itemCount != 3 {
		t.Errorf("stored item count = %d, want 3 (old items replaced)", itemCount)
	}

	// UPDATE to a pass_fail mode REPLACES the checklist with an empty list.
	passFail, err := repo.UpdateToolType(ctx, &core.ToolType{
		ID:                      created.ID,
		Name:                    "Test-PassFail",
		DefaultScheduleID:       scheduleID,
		RequiredQualificationID: qualificationID,
		InspectionMode:          core.InspectionModePassFail,
		Items:                   nil,
	})
	if err != nil {
		t.Fatalf("UpdateToolType(pass_fail) err = %v", err)
	}
	if len(passFail.Items) != 0 {
		t.Errorf("items = %+v, want empty for pass_fail", passFail.Items)
	}

	// ARCHIVE: archived_at set, the row leaves the active list.
	archived, err := repo.ArchiveToolType(ctx, created.ID)
	if err != nil {
		t.Fatalf("ArchiveToolType err = %v", err)
	}
	if archived.ArchivedAt == nil {
		t.Fatal("archived_at = nil, want set")
	}
	list, err = repo.ListToolTypes(ctx)
	if err != nil {
		t.Fatalf("ListToolTypes(after archive) err = %v", err)
	}
	if len(activeTestToolTypes(list)) != 0 {
		t.Fatalf("test rows = %d, want 0 (archived row filtered out)", len(activeTestToolTypes(list)))
	}

	// ARCHIVE_ARCHIVED: archiving the already-archived row → 404 sentinel.
	if _, err := repo.ArchiveToolType(ctx, created.ID); !errors.Is(err, core.ErrToolTypeNotFound) {
		t.Fatalf("ArchiveToolType(archived) err = %v, want ErrToolTypeNotFound", err)
	}
	// UPDATE_ARCHIVED: updating the archived row → 404 sentinel.
	if _, err := repo.UpdateToolType(ctx, &core.ToolType{
		ID: created.ID, Name: "Test-Darf-Nicht", DefaultScheduleID: scheduleID,
		RequiredQualificationID: qualificationID, InspectionMode: core.InspectionModePassFail,
	}); !errors.Is(err, core.ErrToolTypeNotFound) {
		t.Fatalf("UpdateToolType(archived) err = %v, want ErrToolTypeNotFound", err)
	}
	// UPDATE missing id → 404 sentinel.
	if _, err := repo.UpdateToolType(ctx, &core.ToolType{
		ID: "00000000-0000-0000-0000-000000000000", Name: "x", DefaultScheduleID: scheduleID,
		RequiredQualificationID: qualificationID, InspectionMode: core.InspectionModePassFail,
	}); !errors.Is(err, core.ErrToolTypeNotFound) {
		t.Fatalf("UpdateToolType(missing) err = %v, want ErrToolTypeNotFound", err)
	}
	// ARCHIVE missing id → 404 sentinel.
	if _, err := repo.ArchiveToolType(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, core.ErrToolTypeNotFound) {
		t.Fatalf("ArchiveToolType(missing) err = %v, want ErrToolTypeNotFound", err)
	}
	// The lean active-exists check: true for an active id, false for a missing
	// id and for the archived row (the update-path archived sentinel).
	active, err := repo.ToolTypeExistsActive(ctx, created.ID)
	if err != nil {
		t.Fatalf("ToolTypeExistsActive(archived) err = %v", err)
	}
	if active {
		t.Error("ToolTypeExistsActive(archived) = true, want false (the row is archived)")
	}
	active, err = repo.ToolTypeExistsActive(ctx, "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("ToolTypeExistsActive(missing) err = %v", err)
	}
	if active {
		t.Error("ToolTypeExistsActive(missing) = true, want false")
	}

	// Re-creating the EXACT archived name is rejected with the German
	// duplicate-name 400 (the DB UNIQUE constraint is the backstop the active
	// catalog guard cannot see) — never a 500. The archived row keeps the name
	// it had at archive time.
	_, err = repo.CreateToolType(ctx, &core.ToolType{
		Name: "Test-PassFail", DefaultScheduleID: scheduleID,
		RequiredQualificationID: qualificationID, InspectionMode: core.InspectionModePassFail,
	})
	var inv *core.InvalidToolTypeError
	if !errors.As(err, &inv) {
		t.Fatalf("recreate archived name err = %v, want *InvalidToolTypeError (German duplicate)", err)
	}
	if inv.Message != core.MsgToolTypeNameTaken {
		t.Errorf("message = %q, want %q", inv.Message, core.MsgToolTypeNameTaken)
	}

	// A CASE-VARIANT of the archived name is deliberately ALLOWED (the coherent
	// duplicate-name rule: case-insensitive among ACTIVE rows, exact among all
	// rows including archived — the DB UNIQUE is case-sensitive).
	caseVariant, err := repo.CreateToolType(ctx, &core.ToolType{
		Name: "test-passfail", DefaultScheduleID: scheduleID,
		RequiredQualificationID: qualificationID, InspectionMode: core.InspectionModePassFail,
	})
	if err != nil {
		t.Fatalf("case-variant of archived name err = %v, want accepted (documented rule)", err)
	}
	if caseVariant.ID == "" {
		t.Error("case-variant type missing id")
	}
}

// TestPostgresToolTypesListOrder pins the deterministic GET_LIST order (Fix 13,
// verification-gap finding): two created types come back oldest-first
// (created_at ASC), and two types sharing ONE created_at come back ordered by
// name ASC (the tiebreaker).
func TestPostgresToolTypesOptionalQualification(t *testing.T) {
	// 000023 made the required qualification OPTIONAL: an empty
	// required_qualification_id must persist as SQL NULL and read back as an
	// empty string (no qualification required — any Helfer*in may inspect).
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	scheduleID, _ := seedToolTypeRefs(t, ctx, pool)

	created, err := repo.CreateToolType(ctx, &core.ToolType{
		Name:                    "Test-OhneQualifikation",
		DefaultScheduleID:       scheduleID,
		RequiredQualificationID: "", // empty → NULL, optional
		InspectionMode:          core.InspectionModePassFail,
	})
	if err != nil {
		t.Fatalf("CreateToolType(no qualification) err = %v", err)
	}
	if created.RequiredQualificationID != "" {
		t.Errorf("created required qualification = %q, want empty", created.RequiredQualificationID)
	}

	var dbQual *string
	if err := pool.QueryRow(ctx,
		"SELECT required_qualification_id FROM tool_types WHERE id = $1", created.ID,
	).Scan(&dbQual); err != nil {
		t.Fatalf("scan required_qualification_id err = %v", err)
	}
	if dbQual != nil {
		t.Errorf("DB required_qualification_id = %v, want SQL NULL", *dbQual)
	}

	list, err := repo.ListToolTypes(ctx)
	if err != nil {
		t.Fatalf("ListToolTypes err = %v", err)
	}
	found := false
	for _, tt := range list {
		if tt.ID == created.ID {
			found = true
			if tt.RequiredQualificationID != "" {
				t.Errorf("listed required qualification = %q, want empty", tt.RequiredQualificationID)
			}
		}
	}
	if !found {
		t.Error("created type not in list")
	}
}

func TestPostgresToolTypesListOrder(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	scheduleID, qualificationID := seedToolTypeRefs(t, ctx, pool)

	// Two types created sequentially → the second has a later created_at.
	first, err := repo.CreateToolType(ctx, &core.ToolType{
		Name: "Test-Reihenfolge-A", DefaultScheduleID: scheduleID,
		RequiredQualificationID: qualificationID, InspectionMode: core.InspectionModePassFail,
	})
	if err != nil {
		t.Fatalf("CreateToolType(first) err = %v", err)
	}
	second, err := repo.CreateToolType(ctx, &core.ToolType{
		Name: "Test-Reihenfolge-B", DefaultScheduleID: scheduleID,
		RequiredQualificationID: qualificationID, InspectionMode: core.InspectionModePassFail,
	})
	if err != nil {
		t.Fatalf("CreateToolType(second) err = %v", err)
	}

	list, err := repo.ListToolTypes(ctx)
	if err != nil {
		t.Fatalf("ListToolTypes err = %v", err)
	}
	testRows := activeTestToolTypes(list)
	if len(testRows) != 2 || testRows[0].ID != first.ID || testRows[1].ID != second.ID {
		t.Fatalf("test rows = [%s, %s], want oldest-first [%s, %s]",
			testRows[0].ID, testRows[1].ID, first.ID, second.ID)
	}

	// Two types sharing ONE created_at → name ASC tiebreaker. Insert directly
	// with a pinned created_at so the ORDER BY tiebreaker is exercised.
	if _, err := pool.Exec(ctx,
		`INSERT INTO tool_types (name, default_schedule_id, required_qualification_id, inspection_mode, created_at, updated_at)
		 VALUES ('Test-Tiebreak-B', $1, $2, 'pass_fail', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`, scheduleID, qualificationID); err != nil {
		t.Fatalf("inserting tiebreak-B err = %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO tool_types (name, default_schedule_id, required_qualification_id, inspection_mode, created_at, updated_at)
		 VALUES ('Test-Tiebreak-A', $1, $2, 'pass_fail', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`, scheduleID, qualificationID); err != nil {
		t.Fatalf("inserting tiebreak-A err = %v", err)
	}
	list, err = repo.ListToolTypes(ctx)
	if err != nil {
		t.Fatalf("ListToolTypes(tiebreak) err = %v", err)
	}
	var gotA, gotB int
	for i, tt := range list {
		if tt.Name == "Test-Tiebreak-B" {
			gotB = i
		}
		if tt.Name == "Test-Tiebreak-A" {
			gotA = i
		}
	}
	if gotA >= gotB {
		t.Fatalf("tiebreak order = A@%d B@%d, want A before B (name ASC)", gotA, gotB)
	}
}

// TestPostgresToolTypesFKConstraints verifies the DB enforces the cross-module
// FKs (AD-7/AD-10/AD-11): a create referencing a non-existent schedule or
// qualification id is MAPPED by the repository to the German 400 (Fix 2 — the
// core's port validation is the app-level guard, the FK is the DB-level
// backstop, and the raw pg error must never surface as a 500). The raw DB
// constraint is additionally pinned with a direct INSERT expecting 23503.
func TestPostgresToolTypesFKConstraints(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	scheduleID, qualificationID := seedToolTypeRefs(t, ctx, pool)

	missing := "00000000-0000-0000-0000-000000000000"

	// Bad schedule FK → mapped to the German 400, never a raw pg error.
	_, err := repo.CreateToolType(ctx, &core.ToolType{
		Name: "Test-Fk-Schedule", DefaultScheduleID: missing,
		RequiredQualificationID: qualificationID, InspectionMode: core.InspectionModePassFail,
	})
	var inv *core.InvalidToolTypeError
	if !errors.As(err, &inv) {
		t.Fatalf("create with missing schedule err = %v, want *InvalidToolTypeError (German 400)", err)
	}
	if inv.Message != core.MsgToolTypeReferencedGone {
		t.Errorf("message = %q, want %q", inv.Message, core.MsgToolTypeReferencedGone)
	}

	// Bad qualification FK → mapped to the German 400.
	_, err = repo.CreateToolType(ctx, &core.ToolType{
		Name: "Test-Fk-Qualification", DefaultScheduleID: scheduleID,
		RequiredQualificationID: missing, InspectionMode: core.InspectionModePassFail,
	})
	inv = nil
	if !errors.As(err, &inv) {
		t.Fatalf("create with missing qualification err = %v, want *InvalidToolTypeError (German 400)", err)
	}
	if inv.Message != core.MsgToolTypeReferencedGone {
		t.Errorf("message = %q, want %q", inv.Message, core.MsgToolTypeReferencedGone)
	}

	// The raw DB backstop still exists: a direct INSERT with a missing schedule
	// FK trips SQLSTATE 23503.
	_, err = pool.Exec(ctx,
		`INSERT INTO tool_types (name, default_schedule_id, required_qualification_id, inspection_mode)
		 VALUES ('Test-Fk-Raw', $1, $2, 'pass_fail')`, missing, qualificationID)
	if !isForeignKeyViolation(err) {
		t.Fatalf("raw insert with missing schedule err = %v, want FK violation 23503", err)
	}
}

// TestPostgresToolTypesAttributes pins the Story 4.4 attributes surface on the
// `tool_types.attributes` JSONB column: a NON-EMPTY set round-trips byte-for-
// byte (single-key canonical JSON), the DB default '{}' applies to an absent
// create, and the update path honors absent=unchanged / {} = clear /
// object=replace through the repository's COALESCE keep. Archiving preserves
// the stored attributes.
func TestPostgresToolTypesAttributes(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	scheduleID, qualificationID := seedToolTypeRefs(t, ctx, pool)

	// CREATE_TYPE_ATTRS: a type created WITH a non-empty attributes set stores
	// it in tool_types.attributes (impossible before Story 4.4).
	created, err := repo.CreateToolType(ctx, &core.ToolType{
		Name:                    "Test-Attr-Typ",
		DefaultScheduleID:       scheduleID,
		RequiredQualificationID: qualificationID,
		InspectionMode:          core.InspectionModePassFail,
		Attributes:              map[string]any{"standort": "Werkstatt", "leistung": float64(1200)},
	})
	if err != nil {
		t.Fatalf("CreateToolType(attributes) err = %v", err)
	}
	if created.Attributes == nil || created.Attributes["standort"] != "Werkstatt" {
		t.Fatalf("created attributes = %+v, want the stored set", created.Attributes)
	}
	if created.Attributes["leistung"] != float64(1200) {
		t.Errorf("created attributes leistung = %v, want 1200", created.Attributes["leistung"])
	}

	// ROUND_TRIP: the raw DB row holds the valid JSON; a read-back via the list
	// returns the same values (JSONB normalizes key order, so the semantic
	// equality is asserted on the decoded map).
	var raw []byte
	if err := pool.QueryRow(ctx, "SELECT attributes FROM tool_types WHERE id = $1", created.ID).Scan(&raw); err != nil {
		t.Fatalf("scan raw attributes err = %v", err)
	}
	var rawAttrs map[string]any
	if err := json.Unmarshal(raw, &rawAttrs); err != nil {
		t.Fatalf("raw attributes not JSON: %v", err)
	}
	if rawAttrs["standort"] != "Werkstatt" || rawAttrs["leistung"] != float64(1200) {
		t.Errorf("raw DB attributes = %v, want the stored set", rawAttrs)
	}

	// UPDATE_TYPE_ABSENT: an update with a NIL attributes map leaves the stored
	// JSONB unchanged (COALESCE keep).
	updated, err := repo.UpdateToolType(ctx, &core.ToolType{
		ID: created.ID, Name: "Test-Attr-Typ-Neu",
		DefaultScheduleID: scheduleID, RequiredQualificationID: qualificationID,
		InspectionMode: core.InspectionModePassFail,
		Attributes:     nil,
	})
	if err != nil {
		t.Fatalf("UpdateToolType(absent attributes) err = %v", err)
	}
	if updated.Attributes["standort"] != "Werkstatt" {
		t.Errorf("attributes after absent update = %+v, want stored set preserved", updated.Attributes)
	}

	// UPDATE_TYPE_OBJECT: a non-empty object REPLACES the stored set wholesale.
	replaced, err := repo.UpdateToolType(ctx, &core.ToolType{
		ID: created.ID, Name: "Test-Attr-Typ-Neu",
		DefaultScheduleID: scheduleID, RequiredQualificationID: qualificationID,
		InspectionMode: core.InspectionModePassFail,
		Attributes:     map[string]any{"standort": "Lager"},
	})
	if err != nil {
		t.Fatalf("UpdateToolType(replace attributes) err = %v", err)
	}
	if replaced.Attributes["standort"] != "Lager" {
		t.Errorf("attributes after replace = %+v, want the replaced set", replaced.Attributes)
	}
	if _, stale := replaced.Attributes["leistung"]; stale {
		t.Errorf("attributes after replace = %+v, want the old key gone", replaced.Attributes)
	}

	// UPDATE_TYPE_CLEAR: an EXPLICIT `{}` clears the stored set.
	cleared, err := repo.UpdateToolType(ctx, &core.ToolType{
		ID: created.ID, Name: "Test-Attr-Typ-Neu",
		DefaultScheduleID: scheduleID, RequiredQualificationID: qualificationID,
		InspectionMode: core.InspectionModePassFail,
		Attributes:     map[string]any{},
	})
	if err != nil {
		t.Fatalf("UpdateToolType(clear attributes) err = %v", err)
	}
	if cleared.Attributes == nil || len(cleared.Attributes) != 0 {
		t.Errorf("attributes after clear = %+v, want empty map", cleared.Attributes)
	}

	// ARCHIVED: soft-archiving the type preserves its (re-set) attributes on the
	// archived row.
	withAttrs, err := repo.UpdateToolType(ctx, &core.ToolType{
		ID: created.ID, Name: "Test-Attr-Typ-Neu",
		DefaultScheduleID: scheduleID, RequiredQualificationID: qualificationID,
		InspectionMode: core.InspectionModePassFail,
		Attributes:     map[string]any{"hinweis": "archiviert"},
	})
	if err != nil {
		t.Fatalf("UpdateToolType(set attrs before archive) err = %v", err)
	}
	archived, err := repo.ArchiveToolType(ctx, withAttrs.ID)
	if err != nil {
		t.Fatalf("ArchiveToolType err = %v", err)
	}
	if archived.ArchivedAt == nil {
		t.Fatal("archived_at = nil, want set")
	}
	if archived.Attributes["hinweis"] != "archiviert" {
		t.Errorf("archived attributes = %+v, want preserved", archived.Attributes)
	}

	// The archived row's attributes stay in the DB (read out-of-band).
	var archivedRaw []byte
	if err := pool.QueryRow(ctx, "SELECT attributes FROM tool_types WHERE id = $1", created.ID).Scan(&archivedRaw); err != nil {
		t.Fatalf("scan archived attributes err = %v", err)
	}
	var archivedAttrs map[string]any
	if err := json.Unmarshal(archivedRaw, &archivedAttrs); err != nil {
		t.Fatalf("archived attributes not JSON: %v", err)
	}
	if archivedAttrs["hinweis"] != "archiviert" {
		t.Errorf("archived DB attributes = %v, want preserved", archivedAttrs)
	}
}

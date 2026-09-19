package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)// ToolImportRow is ONE parsed CSV row (Story 4.5, FR-9/FR-23). The HTTP
// adapter decodes the uploaded bytes into structured rows — the hexagon: core
// NEVER parses CSV. Line is the 1-based file line (header = 1, first data row
// = 2) so a per-row error can point at the exact file line. The names are the
// match keys: the tool's Name (the upsert key), the tool_type / schedule BY
// NAME (resolved against the ACTIVE catalog). An empty ScheduleName /
// InventoryNumber counts as NOT provided.
type ToolImportRow struct {
	Line            int
	Name            string
	ToolTypeName    string
	ScheduleName    string
	InventoryNumber string
}

// ToolImportError is one per-row import failure (FR-9): the file line + a
// German reason. An invalid row NEVER produces a tool record.
type ToolImportError struct {
	Row    int
	Reason string
}

// ToolImportResult is the import summary (HTTP 200): the number of tools
// created/updated plus the per-row errors. An EMPTY Errors list means every
// row succeeded.
type ToolImportResult struct {
	Imported int
	Errors   []ToolImportError
}

// ToolImportUpdate is ONE update-set row the store persists (Story 4.5). The
// CSV row is the DIFF, not the target state: ToolTypeID is ALWAYS applied,
// while ScheduleID/InventoryNumber apply ONLY when the matching Provided flag
// is true — an absent (empty) cell PRESERVES the stored value (the absolute
// no-data-loss rule, FR-9). Attributes/name/archived_at are NEVER touched by
// the import.
type ToolImportUpdate struct {
	ToolID            string
	ToolTypeID        string
	ScheduleID        string
	ScheduleProvided  bool
	InventoryNumber   string
	InventoryProvided bool
}
// ImportTools persists a bulk CSV import (Story 4.5, FR-9/FR-23/FR-10/AD-6).
// The HTTP adapter has already parsed the uploaded bytes into structured rows
// (the hexagon — this core NEVER parses CSV). Gated `tools.manage`-ONLY
// defense-in-depth (AD-6): a tool.edit-only holder gets the uniform 403 with
// no tool data.
//
// Execution is SET-PARTITIONED (performance): Phase A loads the tool-type
// catalog, the ACTIVE schedule catalog (via the Admin SchedulesPort) and the
// ACTIVE tools ONCE; Phase B validates every row and partitions the valid ones
// into a `newSet` (name not active) and an `updateSet` (name already active),
// collecting per-row errors for the invalid ones; Phase C persists EACH set as
// ONE batched store call in its own transaction (~2-4 round-trips + 2 commits
// regardless of row count — NO row-by-row round-trips).
//
// Row validation (FR-9 per-row atomicity):
//   - name required + bounded; tool_type required, resolved BY NAME against the
//     ACTIVE types; schedule optional, resolved BY NAME against the ACTIVE
//     schedules (an EMPTY cell = NOT provided); inventory optional, bounded to
//     InventoryNumberMaxLength (an EMPTY cell = NOT provided).
//   - an invalid row produces NO tool record and is reported with its file line
//   - a German reason; valid rows persist even when other rows fail.
//   - a within-file duplicate name/inventory → the LATER row is a row error.
//
// Update rows are the DIFF, not the target state (absolute no-data-loss, FR-9):
// tool_type_id is always applied, schedule/inventory apply ONLY when the cell
// is non-empty — an empty cell PRESERVES the stored value; attributes/name/
// archived_at are never touched.
//
// Collision precision: a FindToolCollisions pre-check (the 4-3b backstop)
// reports exact-name / case-insensitive-inventory collisions against ALL rows
// (active + archived) so archived-name/archived-inventory rows fail with
// precise German row errors BEFORE the batch; ON CONFLICT DO NOTHING absorbs
// concurrent races → generic "bereits vergeben" row error, never an abort.
// Audited ONCE per call (`tool.import`, detail "imported=N errors=M").
func (s *Service) ImportTools(ctx context.Context, actorID string, rows []ToolImportRow) (*ToolImportResult, error) {
	if err := s.requireToolsPermission(ctx, actorID, []string{ToolsManagePermission}); err != nil {
		return nil, err
	}

	// Phase A: load the maps ONCE (no N+1).
	toolTypes, err := s.store.ListToolTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to load tool types: %w", err)
	}
	if s.schedules == nil {
		return nil, fmt.Errorf("tools core: schedule catalog port is not wired")
	}
	schedules, err := s.schedules.CurrentSchedules(ctx)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to resolve schedule catalog: %w", err)
	}
	tools, err := s.store.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to load tools: %w", err)
	}

	typeIDByName := make(map[string]string, len(toolTypes))
	for _, tt := range toolTypes {
		if tt.ArchivedAt != nil {
			continue
		}
		typeIDByName[strings.ToLower(tt.Name)] = tt.ID
	}
	scheduleIDByName := make(map[string]string, len(schedules))
	for _, sc := range schedules {
		scheduleIDByName[strings.ToLower(sc.Name)] = sc.ID
	}
	toolByName := make(map[string]*Tool, len(tools))
	toolByInventoryLower := make(map[string]*Tool, len(tools))
	for _, t := range tools {
		toolByName[strings.ToLower(t.Name)] = t
		if t.InventoryNumber != "" {
			toolByInventoryLower[strings.ToLower(t.InventoryNumber)] = t
		}
	}

	result := &ToolImportResult{Errors: []ToolImportError{}}
	var newSet []*Tool
	var updateSet []ToolImportUpdate
	var newLines []int
	var updateLines []int
	seenNames := make(map[string]struct{}, len(rows))
	seenInventories := make(map[string]struct{}, len(rows))

	// Phase B: validate every row and partition it into new/update.
	for _, row := range rows {
		line := row.Line
		name := strings.TrimSpace(row.Name)
		if name == "" {
			result.Errors = append(result.Errors, ToolImportError{Row: line, Reason: "Bitte gib einen Namen für das Werkzeug an."})
			continue
		}
		if utf8.RuneCountInString(name) > 255 {
			result.Errors = append(result.Errors, ToolImportError{Row: line, Reason: "Der Name ist zu lang."})
			continue
		}
		// Within-file duplicate name → the LATER row is a row error.
		lowerName := strings.ToLower(name)
		if _, dup := seenNames[lowerName]; dup {
			result.Errors = append(result.Errors, ToolImportError{Row: line, Reason: MsgToolImportNameDuplicateInFile})
			continue
		}
		seenNames[lowerName] = struct{}{}

		typeName := strings.TrimSpace(row.ToolTypeName)
		if typeName == "" {
			result.Errors = append(result.Errors, ToolImportError{Row: line, Reason: "Bitte wähle einen Gerätetyp aus."})
			continue
		}
		toolTypeID, ok := typeIDByName[strings.ToLower(typeName)]
		if !ok {
			result.Errors = append(result.Errors, ToolImportError{Row: line, Reason: fmt.Sprintf(MsgToolImportTypeNotFound, typeName)})
			continue
		}

		var scheduleID string
		scheduleProvided := false
		if scheduleName := strings.TrimSpace(row.ScheduleName); scheduleName != "" {
			scheduleID, ok = scheduleIDByName[strings.ToLower(scheduleName)]
			if !ok {
				result.Errors = append(result.Errors, ToolImportError{Row: line, Reason: fmt.Sprintf(MsgToolImportScheduleNotFound, scheduleName)})
				continue
			}
			scheduleProvided = true
		}

		inventory := strings.TrimSpace(row.InventoryNumber)
		inventoryProvided := inventory != ""
		if inventoryProvided && utf8.RuneCountInString(inventory) > InventoryNumberMaxLength {
			result.Errors = append(result.Errors, ToolImportError{Row: line, Reason: MsgToolInventoryNumberTooLong})
			continue
		}

		// Active-inventory collision (IMPORT_INV_TAKEN): a provided number held
		// by ANOTHER ACTIVE tool is a row error. For an update row the tool's
		// OWN current number is a NO-OP (re-importing it is not a write, so it
		// neither claims the in-file dedup slot nor risks a collision).
		if existing, isUpdate := toolByName[lowerName]; isUpdate {
			if inventoryProvided && strings.EqualFold(inventory, existing.InventoryNumber) {
				inventoryProvided = false
				inventory = ""
			}
			if inventoryProvided {
				// Within-file duplicate inventory → the LATER row is a row error.
				lowerInv := strings.ToLower(inventory)
				if _, dup := seenInventories[lowerInv]; dup {
					result.Errors = append(result.Errors, ToolImportError{Row: line, Reason: MsgToolImportInventoryDuplicateInFile})
					continue
				}
				seenInventories[lowerInv] = struct{}{}
				if holder, hit := toolByInventoryLower[lowerInv]; hit && holder.ID != existing.ID {
					result.Errors = append(result.Errors, ToolImportError{Row: line, Reason: MsgToolInventoryNumberTaken})
					continue
				}
			}
			updateSet = append(updateSet, ToolImportUpdate{
				ToolID:            existing.ID,
				ToolTypeID:        toolTypeID,
				ScheduleID:        scheduleID,
				ScheduleProvided:  scheduleProvided,
				InventoryNumber:   inventory,
				InventoryProvided: inventoryProvided,
			})
			updateLines = append(updateLines, line)
			continue
		}

		if inventoryProvided {
			// Within-file duplicate inventory → the LATER row is a row error.
			lowerInv := strings.ToLower(inventory)
			if _, dup := seenInventories[lowerInv]; dup {
				result.Errors = append(result.Errors, ToolImportError{Row: line, Reason: MsgToolImportInventoryDuplicateInFile})
				continue
			}
			seenInventories[lowerInv] = struct{}{}
			if _, hit := toolByInventoryLower[lowerInv]; hit {
				result.Errors = append(result.Errors, ToolImportError{Row: line, Reason: MsgToolInventoryNumberTaken})
				continue
			}
		}
		newSet = append(newSet, &Tool{
			Name:            name,
			ToolTypeID:      toolTypeID,
			ScheduleID:      scheduleID,
			InventoryNumber: inventory,
			Attributes:      map[string]any{},
		})
		newLines = append(newLines, line)
	}

	// Collision pre-check (the 4-3b archived backstop): exact name +
	// case-insensitive inventory over ALL rows — an archived tool keeps its
	// name/inventory "taken". It covers the NEW set's names + explicit
	// inventories AND the UPDATE set's PROVIDED (non-self) inventories, so a
	// single archived-inventory collision fails that row with a precise German
	// row error BEFORE the batch — never degrading the whole update batch into
	// per-row fallback. Re-importing a tool's own number is excluded (the Phase
	// B no-op), and within-file duplicates were already rejected per-row.
	if len(newSet) > 0 || len(updateSet) > 0 {
		names := make([]string, 0, len(newSet))
		inventoryNumbers := make([]string, 0, len(newSet)+len(updateSet))
		for _, t := range newSet {
			names = append(names, t.Name)
			if t.InventoryNumber != "" {
				inventoryNumbers = append(inventoryNumbers, t.InventoryNumber)
			}
		}
		for _, u := range updateSet {
			if u.InventoryProvided {
				inventoryNumbers = append(inventoryNumbers, u.InventoryNumber)
			}
		}
		nameHits, inventoryHits, err := s.store.FindToolCollisions(ctx, names, inventoryNumbers)
		if err != nil {
			return nil, fmt.Errorf("tools core: failed to check import collisions: %w", err)
		}

		// Filter the NEW set: a name/inventory hit is a precise row error.
		kept := newSet[:0]
		keptLines := newLines[:0]
		for i, t := range newSet {
			if _, hit := nameHits[t.Name]; hit {
				result.Errors = append(result.Errors, ToolImportError{Row: newLines[i], Reason: MsgToolNameTaken})
				continue
			}
			if t.InventoryNumber != "" {
				if _, hit := inventoryHits[strings.ToLower(t.InventoryNumber)]; hit {
					result.Errors = append(result.Errors, ToolImportError{Row: newLines[i], Reason: MsgToolInventoryNumberTaken})
					continue
				}
			}
			kept = append(kept, t)
			keptLines = append(keptLines, newLines[i])
		}
		newSet, newLines = kept, keptLines

		// Filter the UPDATE set: an inventory hit is a precise row error. A
		// NAME hit on an update row is the tool's OWN active name (the DB UNIQUE
		// name never allows an identical archived row), so it is never an error.
		keptUpdates := updateSet[:0]
		keptUpdateLines := updateLines[:0]
		for i, u := range updateSet {
			if u.InventoryProvided {
				if _, hit := inventoryHits[strings.ToLower(u.InventoryNumber)]; hit {
					result.Errors = append(result.Errors, ToolImportError{Row: updateLines[i], Reason: MsgToolInventoryNumberTaken})
					continue
				}
			}
			keptUpdates = append(keptUpdates, u)
			keptUpdateLines = append(keptUpdateLines, updateLines[i])
		}
		updateSet, updateLines = keptUpdates, keptUpdateLines
	}

	// Phase C: persist each set as ONE batched store call in its own
	// transaction (set-partitioned execution). A per-row failure inside a batch
	// (a concurrent race) becomes a generic "bereits vergeben" row error — the
	// batch never aborts wholesale.
	if len(newSet) > 0 {
		inventoryPrefix, inventoryWidth := s.inventoryNumberFormat(ctx)
		created, failed, err := s.store.CreateToolsBatch(ctx, newSet, inventoryPrefix, inventoryWidth)
		if err != nil {
			return nil, fmt.Errorf("tools core: failed to batch-create tools: %w", err)
		}
		result.Imported += len(created)
		for idx, batchErr := range failed {
			result.Errors = append(result.Errors, ToolImportError{Row: newLines[idx], Reason: importRowErrorReason(batchErr)})
		}
	}

	if len(updateSet) > 0 {
		updated, updateErrs, err := s.store.UpdateToolsBatch(ctx, updateSet)
		if err != nil {
			return nil, fmt.Errorf("tools core: failed to batch-update tools: %w", err)
		}
		result.Imported += len(updated)
		for idx, updateErr := range updateErrs {
			if updateErr == nil {
				continue
			}
			result.Errors = append(result.Errors, ToolImportError{Row: updateLines[idx], Reason: importRowErrorReason(updateErr)})
		}
	}

	// ONE audit event per call (NFR-O2): actor/timestamp/operation + the
	// imported/error counts — never a per-row audit flood.
	s.auditTool(ctx, actorID, AuditOperationToolImport,
		fmt.Sprintf("action=import target=tool imported=%d errors=%d", result.Imported, len(result.Errors)))

	// Present the errors in file-line order (deterministic result).
	sort.SliceStable(result.Errors, func(i, j int) bool { return result.Errors[i].Row < result.Errors[j].Row })
	return result, nil
}

// importRowErrorReason maps a batch-store failure to the German row reason: a
// store that already mapped the failure to an InvalidToolError carries the
// precise microcopy (duplicate name/inventory, referenced-gone); anything else
// (a concurrent race / a tool archived mid-import) falls back to the generic
// "bereits vergeben".
func importRowErrorReason(err error) string {
	var inv *InvalidToolError
	if errors.As(err, &inv) && inv.Message != "" {
		return inv.Message
	}
	return MsgToolImportCollision
}

package core

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	admcore "github.com/saskia-peters/gear/internal/admin/core"
)

// resolveToolScheduleInterval resolves the tool's EFFECTIVE inspection
// schedule interval (AD-5/AD-16): the per-tool override when set, else the
// tool type's default — both plain FK ids resolved through the Admin module's
// read-only SchedulesPort (the Tool module never joins the Admin tables,
// AD-8/AD-11). A nil port is a composition-root wiring defect and FAILS
// LOUDLY; an effective schedule missing from the ACTIVE catalog (archived /
// vanished) is the same — the clock must never silently resolve to a wrong
// interval. The catalog is read here and the pure scheduleIntervalFor helper
// resolves the effective interval against that snapshot.
func (s *Service) resolveToolScheduleInterval(ctx context.Context, tool *ToolWithTypeQualification) (time.Duration, error) {
	if s.schedules == nil {
		return 0, fmt.Errorf("tools core: schedule catalog port is not wired")
	}
	schedules, err := s.schedules.CurrentSchedules(ctx)
	if err != nil {
		return 0, fmt.Errorf("tools core: failed to resolve schedule catalog: %w", err)
	}
	return scheduleIntervalFor(tool.ScheduleID, tool.DefaultScheduleID, schedules)
}

// scheduleIntervalFor resolves a tool's EFFECTIVE schedule interval
// (AD-5/AD-16) against a catalog snapshot: the per-tool OVERRIDE when set,
// else the tool type's DEFAULT — both plain FK ids. It is the PURE helper
// shared by the inspection submit path (resolveToolScheduleInterval) and the
// resilient dashboard read (ListToolsForDashboard, Story 6.1) — the dashboard
// resolves the catalog ONCE per call and feeds this helper per tool (no N+1
// catalog reads, the spec's invariant). A missing/invalid effective schedule
// surfaces as an error: the submit path FAILS LOUDLY, while the dashboard
// renders that single tool `red` (never falsely serviceable).
func scheduleIntervalFor(overrideID, defaultID string, schedules []*admcore.Schedule) (time.Duration, error) {
	scheduleID := overrideID
	if scheduleID == "" {
		scheduleID = defaultID
	}
	for _, sch := range schedules {
		if sch.ID == scheduleID {
			interval := scheduleInterval(sch.IntervalUnit, sch.IntervalMagnitude)
			if interval <= 0 {
				return 0, fmt.Errorf("tools core: effective schedule %q has an invalid interval", scheduleID)
			}
			return interval, nil
		}
	}
	return 0, fmt.Errorf("tools core: effective schedule %q not in the active catalog", scheduleID)
}

// validateInspectionInput enforces the submit contract (SUBMIT_INVALID):
// the mode must match the tool type's, the overall result pass|fail, the notes
// ≤ 4000 runes, and the checklist item set must EXACTLY equal the type's
// checklist (all answered, no extras, each pass|fail — matched by item id,
// with the LABEL + POSITION snapshot taken server-side from the type). A
// pass_fail mode REJECTS any provided items. Nothing is persisted on a
// validation failure.
func validateInspectionInput(input InspectionInput, tool *ToolWithTypeQualification) ([]InspectionItem, error) {
	if input.Mode != tool.InspectionMode {
		return nil, &InvalidInspectionError{Message: MsgInspectionModeMismatch}
	}
	if input.Result != InspectionResultPass && input.Result != InspectionResultFail {
		return nil, &InvalidInspectionError{Message: MsgInspectionResultInvalid}
	}
	if utf8.RuneCountInString(input.Notes) > MaxInspectionNotesRunes {
		return nil, &InvalidInspectionError{Message: MsgInspectionNotesTooLong}
	}

	if input.Mode == InspectionModePassFail {
		if len(input.Items) != 0 {
			return nil, &InvalidInspectionError{Message: MsgInspectionItemsUnexpected}
		}
		return nil, nil
	}

	// Checklist mode: build an index of the submitted items by item id; the
	// set must exactly equal the type's ordered checklist. An EMPTY type
	// checklist (a retired/misconfigured template) rejects the submit — an
	// empty-snapshot inspection must not bypass the checklist contract. An
	// oversized body is rejected up-front (fail-fast, before the map build).
	if len(tool.ChecklistItems) == 0 {
		return nil, &InvalidInspectionError{Message: MsgInspectionItemsMismatch}
	}
	if len(input.Items) > len(tool.ChecklistItems) {
		return nil, &InvalidInspectionError{Message: MsgInspectionItemsMismatch}
	}
	submitted := make(map[string]string, len(input.Items))
	for _, item := range input.Items {
		if item.Result != InspectionResultPass && item.Result != InspectionResultFail {
			return nil, &InvalidInspectionError{Message: MsgInspectionResultInvalid}
		}
		submitted[strings.TrimSpace(item.ItemID)] = item.Result
	}
	items := make([]InspectionItem, 0, len(tool.ChecklistItems))
	for _, typeItem := range tool.ChecklistItems {
		result, ok := submitted[typeItem.ID]
		if !ok {
			return nil, &InvalidInspectionError{Message: MsgInspectionItemsMismatch}
		}
		items = append(items, InspectionItem{
			ItemID:   typeItem.ID,
			Label:    typeItem.Label,
			Position: typeItem.Position,
			Result:   result,
		})
	}
	if len(items) != len(input.Items) {
		return nil, &InvalidInspectionError{Message: MsgInspectionItemsMismatch}
	}
	return items, nil
}
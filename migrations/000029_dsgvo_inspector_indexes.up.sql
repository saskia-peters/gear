-- G.E.A.R. DSGVO data-access report indexes (Story 3.3, FR-24/AD-8): the
-- per-user exports run through the module export ports — the Tool module's
-- ListInspectionsByInspector / ListReinstatementsByActor (filtering the plain
-- FK-less inspector_id / actor_id columns, which the 000027 per-tool index set
-- does not cover) and the User module's ListSessionsByUser (per-user sessions,
-- newest first — sessions already has the plain user_id_idx from 000003; this
-- composite serves the sort). These are pure read indexes — no schema change.

CREATE INDEX inspections_inspector_id_idx
    ON inspections (inspector_id, submitted_at DESC);

CREATE INDEX reinstatements_actor_id_idx
    ON reinstatements (actor_id, created_at DESC);

CREATE INDEX sessions_user_id_created_at_idx
    ON sessions (user_id, created_at DESC);
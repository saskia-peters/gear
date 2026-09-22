-- G.E.A.R. Tool-owned tool_types (Story 4.2, FR-8/FR-11/AD-7).
--
-- Most tools do not require a specific qualification — they can be inspected
-- by any Helfer*in. Making the required qualification OPTIONAL lets a tool
-- type declare "no qualification required" (a NULL required_qualification_id),
-- while a present value still cross-module-references the User qualifications
-- vocabulary and is validated through the User QualificationCatalogPort at
-- write time (AD-7/AD-11). The FK constraint stays; only NOT NULL is dropped.
ALTER TABLE tool_types
    ALTER COLUMN required_qualification_id DROP NOT NULL;
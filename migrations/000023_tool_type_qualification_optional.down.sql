-- Reverts 000023: restores NOT NULL on required_qualification_id.
ALTER TABLE tool_types
    ALTER COLUMN required_qualification_id SET NOT NULL;
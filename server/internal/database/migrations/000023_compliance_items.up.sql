-- Add per-item compliance results column to compliance_results.
-- Stores a JSON array of { schema_id, key, status, message } objects
-- reported by the agent for dconf policies.
ALTER TABLE compliance_results ADD COLUMN items_json JSONB;

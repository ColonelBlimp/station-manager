-- Reverse 0008. operator_event is purely additive (no table rebuild, no data
-- copied out of a sibling table), so the down is a clean drop of the three
-- objects the up created, in reverse dependency order.
DROP TRIGGER IF EXISTS trg_operator_event_no_update;
DROP INDEX IF EXISTS idx_operator_event_category_id;
DROP TABLE IF EXISTS operator_event;

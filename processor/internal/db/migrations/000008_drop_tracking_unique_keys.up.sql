-- Drop the two legacy UNIQUE keys left on tracking tables. Schema policy is
-- app-managed integrity (the FK constraints were stripped for the same
-- reason; DiffAndClassify/ApplyDiff dedupe inserts, and the v2 PUT path
-- validates identity collisions). These keys were carried over from the
-- legacy Knex schema and made the v2 PUT delete-then-insert destructive:
-- an insert colliding with another rule's key failed AFTER the delete
-- committed, permanently destroying the addressed rule.
--
-- Conditional via information_schema + PREPARE so the migration succeeds
-- on adopted legacy databases where the index name may differ or the key
-- may already be absent (plain DROP INDEX would error and leave the
-- migration dirty).

SET @has_idx := (SELECT COUNT(*) FROM information_schema.statistics
	WHERE table_schema = DATABASE() AND table_name = 'invasion' AND index_name = 'invasion_tracking');
SET @ddl := IF(@has_idx > 0, 'ALTER TABLE `invasion` DROP INDEX `invasion_tracking`', 'SELECT 1');
PREPARE stmt FROM @ddl;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @has_idx := (SELECT COUNT(*) FROM information_schema.statistics
	WHERE table_schema = DATABASE() AND table_name = 'lures' AND index_name = 'lure_tracking');
SET @ddl := IF(@has_idx > 0, 'ALTER TABLE `lures` DROP INDEX `lure_tracking`', 'SELECT 1');
PREPARE stmt FROM @ddl;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

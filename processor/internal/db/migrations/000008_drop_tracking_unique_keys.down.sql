-- Re-add the legacy unique keys. NOTE: this fails if duplicate rows were
-- created while the keys were absent; deduplicate manually before rolling
-- back.

SET @has_idx := (SELECT COUNT(*) FROM information_schema.statistics
	WHERE table_schema = DATABASE() AND table_name = 'invasion' AND index_name = 'invasion_tracking');
SET @ddl := IF(@has_idx = 0, 'ALTER TABLE `invasion` ADD UNIQUE KEY `invasion_tracking` (`id`,`profile_no`,`gender`,`grunt_type`)', 'SELECT 1');
PREPARE stmt FROM @ddl;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @has_idx := (SELECT COUNT(*) FROM information_schema.statistics
	WHERE table_schema = DATABASE() AND table_name = 'lures' AND index_name = 'lure_tracking');
SET @ddl := IF(@has_idx = 0, 'ALTER TABLE `lures` ADD UNIQUE KEY `lure_tracking` (`id`,`profile_no`,`lure_id`)', 'SELECT 1');
PREPARE stmt FROM @ddl;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

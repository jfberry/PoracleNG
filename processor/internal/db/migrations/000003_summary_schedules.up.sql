CREATE TABLE IF NOT EXISTS `summary_schedules` (
  `id` varchar(64) NOT NULL,
  `alert_type` varchar(32) NOT NULL,
  `active_hours` varchar(4096) NOT NULL DEFAULT '[]',
  PRIMARY KEY (`id`,`alert_type`)
) ENGINE=InnoDB;
-- No FK CASCADE: poracle's convention is explicit cleanup in
-- DeleteHumanAndTracking / SQLHumanStore.Delete (no other tracking
-- table has a CASCADE either). Adding one here would diverge from
-- the codebase pattern and surprise anyone reading the cascade-delete
-- code.

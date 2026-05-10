CREATE TABLE IF NOT EXISTS `summary_schedules` (
  `id` varchar(64) NOT NULL,
  `alert_type` varchar(32) NOT NULL,
  `active_hours` varchar(4096) NOT NULL DEFAULT '[]',
  PRIMARY KEY (`id`,`alert_type`),
  CONSTRAINT `fk_summary_schedules_human` FOREIGN KEY (`id`) REFERENCES `humans` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB;

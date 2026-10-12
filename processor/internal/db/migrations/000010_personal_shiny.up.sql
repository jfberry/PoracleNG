CREATE TABLE IF NOT EXISTS `player_accounts` (
  `uid`        INT PRIMARY KEY AUTO_INCREMENT,
  `id`         VARCHAR(255)   NOT NULL,
  `player_id`  VARBINARY(256) NOT NULL,
  `label`      VARCHAR(64)    NULL,
  `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY `uniq_id_player` (`id`, `player_id`),
  UNIQUE KEY `uniq_id_label`  (`id`, `label`)
) ENGINE=InnoDB;

ALTER TABLE `monsters`
  ADD COLUMN `personal` tinyint NOT NULL DEFAULT 0;

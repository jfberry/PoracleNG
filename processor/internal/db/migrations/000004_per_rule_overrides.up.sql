CREATE TABLE IF NOT EXISTS `user_locations` (
  `uid`        INT PRIMARY KEY AUTO_INCREMENT,
  `id`         VARCHAR(50) NOT NULL,
  `label`      VARCHAR(64) NOT NULL,
  `latitude`   float(14,10) NOT NULL,
  `longitude`  float(14,10) NOT NULL,
  `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY `uniq_id_label` (`id`, `label`)
) ENGINE=InnoDB;

ALTER TABLE `monsters`
  ADD COLUMN `override_location_label` VARCHAR(64) NULL,
  ADD COLUMN `override_areas`          TEXT NULL;

ALTER TABLE `raid`
  ADD COLUMN `override_location_label` VARCHAR(64) NULL,
  ADD COLUMN `override_areas`          TEXT NULL;

ALTER TABLE `egg`
  ADD COLUMN `override_location_label` VARCHAR(64) NULL,
  ADD COLUMN `override_areas`          TEXT NULL;

ALTER TABLE `quest`
  ADD COLUMN `override_location_label` VARCHAR(64) NULL,
  ADD COLUMN `override_areas`          TEXT NULL;

ALTER TABLE `invasion`
  ADD COLUMN `override_location_label` VARCHAR(64) NULL,
  ADD COLUMN `override_areas`          TEXT NULL;

ALTER TABLE `lures`
  ADD COLUMN `override_location_label` VARCHAR(64) NULL,
  ADD COLUMN `override_areas`          TEXT NULL;

ALTER TABLE `nests`
  ADD COLUMN `override_location_label` VARCHAR(64) NULL,
  ADD COLUMN `override_areas`          TEXT NULL;

ALTER TABLE `gym`
  ADD COLUMN `override_location_label` VARCHAR(64) NULL,
  ADD COLUMN `override_areas`          TEXT NULL;

ALTER TABLE `forts`
  ADD COLUMN `override_location_label` VARCHAR(64) NULL,
  ADD COLUMN `override_areas`          TEXT NULL;

ALTER TABLE `maxbattle`
  ADD COLUMN `override_location_label` VARCHAR(64) NULL,
  ADD COLUMN `override_areas`          TEXT NULL;

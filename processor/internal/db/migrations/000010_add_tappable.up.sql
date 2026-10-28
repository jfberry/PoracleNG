CREATE TABLE IF NOT EXISTS `tappable` (
  `uid` int(10) unsigned NOT NULL AUTO_INCREMENT,
  `id` varchar(255) NOT NULL,
  `profile_no` int(11) NOT NULL DEFAULT 1,
  `reward_type` int(11) NOT NULL DEFAULT 0,
  `reward` int(11) NOT NULL DEFAULT 0,
  `amount` int(11) NOT NULL DEFAULT 0,
  `distance` int(11) NOT NULL DEFAULT 0,
  `ping` text NOT NULL,
  `clean` tinyint(1) NOT NULL DEFAULT 0,
  `template` text NOT NULL,
  `override_location_label` VARCHAR(64) NULL,
  `override_areas` TEXT NULL,
  PRIMARY KEY (`uid`)
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

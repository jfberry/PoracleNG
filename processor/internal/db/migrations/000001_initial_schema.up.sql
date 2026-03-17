CREATE TABLE IF NOT EXISTS `humans` (
  `id` varchar(255) NOT NULL,
  `type` varchar(255) NOT NULL,
  `name` varchar(255) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `area` text NOT NULL,
  `latitude` float(14,10) NOT NULL DEFAULT 0.0000000000,
  `longitude` float(14,10) NOT NULL DEFAULT 0.0000000000,
  `fails` int(11) NOT NULL DEFAULT 0,
  `last_checked` datetime NOT NULL DEFAULT current_timestamp(),
  `language` varchar(255) DEFAULT NULL,
  `admin_disable` tinyint(1) NOT NULL DEFAULT 0,
  `disabled_date` datetime DEFAULT NULL,
  `current_profile_no` int(11) NOT NULL DEFAULT 1,
  `community_membership` text NOT NULL,
  `area_restriction` text DEFAULT NULL,
  `notes` varchar(255) NOT NULL DEFAULT '',
  `blocked_alerts` varchar(255) DEFAULT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `profiles` (
  `uid` int(10) unsigned NOT NULL AUTO_INCREMENT,
  `id` varchar(255) NOT NULL,
  `profile_no` int(11) NOT NULL DEFAULT 1,
  `name` varchar(255) NOT NULL,
  `area` text NOT NULL,
  `latitude` float(14,10) NOT NULL DEFAULT 0.0000000000,
  `longitude` float(14,10) NOT NULL DEFAULT 0.0000000000,
  `active_hours` varchar(4096) NOT NULL DEFAULT '[]',
  PRIMARY KEY (`uid`),
  UNIQUE KEY `profile_unique` (`id`,`profile_no`),
  CONSTRAINT `profiles_id_foreign` FOREIGN KEY (`id`) REFERENCES `humans` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `monsters` (
  `id` varchar(255) NOT NULL,
  `ping` text NOT NULL,
  `clean` tinyint(1) NOT NULL DEFAULT 0,
  `pokemon_id` int(11) NOT NULL,
  `distance` int(11) NOT NULL,
  `min_iv` int(11) NOT NULL,
  `max_iv` int(11) NOT NULL,
  `min_cp` int(11) NOT NULL,
  `max_cp` int(11) NOT NULL,
  `min_level` int(11) NOT NULL,
  `max_level` int(11) NOT NULL,
  `atk` int(11) NOT NULL,
  `def` int(11) NOT NULL,
  `sta` int(11) NOT NULL,
  `template` text DEFAULT NULL,
  `min_weight` int(11) NOT NULL,
  `max_weight` int(11) NOT NULL,
  `form` int(11) NOT NULL,
  `max_atk` int(11) NOT NULL,
  `max_def` int(11) NOT NULL,
  `max_sta` int(11) NOT NULL,
  `gender` int(11) NOT NULL,
  `uid` int(10) unsigned NOT NULL AUTO_INCREMENT,
  `profile_no` int(11) NOT NULL DEFAULT 1,
  `min_time` int(11) NOT NULL DEFAULT 0,
  `rarity` int(11) NOT NULL DEFAULT -1,
  `max_rarity` int(11) NOT NULL DEFAULT 6,
  `pvp_ranking_worst` int(11) NOT NULL DEFAULT 4096,
  `pvp_ranking_best` int(11) NOT NULL DEFAULT 1,
  `pvp_ranking_min_cp` int(11) NOT NULL DEFAULT 1,
  `pvp_ranking_league` int(11) NOT NULL DEFAULT 0,
  `pvp_ranking_cap` int(11) NOT NULL DEFAULT 0,
  `size` int(11) NOT NULL DEFAULT -1,
  `max_size` int(11) NOT NULL DEFAULT 5,
  PRIMARY KEY (`uid`),
  KEY `monsters_id_foreign` (`id`),
  KEY `monsters_pvp_ranking_league_pokemon_id_min_iv_index` (`pvp_ranking_league`,`pokemon_id`,`min_iv`),
  KEY `monsters_pvp_ranking_league_pokemon_id_pvp_ranking_worst_index` (`pvp_ranking_league`,`pokemon_id`,`pvp_ranking_worst`),
  CONSTRAINT `monsters_id_foreign` FOREIGN KEY (`id`) REFERENCES `humans` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `raid` (
  `id` varchar(255) NOT NULL,
  `ping` text NOT NULL,
  `clean` tinyint(1) NOT NULL DEFAULT 0,
  `pokemon_id` int(11) NOT NULL,
  `exclusive` tinyint(1) DEFAULT 0,
  `template` text DEFAULT NULL,
  `distance` int(11) NOT NULL,
  `team` int(11) NOT NULL,
  `level` int(11) NOT NULL,
  `form` int(11) NOT NULL,
  `uid` int(10) unsigned NOT NULL AUTO_INCREMENT,
  `profile_no` int(11) NOT NULL DEFAULT 1,
  `move` int(11) NOT NULL DEFAULT 9000,
  `evolution` int(11) NOT NULL DEFAULT 9000,
  `gym_id` varchar(255) DEFAULT NULL,
  `rsvp_changes` tinyint(8) NOT NULL DEFAULT 0,
  PRIMARY KEY (`uid`),
  KEY `raid_id_foreign` (`id`),
  CONSTRAINT `raid_id_foreign` FOREIGN KEY (`id`) REFERENCES `humans` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `egg` (
  `id` varchar(255) NOT NULL,
  `ping` text NOT NULL,
  `clean` tinyint(1) NOT NULL DEFAULT 0,
  `exclusive` tinyint(1) DEFAULT 0,
  `template` text DEFAULT NULL,
  `distance` int(11) NOT NULL,
  `team` int(11) NOT NULL,
  `level` int(11) NOT NULL,
  `uid` int(10) unsigned NOT NULL AUTO_INCREMENT,
  `profile_no` int(11) NOT NULL DEFAULT 1,
  `gym_id` varchar(255) DEFAULT NULL,
  `rsvp_changes` tinyint(8) NOT NULL DEFAULT 0,
  PRIMARY KEY (`uid`),
  KEY `egg_id_foreign` (`id`),
  CONSTRAINT `egg_id_foreign` FOREIGN KEY (`id`) REFERENCES `humans` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `quest` (
  `id` varchar(255) NOT NULL,
  `ping` text NOT NULL,
  `clean` tinyint(1) NOT NULL DEFAULT 0,
  `reward` int(11) NOT NULL,
  `template` text DEFAULT NULL,
  `shiny` tinyint(1) DEFAULT 0,
  `reward_type` int(11) NOT NULL,
  `distance` int(11) NOT NULL,
  `uid` int(10) unsigned NOT NULL AUTO_INCREMENT,
  `profile_no` int(11) NOT NULL DEFAULT 1,
  `form` int(11) NOT NULL DEFAULT 0,
  `amount` int(11) NOT NULL DEFAULT 0,
  PRIMARY KEY (`uid`),
  KEY `quest_id_foreign` (`id`),
  CONSTRAINT `quest_id_foreign` FOREIGN KEY (`id`) REFERENCES `humans` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `invasion` (
  `id` varchar(255) NOT NULL,
  `ping` varchar(255) NOT NULL,
  `clean` tinyint(1) NOT NULL DEFAULT 0,
  `distance` int(11) NOT NULL,
  `template` text DEFAULT NULL,
  `gender` int(11) NOT NULL,
  `grunt_type` varchar(255) NOT NULL,
  `uid` int(10) unsigned NOT NULL AUTO_INCREMENT,
  `profile_no` int(11) NOT NULL DEFAULT 1,
  PRIMARY KEY (`uid`),
  UNIQUE KEY `invasion_tracking` (`id`,`profile_no`,`gender`,`grunt_type`),
  CONSTRAINT `invasion_id_foreign` FOREIGN KEY (`id`) REFERENCES `humans` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `lures` (
  `uid` int(10) unsigned NOT NULL AUTO_INCREMENT,
  `id` varchar(255) NOT NULL,
  `profile_no` int(11) NOT NULL DEFAULT 1,
  `ping` varchar(255) NOT NULL,
  `clean` tinyint(1) NOT NULL DEFAULT 0,
  `distance` int(11) NOT NULL,
  `template` text DEFAULT NULL,
  `lure_id` int(11) NOT NULL,
  PRIMARY KEY (`uid`),
  UNIQUE KEY `lure_tracking` (`id`,`profile_no`,`lure_id`),
  CONSTRAINT `lures_id_foreign` FOREIGN KEY (`id`) REFERENCES `humans` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `nests` (
  `uid` int(10) unsigned NOT NULL AUTO_INCREMENT,
  `id` varchar(255) NOT NULL,
  `profile_no` int(11) NOT NULL DEFAULT 1,
  `ping` varchar(255) NOT NULL,
  `clean` tinyint(1) NOT NULL DEFAULT 0,
  `distance` int(11) NOT NULL,
  `template` text DEFAULT NULL,
  `pokemon_id` int(11) NOT NULL,
  `min_spawn_avg` int(11) NOT NULL,
  `form` int(11) NOT NULL,
  PRIMARY KEY (`uid`),
  KEY `nests_id_foreign` (`id`),
  CONSTRAINT `nests_id_foreign` FOREIGN KEY (`id`) REFERENCES `humans` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `gym` (
  `uid` int(10) unsigned NOT NULL AUTO_INCREMENT,
  `id` varchar(255) NOT NULL,
  `profile_no` int(11) NOT NULL DEFAULT 1,
  `ping` varchar(255) NOT NULL,
  `clean` tinyint(1) NOT NULL DEFAULT 0,
  `distance` int(11) NOT NULL,
  `template` text NOT NULL,
  `team` int(11) NOT NULL,
  `slot_changes` tinyint(1) NOT NULL,
  `gym_id` varchar(255) DEFAULT NULL,
  `battle_changes` tinyint(1) NOT NULL DEFAULT 0,
  PRIMARY KEY (`uid`),
  KEY `gym_id_foreign` (`id`),
  CONSTRAINT `gym_id_foreign` FOREIGN KEY (`id`) REFERENCES `humans` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `forts` (
  `uid` int(10) unsigned NOT NULL AUTO_INCREMENT,
  `id` varchar(255) NOT NULL,
  `profile_no` int(11) NOT NULL DEFAULT 1,
  `ping` varchar(255) NOT NULL,
  `distance` int(11) NOT NULL,
  `template` text NOT NULL,
  `fort_type` varchar(255) NOT NULL DEFAULT 'everything',
  `include_empty` tinyint(1) NOT NULL DEFAULT 1,
  `change_types` varchar(255) NOT NULL DEFAULT '[]',
  PRIMARY KEY (`uid`),
  KEY `forts_id_foreign` (`id`),
  CONSTRAINT `forts_id_foreign` FOREIGN KEY (`id`) REFERENCES `humans` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `weather` (
  `id` varchar(255) NOT NULL,
  `ping` text NOT NULL,
  `template` int(11) NOT NULL,
  `clean` tinyint(1) NOT NULL DEFAULT 0,
  `condition` int(11) NOT NULL,
  `cell` varchar(255) NOT NULL,
  `uid` int(10) unsigned NOT NULL AUTO_INCREMENT,
  `profile_no` int(11) NOT NULL DEFAULT 1,
  PRIMARY KEY (`uid`),
  UNIQUE KEY `weather_tracking` (`id`,`profile_no`,`condition`,`cell`),
  CONSTRAINT `weather_id_foreign` FOREIGN KEY (`id`) REFERENCES `humans` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `maxbattle` (
  `uid` int(10) unsigned NOT NULL AUTO_INCREMENT,
  `id` varchar(255) NOT NULL,
  `profile_no` int(11) NOT NULL DEFAULT 1,
  `pokemon_id` int(11) NOT NULL DEFAULT 9000,
  `gmax` tinyint(1) NOT NULL DEFAULT 0,
  `level` int(11) NOT NULL DEFAULT 9000,
  `form` int(11) NOT NULL DEFAULT 0,
  `move` int(11) NOT NULL DEFAULT 9000,
  `evolution` int(11) NOT NULL DEFAULT 9000,
  `distance` int(11) NOT NULL DEFAULT 0,
  `station_id` varchar(255) DEFAULT NULL,
  `ping` text NOT NULL,
  `clean` tinyint(1) NOT NULL DEFAULT 0,
  `template` text NOT NULL,
  PRIMARY KEY (`uid`),
  KEY `maxbattle_id_foreign` (`id`),
  CONSTRAINT `maxbattle_id_foreign` FOREIGN KEY (`id`) REFERENCES `humans` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

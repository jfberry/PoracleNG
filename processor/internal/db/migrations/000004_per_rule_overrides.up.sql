CREATE TABLE user_locations (
  uid        INT PRIMARY KEY AUTO_INCREMENT,
  id         VARCHAR(50) NOT NULL,
  label      VARCHAR(64) NOT NULL,
  latitude   DOUBLE NOT NULL,
  longitude  DOUBLE NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_id_label (id, label),
  INDEX idx_id (id)
);

ALTER TABLE monsters
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;

ALTER TABLE raid
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;

ALTER TABLE egg
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;

ALTER TABLE quest
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;

ALTER TABLE invasion
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;

ALTER TABLE lures
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;

ALTER TABLE nests
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;

ALTER TABLE gym
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;

ALTER TABLE forts
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;

ALTER TABLE maxbattle
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;

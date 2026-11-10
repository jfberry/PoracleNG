-- Move humans off profile 0, which never existed.
--
-- SQLHumanStore.Create named current_profile_no in its INSERT and stored the
-- caller's unset 0, so the column default (1) never applied. !poracle, Discord
-- role reconciliation, !channel add, !webhook add and Telegram channel
-- registration all created humans on profile 0, while CreateDefaultProfile
-- always inserts profile 1. Bot commands saved rules to the current profile
-- (0), so they worked; anything that addressed profile 1 explicitly (e.g.
-- PoracleWeb) saved rules that never matched. AddProfile numbers new profiles
-- from 1, so these users could add profile 2 but never switch back to 0.
--
-- Profile 0 here is the default profile with the wrong number, so it is
-- folded into profile 1. A human that has a real profiles row for 0 is left
-- alone throughout — its profile 0 is a genuine, separate profile.
--
-- Collations: legacy Knex databases skip 000001's database-default collation,
-- so tables created later (notably maxbattle) can differ from humans/profiles
-- and from each other; comparing their ids directly fails with "Illegal mix of
-- collations". Every id is therefore normalised to utf8mb4_unicode_ci into a
-- temporary table keyed on it, and each comparison converts only the
-- persistent table's side, so the temporary table's primary key still serves
-- the lookup. Persistent columns are not altered. The temporary tables declare
-- id explicitly: older MariaDB (10.5, 10.6) types a CONVERT(... USING) result
-- as TEXT in CREATE ... SELECT, which cannot be a key without a length.
--
-- Safe to re-run: it is idempotent (once it succeeds nothing is left on a
-- fake profile 0), and all data changes commit in one transaction, so a
-- failure part-way changes nothing. RunMigrations relies on this to retry it
-- when left dirty.
--
-- Not reversible: the down migration is a no-op.

-- Temporary tables are built before the transaction: CREATE TEMPORARY TABLE
-- inside one is rejected under some GTID replication settings.
CREATE TEMPORARY TABLE tmp_real_profile_zero (id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL, PRIMARY KEY (id))
	SELECT DISTINCT CONVERT(id USING utf8mb4) COLLATE utf8mb4_unicode_ci AS id FROM profiles WHERE profile_no = 0;

-- Humans that already have rules on profile 1, captured before anything moves.
CREATE TEMPORARY TABLE tmp_has_profile_one_rules (id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL, PRIMARY KEY (id))
	SELECT DISTINCT id FROM (
	SELECT CONVERT(id USING utf8mb4) COLLATE utf8mb4_unicode_ci AS id FROM `monsters` WHERE profile_no = 1
	UNION SELECT CONVERT(id USING utf8mb4) COLLATE utf8mb4_unicode_ci AS id FROM `raid` WHERE profile_no = 1
	UNION SELECT CONVERT(id USING utf8mb4) COLLATE utf8mb4_unicode_ci AS id FROM `egg` WHERE profile_no = 1
	UNION SELECT CONVERT(id USING utf8mb4) COLLATE utf8mb4_unicode_ci AS id FROM `quest` WHERE profile_no = 1
	UNION SELECT CONVERT(id USING utf8mb4) COLLATE utf8mb4_unicode_ci AS id FROM `invasion` WHERE profile_no = 1
	UNION SELECT CONVERT(id USING utf8mb4) COLLATE utf8mb4_unicode_ci AS id FROM `lures` WHERE profile_no = 1
	UNION SELECT CONVERT(id USING utf8mb4) COLLATE utf8mb4_unicode_ci AS id FROM `nests` WHERE profile_no = 1
	UNION SELECT CONVERT(id USING utf8mb4) COLLATE utf8mb4_unicode_ci AS id FROM `gym` WHERE profile_no = 1
	UNION SELECT CONVERT(id USING utf8mb4) COLLATE utf8mb4_unicode_ci AS id FROM `forts` WHERE profile_no = 1
	UNION SELECT CONVERT(id USING utf8mb4) COLLATE utf8mb4_unicode_ci AS id FROM `weather` WHERE profile_no = 1
	UNION SELECT CONVERT(id USING utf8mb4) COLLATE utf8mb4_unicode_ci AS id FROM `maxbattle` WHERE profile_no = 1
	) x;

-- Humans on the fake profile 0, with the live area/location to fold into 1.
CREATE TEMPORARY TABLE tmp_fold_humans (id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL, PRIMARY KEY (id))
	SELECT CONVERT(h.id USING utf8mb4) COLLATE utf8mb4_unicode_ci AS id, h.name, h.area, h.latitude, h.longitude
	FROM humans h
	WHERE h.current_profile_no = 0
	  AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(h.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);

-- Humans that switched away from the fake profile 0 and already have profile-1
-- rules (see step 3).
CREATE TEMPORARY TABLE tmp_abandoned_profile_zero (id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL, PRIMARY KEY (id))
	SELECT CONVERT(h.id USING utf8mb4) COLLATE utf8mb4_unicode_ci AS id
	FROM humans h
	WHERE h.current_profile_no <> 0
	  AND EXISTS (SELECT 1 FROM tmp_has_profile_one_rules p1 WHERE p1.id = CONVERT(h.id USING utf8mb4) COLLATE utf8mb4_unicode_ci)
	  AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(h.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);

START TRANSACTION;

-- 1. A profile-0 human must have a profile 1 to move to (normally it does;
--    CreateDefaultProfile may have failed).
INSERT IGNORE INTO profiles (id, profile_no, name, area, latitude, longitude)
	SELECT id, 1, name, area, latitude, longitude FROM tmp_fold_humans;

-- 2. While on profile 0 every area/location write reached humans only (there
--    was no profile-0 row to update), so profile 1 still holds its creation
--    values. Copy the live ones across, but only for humans currently on 0:
--    for anyone else humans holds a different profile's values.
UPDATE profiles p
	JOIN tmp_fold_humans h ON h.id = CONVERT(p.id USING utf8mb4) COLLATE utf8mb4_unicode_ci
	SET p.area = h.area, p.latitude = h.latitude, p.longitude = h.longitude
	WHERE p.profile_no = 1;

-- 3. A human that has switched away from 0 and already has profile-1 rules
--    rebuilt its default profile after finding profile 1 empty (it could not
--    switch back to 0). Its profile-0 rules are abandoned — they have not fired
--    since the switch and the user cannot see them — so drop them rather than
--    resurrect rules the user chose not to recreate. A human still ON 0 keeps
--    them: there profile 0 is the live set and any profile-1 rules are a
--    client's never-matched attempt at the same default profile.
DELETE t FROM `monsters` t
	WHERE t.profile_no = 0
	  AND EXISTS (SELECT 1 FROM tmp_abandoned_profile_zero a WHERE a.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `raid` t
	WHERE t.profile_no = 0
	  AND EXISTS (SELECT 1 FROM tmp_abandoned_profile_zero a WHERE a.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `egg` t
	WHERE t.profile_no = 0
	  AND EXISTS (SELECT 1 FROM tmp_abandoned_profile_zero a WHERE a.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `quest` t
	WHERE t.profile_no = 0
	  AND EXISTS (SELECT 1 FROM tmp_abandoned_profile_zero a WHERE a.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `invasion` t
	WHERE t.profile_no = 0
	  AND EXISTS (SELECT 1 FROM tmp_abandoned_profile_zero a WHERE a.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `lures` t
	WHERE t.profile_no = 0
	  AND EXISTS (SELECT 1 FROM tmp_abandoned_profile_zero a WHERE a.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `nests` t
	WHERE t.profile_no = 0
	  AND EXISTS (SELECT 1 FROM tmp_abandoned_profile_zero a WHERE a.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `gym` t
	WHERE t.profile_no = 0
	  AND EXISTS (SELECT 1 FROM tmp_abandoned_profile_zero a WHERE a.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `forts` t
	WHERE t.profile_no = 0
	  AND EXISTS (SELECT 1 FROM tmp_abandoned_profile_zero a WHERE a.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `weather` t
	WHERE t.profile_no = 0
	  AND EXISTS (SELECT 1 FROM tmp_abandoned_profile_zero a WHERE a.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `maxbattle` t
	WHERE t.profile_no = 0
	  AND EXISTS (SELECT 1 FROM tmp_abandoned_profile_zero a WHERE a.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);

-- 4. Move the remaining profile-0 rules to profile 1: every rule of a human on
--    0, and those of a switched-away human whose profile 1 is empty (restoring
--    the default profile it lost). UPDATE IGNORE
--    skips a row that would collide with an identical profile-1 rule on a
--    database still carrying a legacy unique key (weather_tracking, or an
--    invasion/lures key 000008 did not recognise); the DELETE then drops those
--    leftovers, which duplicate a rule already on profile 1.
UPDATE IGNORE `monsters` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `monsters` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);

UPDATE IGNORE `raid` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `raid` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);

UPDATE IGNORE `egg` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `egg` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);

UPDATE IGNORE `quest` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `quest` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);

UPDATE IGNORE `invasion` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `invasion` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);

UPDATE IGNORE `lures` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `lures` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);

UPDATE IGNORE `nests` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `nests` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);

UPDATE IGNORE `gym` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `gym` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);

UPDATE IGNORE `forts` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `forts` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);

UPDATE IGNORE `weather` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `weather` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);

UPDATE IGNORE `maxbattle` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);
DELETE t FROM `maxbattle` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = CONVERT(t.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);

-- 5. Finally point the humans at profile 1 (after step 2, which selects on 0).
UPDATE humans h SET h.current_profile_no = 1
	WHERE h.current_profile_no = 0
	  AND EXISTS (SELECT 1 FROM tmp_fold_humans f WHERE f.id = CONVERT(h.id USING utf8mb4) COLLATE utf8mb4_unicode_ci);

COMMIT;

DROP TEMPORARY TABLE tmp_abandoned_profile_zero;
DROP TEMPORARY TABLE tmp_fold_humans;
DROP TEMPORARY TABLE tmp_has_profile_one_rules;
DROP TEMPORARY TABLE tmp_real_profile_zero;

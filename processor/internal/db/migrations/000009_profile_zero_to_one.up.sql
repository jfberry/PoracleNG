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
-- Not reversible: the down migration is a no-op.

CREATE TEMPORARY TABLE tmp_real_profile_zero (PRIMARY KEY (id))
	SELECT DISTINCT id FROM profiles WHERE profile_no = 0;

-- 1. A profile-0 human must have a profile 1 to move to (normally it does;
--    CreateDefaultProfile may have failed).
INSERT IGNORE INTO profiles (id, profile_no, name, area, latitude, longitude)
	SELECT h.id, 1, h.name, h.area, h.latitude, h.longitude
	FROM humans h
	WHERE h.current_profile_no = 0
	  AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = h.id);

-- 2. While on profile 0 every area/location write reached humans only (there
--    was no profile-0 row to update), so profile 1 still holds its creation
--    values. Copy the live ones across, but only for humans currently on 0:
--    for anyone else humans holds a different profile's values.
UPDATE profiles p
	JOIN humans h ON h.id = p.id
	SET p.area = h.area, p.latitude = h.latitude, p.longitude = h.longitude
	WHERE p.profile_no = 1 AND h.current_profile_no = 0
	  AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = h.id);

-- 3. Move profile-0 rules to profile 1, for every human (one that has since
--    switched away still has its default profile's rules on 0). UPDATE IGNORE
--    skips a row that would collide with an identical profile-1 rule on a
--    database still carrying a legacy unique key (weather_tracking, or an
--    invasion/lures key 000008 did not recognise); the DELETE then drops those
--    leftovers, which duplicate a rule already on profile 1.
UPDATE IGNORE `monsters` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);
DELETE t FROM `monsters` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);

UPDATE IGNORE `raid` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);
DELETE t FROM `raid` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);

UPDATE IGNORE `egg` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);
DELETE t FROM `egg` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);

UPDATE IGNORE `quest` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);
DELETE t FROM `quest` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);

UPDATE IGNORE `invasion` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);
DELETE t FROM `invasion` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);

UPDATE IGNORE `lures` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);
DELETE t FROM `lures` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);

UPDATE IGNORE `nests` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);
DELETE t FROM `nests` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);

UPDATE IGNORE `gym` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);
DELETE t FROM `gym` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);

UPDATE IGNORE `forts` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);
DELETE t FROM `forts` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);

UPDATE IGNORE `weather` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);
DELETE t FROM `weather` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);

UPDATE IGNORE `maxbattle` t SET t.profile_no = 1
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);
DELETE t FROM `maxbattle` t
	WHERE t.profile_no = 0 AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = t.id);

-- 4. Finally point the humans at profile 1 (after step 2, which selects on 0).
UPDATE humans h SET h.current_profile_no = 1
	WHERE h.current_profile_no = 0
	  AND NOT EXISTS (SELECT 1 FROM tmp_real_profile_zero r WHERE r.id = h.id);

DROP TEMPORARY TABLE tmp_real_profile_zero;

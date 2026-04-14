package db

import "reflect"

// DiffTracking compares two tracking structs using `diff` struct tags.
//
// Tag values:
//   - diff:"-"      skip (uid, id, profile_no — always same or irrelevant)
//   - diff:"match"  match key — if different, rows aren't related (noMatch=true)
//   - diff:"update" updatable field — a single updatable diff triggers an update
//   - (no tag)      regular field — any diff here means new insert
//
// An update is only triggered when exactly one field differs and that field is
// updatable. Multiple diffs (even if all updatable) create a new row, matching
// PoracleJS behavior where e.g. iv95+d500 and iv90+d1000 are separate rules.
//
// Both existing and toInsert must be pointers to the same struct type.
func DiffTracking(existing, toInsert any) (noMatch, isDuplicate bool, existingUID int64, isUpdate bool) {
	ev := reflect.ValueOf(existing).Elem()
	iv := reflect.ValueOf(toInsert).Elem()
	et := ev.Type()

	var uid int64
	totalDiffs, nonUpdatableDiffs := 0, 0

	for i := 0; i < et.NumField(); i++ {
		field := et.Field(i)
		tag := field.Tag.Get("diff")

		switch tag {
		case "-":
			if field.Tag.Get("db") == "uid" {
				uid = ev.Field(i).Int()
			}
		case "match":
			if !reflect.DeepEqual(ev.Field(i).Interface(), iv.Field(i).Interface()) {
				return true, false, 0, false
			}
		case "update":
			if !reflect.DeepEqual(ev.Field(i).Interface(), iv.Field(i).Interface()) {
				totalDiffs++
			}
		default:
			if !reflect.DeepEqual(ev.Field(i).Interface(), iv.Field(i).Interface()) {
				totalDiffs++
				nonUpdatableDiffs++
			}
		}
	}

	if totalDiffs == 0 {
		return false, true, 0, false // duplicate — all fields match
	}
	if totalDiffs == 1 && nonUpdatableDiffs == 0 {
		return false, false, uid, true // update — exactly one updatable field differs
	}
	return false, false, 0, false // new insert
}

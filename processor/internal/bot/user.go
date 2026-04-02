package bot

import "github.com/pokemon/poracleng/processor/internal/store"

// LookupUserStateFromStore loads basic user info via the HumanStore interface.
func LookupUserStateFromStore(hs store.HumanStore, userID, defaultLocale string) (lang string, profileNo int, hasLocation, hasArea, isRegistered bool) {
	lang = defaultLocale
	profileNo = 1

	h, err := hs.Get(userID)
	if err != nil || h == nil {
		return
	}

	// Admin-disabled users are treated as unregistered (role removed, banned).
	// enabled=0 (!stop) just pauses alerts — user is still registered and can
	// run commands like !start, !tracked, !area, etc.
	if h.AdminDisable {
		return
	}

	isRegistered = true
	if h.Language != "" {
		lang = h.Language
	}
	profileNo = h.CurrentProfileNo
	hasLocation = h.Latitude != 0 || h.Longitude != 0
	hasArea = len(h.Area) > 0
	return
}

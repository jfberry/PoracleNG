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

	isRegistered = true
	if h.Language != "" {
		lang = h.Language
	}
	profileNo = h.CurrentProfileNo
	hasLocation = h.Latitude != 0 || h.Longitude != 0
	hasArea = len(h.Area) > 0
	return
}

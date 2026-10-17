package db

// Personal bitmask on monsters.personal: which per-player conditions a rule
// requires. All set bits must hold.
//
//	1 = shiny for one of the human's linked accounts
//	2 = special (event) background for one of the human's linked accounts
//	4, 8 = reserved for per-player XXL / XXS (Golbat#424)
const (
	PersonalShiny      = 1
	PersonalBackground = 2
	// PersonalKnownMask is every bit this binary can evaluate. Rules with any
	// other bit set never match, so a newer binary's rule is not silently
	// widened by an older one.
	PersonalKnownMask = PersonalShiny | PersonalBackground
)

// IsPersonalShiny reports whether the rule requires a personal shiny.
func IsPersonalShiny(personal int) bool { return personal&PersonalShiny != 0 }

// IsPersonalBackground reports whether the rule requires a personal background.
func IsPersonalBackground(personal int) bool { return personal&PersonalBackground != 0 }

// PackPersonal builds the bitmask from its booleans.
func PackPersonal(shiny, background bool) int {
	v := 0
	if shiny {
		v |= PersonalShiny
	}
	if background {
		v |= PersonalBackground
	}
	return v
}

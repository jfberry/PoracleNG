package delivery

// TargetClass maps a delivery Job.Type to the coarse destination class used by
// snapshots (Snapshot.TargetType) and button applies_to checks: "dm",
// "channel", "webhook", or "" for an unknown type. This is the single source
// of truth previously duplicated as snapshotTargetType (cmd/processor) and
// deliveryTargetType (internal/dts); both now delegate here. telegram:topic is
// included as a channel class (it was missing from both duplicates).
func TargetClass(jobType string) string {
	switch jobType {
	case "discord:user", "telegram:user", "api:user":
		return "dm"
	case "discord:channel", "discord:thread", "telegram:group", "telegram:channel", "telegram:topic", "api:channel":
		return "channel"
	case "webhook":
		return "webhook"
	default:
		return ""
	}
}

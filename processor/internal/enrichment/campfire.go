package enrichment

import (
	"encoding/base64"
	"fmt"
	"net/url"

	"github.com/google/uuid"
)

const (
	campfireDefaultImage = "https://lh3.googleusercontent.com/2hDpMTpBQ2VblLhRrjUBFPFqCL7gOX0OMjaQxTHdAMbMSoyFIdHFe8VDjeKMXsRXALo"
	campfireBaseURL      = "https://campfire.onelink.me/eBr8"
)

// CampfireURL builds a Niantic Campfire deep link for a gym location.
// Matches the URL format from PoracleJS campfireLink.js.
func CampfireURL(lat, lon float64, gymID, gymName, gymImageURL string) string {
	// Marker ID: use gym_id if available, otherwise random UUID
	markerID := gymID
	if markerID == "" {
		markerID = uuid.New().String()
	}

	// Build deep link data and base64 encode
	deepLinkData := fmt.Sprintf("r=map&lat=%f&lng=%f&m=%s&g=PGO", lat, lon, markerID)
	encodedData := base64.StdEncoding.EncodeToString([]byte(deepLinkData))

	// URL-encode title and image
	title := gymName
	if title == "" {
		title = "Gym"
	}
	image := gymImageURL
	if image == "" {
		image = campfireDefaultImage
	}

	return fmt.Sprintf("%s?af_dp=campfire://&af_force_deeplink=true&deep_link_sub1=%s&af_og_title=%s&af_og_description=%%20&af_og_image=%s",
		campfireBaseURL,
		url.QueryEscape(encodedData),
		url.QueryEscape(title),
		url.QueryEscape(image),
	)
}

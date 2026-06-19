package geocoding

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
)

// Intersection holds the configuration for intersection lookups
type Intersection struct {
	Usernames []string
}

// NewIntersection creates a new instance for fetching intersections
func NewIntersection(usernames []string) *Intersection {
	return &Intersection{
		Usernames: usernames,
	}
}

// GetIntersection picks a random user and fetches the intersection, returning an empty string on failure
func (i *Intersection) GetIntersection(lat, lon float64) string {
	if len(i.Usernames) == 0 {
		log.Debugf("GeoNames intersection: no usernames configured, skipping")
		return ""
	}

	randSource := rand.NewSource(time.Now().UnixNano())
	r := rand.New(randSource)
	username := i.Usernames[r.Intn(len(i.Usernames))]

	url := fmt.Sprintf("http://api.geonames.org/findNearestIntersectionOSMJSON?lat=%f&lng=%f&username=%s", lat, lon, username)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Debugf("GeoNames request failed: %v", err)
		return "No intersection"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Debugf("GeoNames API returned status %d", resp.StatusCode)
		return "No intersection"
	}

	var result struct {
		Intersection struct {
			Street1 string `json:"street1"`
			Street2 string `json:"street2"`
		} `json:"intersection"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Debugf("Failed to decode GeoNames response: %v", err)
		return "No intersection"
	}

	if result.Intersection.Street1 != "" && result.Intersection.Street2 != "" {
		return fmt.Sprintf("%s & %s", result.Intersection.Street1, result.Intersection.Street2)
	}

	return "No intersection"
}
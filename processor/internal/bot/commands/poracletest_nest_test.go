package commands

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/store"
)

// The bundled testdata had no nest entry although !poracle-test accepts the
// type (#284). Nest webhooks use lat/lon (not latitude/longitude) and carry a
// polygon and a reset_time, so the "current location" move and timestamp
// freshening have to handle them too: the nest, and its polygon, move to the
// tester, and reset_time becomes recent so the 7-day expiry is in the future.
func TestPoracleTest_NestMovesToUserAndFreshensReset(t *testing.T) {
	ctx, fake := poracleTestCmdCtx(t)
	humans := ctx.Humans.(*store.MockHumanStore)
	h, _ := humans.Get("user1")
	h.Latitude, h.Longitude = 40.0, -3.0
	humans.AddHuman(h)

	replies := runPoracleTest(t, ctx, "nest", "riverside")
	if len(replies) == 0 || replies[0].React != "✅" {
		t.Fatalf("nest,riverside: replies = %+v, want ✅", replies)
	}
	if fake.gotWebhookType != "nest" {
		t.Fatalf("webhook type = %q, want nest", fake.gotWebhookType)
	}

	var hook struct {
		Lat       float64 `json:"lat"`
		Lon       float64 `json:"lon"`
		PolyPath  string  `json:"poly_path"`
		ResetTime int64   `json:"reset_time"`
	}
	if err := json.Unmarshal(fake.gotRaw, &hook); err != nil {
		t.Fatal(err)
	}
	if hook.Lat != 40.0 || hook.Lon != -3.0 {
		t.Errorf("nest at %v,%v, want moved to the user (40,-3)", hook.Lat, hook.Lon)
	}

	var poly [][][2]float64
	if err := json.Unmarshal([]byte(hook.PolyPath), &poly); err != nil {
		t.Fatalf("poly_path: %v (%q)", err, hook.PolyPath)
	}
	// The sample polygon surrounds its centre point; after the move it must
	// surround the user's location instead.
	minLat, maxLat, minLon, maxLon := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
	for _, pt := range poly[0] {
		minLat, maxLat = math.Min(minLat, pt[0]), math.Max(maxLat, pt[0])
		minLon, maxLon = math.Min(minLon, pt[1]), math.Max(maxLon, pt[1])
	}
	if !(minLat < 40.0 && 40.0 < maxLat && minLon < -3.0 && -3.0 < maxLon) {
		t.Errorf("polygon %v..%v / %v..%v does not surround the moved nest", minLat, maxLat, minLon, maxLon)
	}

	age := time.Now().Unix() - hook.ResetTime
	if age < 0 || age > 24*3600 {
		t.Errorf("reset_time %d is %ds old, want within the last day", hook.ResetTime, age)
	}
}

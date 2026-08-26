package enrichment

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// latitude/longitude are declared in the API's commonFields (Preferred: true),
// so the template editor advertises {{latitude}}/{{longitude}} on every DTS
// type — fort-update included.
//
// Every other alert type gets them for free: their Golbat webhooks carry flat
// `latitude`/`longitude`, which the LayeredView's webhook layer resolves even
// though enrichment never sets them. The fort_update webhook is nested
// (new/old snapshots, each with `location.lat`/`lon`) and has no flat field,
// so unless enrichment sets them explicitly they render empty. See issue #200.
func TestFortUpdate_ExposesLatitudeLongitude(t *testing.T) {
	e := &Enricher{WeatherProvider: &mockWeather{}, TimeLayout: "15:04:05"}
	fort := &webhook.FortWebhook{
		ChangeType: "edit",
		EditTypes:  []string{"name"},
		New: &webhook.FortSnapshot{
			ID:       "fort123",
			FortType: "pokestop",
			Name:     "New Name",
			Location: webhook.FortLocation{Lat: 52.5, Lon: 13.4},
		},
	}

	m, _ := e.FortUpdate(52.5, 13.4, "fort123", fort, TileModeSkip)

	lat, ok := m["latitude"].(float64)
	if !ok {
		t.Fatalf("latitude missing or not a float64: %#v", m["latitude"])
	}
	if lat != 52.5 {
		t.Errorf("latitude = %v, want 52.5", lat)
	}
	lon, ok := m["longitude"].(float64)
	if !ok {
		t.Fatalf("longitude missing or not a float64: %#v", m["longitude"])
	}
	if lon != 13.4 {
		t.Errorf("longitude = %v, want 13.4", lon)
	}
}

// A removal carries only the old snapshot. The handler resolves lat/lon via
// FortWebhook.Latitude()/Longitude() (new first, else old) and passes them in,
// so enrichment must surface whatever it was given rather than reaching into
// a snapshot itself — otherwise removals would report 0,0.
func TestFortUpdate_LatitudeFromOldSnapshotOnRemoval(t *testing.T) {
	e := &Enricher{WeatherProvider: &mockWeather{}, TimeLayout: "15:04:05"}
	fort := &webhook.FortWebhook{
		ChangeType: "removal",
		Old: &webhook.FortSnapshot{
			ID:       "fort456",
			FortType: "pokestop",
			Name:     "Gone",
			Location: webhook.FortLocation{Lat: 51.1, Lon: -0.5},
		},
	}

	// Mirrors the handler: lat/lon come from the webhook helpers.
	m, _ := e.FortUpdate(fort.Latitude(), fort.Longitude(), "fort456", fort, TileModeSkip)

	if lat, _ := m["latitude"].(float64); lat != 51.1 {
		t.Errorf("latitude = %v, want 51.1 (from the old snapshot)", m["latitude"])
	}
	if lon, _ := m["longitude"].(float64); lon != -0.5 {
		t.Errorf("longitude = %v, want -0.5 (from the old snapshot)", m["longitude"])
	}
}

// The existing old*/new* fields describe a location *change*; they must keep
// working alongside the plain fields, which describe where the alert is.
func TestFortUpdate_KeepsOldNewLocationFields(t *testing.T) {
	e := &Enricher{WeatherProvider: &mockWeather{}, TimeLayout: "15:04:05"}
	fort := &webhook.FortWebhook{
		ChangeType: "edit",
		EditTypes:  []string{"location"},
		Old:        &webhook.FortSnapshot{ID: "f", Location: webhook.FortLocation{Lat: 1.5, Lon: 2.5}},
		New:        &webhook.FortSnapshot{ID: "f", Location: webhook.FortLocation{Lat: 3.5, Lon: 4.5}},
	}

	m, _ := e.FortUpdate(3.5, 4.5, "f", fort, TileModeSkip)

	for field, want := range map[string]float64{
		"oldLatitude": 1.5, "oldLongitude": 2.5,
		"newLatitude": 3.5, "newLongitude": 4.5,
		"latitude": 3.5, "longitude": 4.5,
	} {
		if got, _ := m[field].(float64); got != want {
			t.Errorf("%s = %v, want %v", field, m[field], want)
		}
	}
}

package webhook

import (
	"encoding/json"
	"testing"
)

func TestFortWebhookUnmarshal_Edit(t *testing.T) {
	raw := `{"change_type":"edit","edit_types":["name","description"],"new":{"id":"f7430347f5c34facb838be376f16adea.16","type":"gym","name":"Journey Through Trees And Time","description":"A beautiful trip","image_url":"http://example.com/img.jpg","location":{"lat":51.268716,"lon":1.013956}},"old":{"id":"f7430347f5c34facb838be376f16adea.16","type":"gym","name":"The old walkway","description":"An ancient trip","image_url":"http://example.com/img.jpg","location":{"lat":51.268716,"lon":1.013956}}}`

	var fort FortWebhook
	if err := json.Unmarshal([]byte(raw), &fort); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if fort.ChangeType != "edit" {
		t.Errorf("ChangeType = %q, want edit", fort.ChangeType)
	}
	if len(fort.EditTypes) != 2 || fort.EditTypes[0] != "name" {
		t.Errorf("EditTypes = %v, want [name, description]", fort.EditTypes)
	}
	if fort.FortID() != "f7430347f5c34facb838be376f16adea.16" {
		t.Errorf("FortID() = %q", fort.FortID())
	}
	if fort.FortType() != "gym" {
		t.Errorf("FortType() = %q, want gym", fort.FortType())
	}
	if fort.Latitude() != 51.268716 {
		t.Errorf("Latitude() = %v", fort.Latitude())
	}
	if fort.Longitude() != 1.013956 {
		t.Errorf("Longitude() = %v", fort.Longitude())
	}
	if fort.IsEmpty() {
		t.Error("IsEmpty() = true, want false")
	}
	changes := fort.AllChangeTypes()
	if len(changes) != 3 { // name, description, edit
		t.Errorf("AllChangeTypes() = %v, want 3 entries", changes)
	}
}

func TestFortWebhookUnmarshal_New(t *testing.T) {
	raw := `{"change_type":"new","new":{"id":"0b427e88a3254eeab442d425412e4505.16","type":"pokestop","name":null,"description":null,"image_url":null,"location":{"lat":50.982116,"lon":6.933164}}}`

	var fort FortWebhook
	if err := json.Unmarshal([]byte(raw), &fort); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if fort.ChangeType != "new" {
		t.Errorf("ChangeType = %q, want new", fort.ChangeType)
	}
	if fort.FortID() != "0b427e88a3254eeab442d425412e4505.16" {
		t.Errorf("FortID() = %q", fort.FortID())
	}
	if fort.FortType() != "pokestop" {
		t.Errorf("FortType() = %q, want pokestop", fort.FortType())
	}
	if fort.Old != nil {
		t.Error("Old should be nil for new fort")
	}
	if !fort.IsEmpty() {
		t.Error("IsEmpty() = false, want true (name and description are null)")
	}
}

func TestFortWebhookUnmarshal_Removal(t *testing.T) {
	raw := `{"change_type":"removal","old":{"id":"f7430347f5c34facb838be376f16adea.16","type":"pokestop","name":"Journey Through Trees And Time","description":null,"image_url":"http://example.com/img.jpg","location":{"lat":50.975598,"lon":6.942527}}}`

	var fort FortWebhook
	if err := json.Unmarshal([]byte(raw), &fort); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if fort.ChangeType != "removal" {
		t.Errorf("ChangeType = %q, want removal", fort.ChangeType)
	}
	if fort.New != nil {
		t.Error("New should be nil for removal")
	}
	if fort.FortID() != "f7430347f5c34facb838be376f16adea.16" {
		t.Errorf("FortID() = %q", fort.FortID())
	}
	if fort.FortType() != "pokestop" {
		t.Errorf("FortType() = %q, want pokestop", fort.FortType())
	}
	if fort.Latitude() != 50.975598 {
		t.Errorf("Latitude() = %v", fort.Latitude())
	}
	changes := fort.AllChangeTypes()
	if len(changes) != 1 || changes[0] != "removal" {
		t.Errorf("AllChangeTypes() = %v, want [removal]", changes)
	}
}

func TestFortWebhookUnmarshal_EditFromEmpty(t *testing.T) {
	// edit where old has no name/description should be treated as "new"
	raw := `{"change_type":"edit","edit_types":["name"],"new":{"id":"abc.16","type":"gym","name":"New Name","description":null,"image_url":null,"location":{"lat":51.0,"lon":7.0}},"old":{"id":"abc.16","type":"gym","name":"","description":"","image_url":null,"location":{"lat":51.0,"lon":7.0}}}`

	var fort FortWebhook
	if err := json.Unmarshal([]byte(raw), &fort); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	changes := fort.AllChangeTypes()
	// Should contain "name" from edit_types and "new" (not "edit") because old was empty
	hasNew := false
	for _, c := range changes {
		if c == "new" {
			hasNew = true
		}
		if c == "edit" {
			t.Error("AllChangeTypes() should not contain 'edit' when old is empty")
		}
	}
	if !hasNew {
		t.Errorf("AllChangeTypes() should contain 'new' when old is empty, got %v", changes)
	}
}

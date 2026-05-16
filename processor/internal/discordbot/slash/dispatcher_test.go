package slash

import "testing"

func TestNewDispatcherStoresConfig(t *testing.T) {
	cfg := Config{Enabled: true, Global: true}
	d := NewDispatcher(cfg)
	if !d.cfg.Enabled {
		t.Error("cfg.Enabled lost")
	}
}

func TestHandleCommandSkipsWhenNoCommand(t *testing.T) {
	d := NewDispatcher(Config{})
	// No registration; HandleCommand should return without panic
	d.HandleCommand(nil, nil)
}

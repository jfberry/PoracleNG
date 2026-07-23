package config

import "testing"

// TestAPIDeliveryDefaults pins the zero-value defaults applied to an
// enabled [api_delivery] block by applyAPIDeliveryDefaults.
func TestAPIDeliveryDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.APIDelivery.Enabled = true
	cfg.APIDelivery.Endpoint = "https://example.test/hook"
	applyAPIDeliveryDefaults(cfg)
	if cfg.APIDelivery.SecretHeader != "X-Poracle-Secret" {
		t.Errorf("SecretHeader default = %q, want X-Poracle-Secret", cfg.APIDelivery.SecretHeader)
	}
	if cfg.APIDelivery.TimeoutMs != 10000 {
		t.Errorf("TimeoutMs default = %d, want 10000", cfg.APIDelivery.TimeoutMs)
	}
	if cfg.APIDelivery.MaxRetries != 3 {
		t.Errorf("MaxRetries default = %d, want 3", cfg.APIDelivery.MaxRetries)
	}
	if cfg.APIDelivery.Concurrency != 4 {
		t.Errorf("Concurrency default = %d, want 4", cfg.APIDelivery.Concurrency)
	}
	if cfg.APIDelivery.Template != "diadem" {
		t.Errorf("Template default = %q, want diadem", cfg.APIDelivery.Template)
	}
}

// TestAPIDeliveryValidation pins validateAPIDelivery's sole rule: an
// enabled [api_delivery] block requires a non-empty endpoint. Disabled
// blocks are never validated, regardless of endpoint.
func TestAPIDeliveryValidation(t *testing.T) {
	cfg := &Config{}
	cfg.APIDelivery.Enabled = true
	cfg.APIDelivery.Endpoint = ""
	if err := validateAPIDelivery(cfg); err == nil {
		t.Fatal("expected error when enabled with empty endpoint, got nil")
	}
	cfg.APIDelivery.Endpoint = "https://example.test/hook"
	if err := validateAPIDelivery(cfg); err != nil {
		t.Fatalf("unexpected error with valid config: %v", err)
	}
	cfg.APIDelivery.Enabled = false
	cfg.APIDelivery.Endpoint = ""
	if err := validateAPIDelivery(cfg); err != nil {
		t.Fatalf("disabled+empty endpoint should be valid, got: %v", err)
	}
}

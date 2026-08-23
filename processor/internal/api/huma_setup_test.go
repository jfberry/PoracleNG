package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
)

// TestProblemJSONErrorModelSerialises asserts that the huma surface now uses
// huma's default RFC 9457 problem+json error model: a numeric "status", a
// "title", an "errors" array, and NOT the legacy {"status":"error"} envelope.
func TestProblemJSONErrorModelSerialises(t *testing.T) {
	r, _ := buildSchemaTestAPI(t)

	// Send a string where an integer is expected; huma produces a 422.
	req := httptest.NewRequest(http.MethodPost, "/api/schema-test",
		strings.NewReader(`{"value":"not-an-int"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}

	var got map[string]any
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}

	// status must be a JSON number (422), not the legacy string "error".
	statusNum, ok := got["status"].(float64)
	if !ok {
		t.Fatalf("status field = %v (%T), want JSON number 422", got["status"], got["status"])
	}
	if statusNum != float64(http.StatusUnprocessableEntity) {
		t.Errorf("status = %v, want %d", statusNum, http.StatusUnprocessableEntity)
	}
	if got["status"] == "error" {
		t.Errorf("body must not use legacy {status:\"error\"} envelope: %v", got)
	}
	if _, hasTitle := got["title"]; !hasTitle {
		t.Errorf("problem+json body must contain a \"title\" field: %v", got)
	}
	if _, hasErrors := got["errors"]; !hasErrors {
		t.Errorf("problem+json body must contain an \"errors\" array: %v", got)
	}
}

func TestPublicDocsUnauthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	apiGroup := r.Group("/api")
	apiGroup.Use(RequireSecretGin("topsecret")) // gate /api
	_ = NewHumaAPI(r, apiGroup, "test-version") // mounts docs on r (public)

	for _, path := range []string{"/openapi.json", "/docs"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s unauthenticated = %d, want 200", path, w.Code)
		}
	}
}

// schemaTestInput / schemaTestOutput are the types used by the three
// $schema-leak and validation-message tests below.
type schemaTestInput struct {
	Body struct {
		Value int `json:"value"`
	}
}
type schemaTestOutput struct {
	Body struct {
		Status string `json:"status"`
		Value  int    `json:"value"`
	}
}

// buildSchemaTestAPI creates a gin engine with a single POST /api/schema-test
// endpoint and returns both the engine and the huma API handle.
func buildSchemaTestAPI(t *testing.T) (*gin.Engine, huma.API) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")
	huma.Register(humaAPI, huma.Operation{
		OperationID: "schema-test",
		Method:      http.MethodPost,
		Path:        "/schema-test",
	}, func(_ context.Context, in *schemaTestInput) (*schemaTestOutput, error) {
		out := &schemaTestOutput{}
		out.Body.Status = "ok"
		out.Body.Value = in.Body.Value
		return out, nil
	})
	return r, humaAPI
}

// TestNoSchemaLeakInSuccessBody asserts that a valid request does NOT receive
// a "$schema" field in the response body, while the expected status/value
// fields are present.
func TestNoSchemaLeakInSuccessBody(t *testing.T) {
	r, _ := buildSchemaTestAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/api/schema-test",
		strings.NewReader(`{"value":42}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got map[string]any
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, hasSchema := got["$schema"]; hasSchema {
		t.Errorf("success body must not contain $schema field; full body: %v", got)
	}
	if got["status"] != "ok" {
		t.Errorf("status = %v, want \"ok\"", got["status"])
	}
	if got["value"] != float64(42) {
		t.Errorf("value = %v, want 42", got["value"])
	}
}

// TestNoSchemaLeakInErrorBody triggers a 422 (invalid body type) and asserts
// that the problem+json error body carries no "$schema" field while still
// presenting the RFC 9457 shape (numeric "status", "title", "errors").
func TestNoSchemaLeakInErrorBody(t *testing.T) {
	r, _ := buildSchemaTestAPI(t)

	// Send a string where an integer is expected; huma will produce a 422.
	req := httptest.NewRequest(http.MethodPost, "/api/schema-test",
		strings.NewReader(`{"value":"not-an-int"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}

	var got map[string]any
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if _, hasSchema := got["$schema"]; hasSchema {
		t.Errorf("error body must not contain $schema field; full body: %v", got)
	}
	// problem+json shape: numeric status (not legacy "error"), title, errors.
	if _, ok := got["status"].(float64); !ok {
		t.Errorf("status = %v (%T), want JSON number; full body: %v", got["status"], got["status"], got)
	}
	if got["status"] == "error" {
		t.Errorf("error body must not use legacy {status:\"error\"} envelope: %v", got)
	}
	if _, hasTitle := got["title"]; !hasTitle {
		t.Errorf("error body must contain title field; full body: %v", got)
	}
	if _, hasErrors := got["errors"]; !hasErrors {
		t.Errorf("error body must contain errors field; full body: %v", got)
	}
}

// TestValidationMessageIncludesFieldDetail asserts that a 422 problem+json body
// carries per-field detail in errors[], so API clients can understand which
// field was invalid. Per RFC 9457, that lives in errors[].location /
// errors[].message rather than a flat message string.
func TestValidationMessageIncludesFieldDetail(t *testing.T) {
	r, _ := buildSchemaTestAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/api/schema-test",
		strings.NewReader(`{"value":"not-an-int"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}

	var got struct {
		Detail string `json:"detail"`
		Errors []struct {
			Message  string `json:"message"`
			Location string `json:"location"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if len(got.Errors) == 0 {
		t.Fatalf("problem+json body must include errors[]; full body: %s", w.Body.String())
	}
	// The offending field's location ("body.value") must appear in errors[].
	foundField := false
	for _, e := range got.Errors {
		if strings.Contains(e.Location, "value") || strings.Contains(e.Message, "value") {
			foundField = true
			break
		}
	}
	if !foundField {
		t.Errorf("errors[] do not mention the offending field \"value\"; full body: %s", w.Body.String())
	}
}

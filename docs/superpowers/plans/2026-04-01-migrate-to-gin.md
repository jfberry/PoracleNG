# Migrate HTTP Router from net/http to Gin

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace Go's standard `net/http` ServeMux with Gin for all API routes and middleware, eliminating route pattern conflicts and gaining better middleware, parameter handling, and JSON helpers.

**Architecture:** All HTTP handling moves to `gin.Engine`. Handler signatures change from `http.HandlerFunc` to `gin.HandlerFunc`. Path parameters accessed via `c.Param()` instead of `r.PathValue()`. Middleware uses Gin's `Use()` and group-level middleware.

**Tech Stack:** gin-gonic/gin v1.10+, existing handler logic unchanged

---

## Current Problems

1. **Route conflicts**: Go 1.22+ mux rejects overlapping wildcard patterns (e.g. `humans/{id}/roles` vs `humans/one/{id}`). Required a manual path dispatcher hack.
2. **No route groups**: All routes registered flat with repeated `auth()` wrapping and `apiRoute()` logging.
3. **Verbose JSON responses**: Every handler manually sets `Content-Type`, calls `json.NewEncoder`, handles errors individually.
4. **No built-in middleware chain**: Auth, logging, CORS, recovery all hand-wired.

## Design

### Route Groups

```go
r := gin.New()
r.Use(gin.Recovery())
r.Use(requestLogger())

// Public endpoints
r.POST("/", webhookHandler)
r.GET("/health", healthHandler)
r.GET("/metrics", metricsHandler)

// Authenticated API
api := r.Group("/api", requireSecret(cfg))
{
    api.POST("/reload", handleReload)
    api.POST("/geofence/reload", handleGeofenceReload)
    api.POST("/test", handleTest)
    api.POST("/command", handleCommand)
    api.POST("/deliverMessages", handleDeliverMessages)
    api.POST("/postMessage", handleDeliverMessages) // legacy alias

    // Tracking CRUD — 10 types, same pattern
    for _, t := range trackingTypes {
        api.GET("/tracking/"+t.path+"/:id", t.get)
        api.POST("/tracking/"+t.path+"/:id", t.create)
        api.DELETE("/tracking/"+t.path+"/:id/byUid/:uid", t.delete)
        api.POST("/tracking/"+t.path+"/:id/delete", t.bulkDelete)
    }
    api.GET("/tracking/all/:id", handleGetAllTracking)
    api.GET("/tracking/allProfiles/:id", handleGetAllProfilesTracking)

    // Humans — no more conflict between /one/:id and /:id/roles
    humans := api.Group("/humans")
    {
        humans.GET("/one/:id", handleGetOneHuman)
        humans.GET("/:id", handleGetHumanAreas)
        humans.GET("/:id/roles", handleGetRoles)
        humans.GET("/:id/getAdministrationRoles", handleGetAdminRoles)
        humans.GET("/:id/checkLocation/:lat/:lon", handleCheckLocation)
        humans.POST("/:id/start", handleStartHuman)
        humans.POST("/:id/stop", handleStopHuman)
        humans.POST("/:id/adminDisabled", handleAdminDisabled)
        humans.POST("/:id/switchProfile/:profile", handleSwitchProfile)
        humans.POST("/:id/setLocation/:lat/:lon", handleSetLocation)
        humans.POST("/:id/setAreas", handleSetAreas)
        humans.POST("/:id/roles/add/:roleId", handleAddRole)
        humans.POST("/:id/roles/remove/:roleId", handleRemoveRole)
        humans.POST("", handleCreateHuman)
    }

    // Profiles
    profiles := api.Group("/profiles")
    {
        profiles.GET("/:id", handleGetProfiles)
        profiles.DELETE("/:id/byProfileNo/:profile_no", handleDeleteProfile)
        profiles.POST("/:id/add", handleAddProfile)
        profiles.POST("/:id/update", handleUpdateProfile)
        profiles.POST("/:id/copy/:from/:to", handleCopyProfile)
    }

    // Geofence, config, masterdata, etc.
    api.GET("/geofence/all", handleGeofenceAll)
    // ... etc
}
```

### Handler Signature Change

**Before (net/http):**
```go
func HandleGetEgg(deps *TrackingDeps) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        id := r.PathValue("id")
        // ...
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(resp)
    }
}
```

**After (gin):**
```go
func HandleGetEgg(deps *TrackingDeps) gin.HandlerFunc {
    return func(c *gin.Context) {
        id := c.Param("id")
        // ... same logic ...
        c.JSON(http.StatusOK, resp)
    }
}
```

### Middleware

**Auth middleware:**
```go
func requireSecret(cfg *config.Config) gin.HandlerFunc {
    return func(c *gin.Context) {
        if cfg.Processor.APISecret == "" {
            c.Next()
            return
        }
        if c.GetHeader("X-Poracle-Secret") != cfg.Processor.APISecret {
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"status": "authError"})
            return
        }
        c.Next()
    }
}
```

**Request logging middleware** (replaces `apiRoute()` wrapper):
```go
func requestLogger() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        c.Next()
        log.Infof("API: %s %s %d %s", c.Request.Method, c.Request.URL.Path,
            c.Writer.Status(), time.Since(start))
    }
}
```

### JSON Response Helpers

Replace `trackingJSONOK`/`trackingJSONError` with gin's built-in:
```go
// Before
trackingJSONOK(w, map[string]any{"egg": result})
trackingJSONError(w, http.StatusNotFound, "User not found")

// After
c.JSON(http.StatusOK, gin.H{"status": "ok", "egg": result})
c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "User not found"})
```

---

## Tasks

### Task 1: Add Gin Dependency and Create Router

**Files:**
- Modify: `processor/go.mod` — add `github.com/gin-gonic/gin`
- Create: `processor/internal/api/router.go` — `NewRouter()` function that creates gin.Engine with middleware
- Modify: `processor/cmd/processor/main.go` — replace `http.NewServeMux()` with `gin.New()`

Set up the gin engine with recovery and logging middleware. Register the webhook handler, health, metrics endpoints. Wire gin as the `http.Server` handler.

### Task 2: Migrate Auth Middleware

**Files:**
- Create: `processor/internal/api/middleware.go` — `RequireSecret()` gin middleware
- Delete the old `RequireSecret` http middleware wrapper from `api/api.go`

### Task 3: Migrate Tracking CRUD Handlers (10 types)

**Files:**
- Modify: `processor/internal/api/trackingEgg.go` (and all 9 others)
- Modify: `processor/internal/api/tracking.go` — update helper functions

For each handler:
1. Change return type from `http.HandlerFunc` to `gin.HandlerFunc`
2. Change `w http.ResponseWriter, r *http.Request` to `c *gin.Context`
3. Replace `r.PathValue("id")` with `c.Param("id")`
4. Replace `r.URL.Query().Get("x")` with `c.Query("x")`
5. Replace `trackingJSONOK(w, data)` with `c.JSON(200, data)` (merge "status":"ok")
6. Replace `trackingJSONError(w, code, msg)` with `c.JSON(code, gin.H{...})`
7. Replace `readJSONBody(r, &v)` with `c.ShouldBindJSON(&v)` or `c.GetRawData()`
8. Replace `r.Method` checks with gin route-level method binding

### Task 4: Migrate Human Endpoints

**Files:**
- Modify: `processor/internal/api/humans.go`
- Remove the path dispatcher hack from `main.go`

Register humans routes as a gin group — the `one/:id` vs `:id/roles` conflict resolves naturally.

### Task 5: Migrate Role Endpoints

**Files:**
- Modify: `processor/internal/api/roles.go`

Same pattern as Task 4: change handler signatures, use `c.Param()`.

### Task 6: Migrate Geofence, Config, Masterdata, and Utility Endpoints

**Files:**
- Modify: `processor/internal/api/tiles.go`, `config.go`, `masterdata.go`
- Modify: All remaining handlers in `processor/internal/api/`

### Task 7: Migrate Webhook Receiver

**Files:**
- Modify: `processor/internal/webhook/receiver.go`

The `POST /` webhook endpoint. Change from `http.Handler` to gin handler.

### Task 8: Wire Up in main.go

**Files:**
- Modify: `processor/cmd/processor/main.go`

Replace all `mux.HandleFunc` registrations with gin route group registrations. Remove the old `http.NewServeMux` setup. Remove `apiRoute()` and `auth()` wrappers. Use `r.Run()` or wrap gin engine in `http.Server` for graceful shutdown.

### Task 9: Clean Up Old HTTP Helpers

**Files:**
- Modify: `processor/internal/api/tracking.go` — remove `trackingJSONOK`, `trackingJSONError`, `readJSONBody`
- Modify: `processor/internal/api/api.go` — remove `RequireSecret`, old middleware
- Remove any unused `net/http` imports

### Task 10: Update Tests

**Files:**
- Modify: `processor/internal/api/*_test.go`

Update any API tests that use `httptest.NewRecorder` to use gin's test context or `httptest` with the gin router.

---

## Implementation Order

1. **Task 1**: Add gin, create router skeleton — can coexist with old mux temporarily
2. **Task 2**: Auth middleware
3. **Task 3**: Tracking handlers (largest batch — 10 files, mechanical changes)
4. **Task 4**: Human endpoints (removes the path dispatcher hack)
5. **Task 5**: Role endpoints
6. **Task 6**: Other endpoints
7. **Task 7**: Webhook receiver
8. **Task 8**: Wire everything in main.go, remove old mux
9. **Task 9**: Clean up old helpers
10. **Task 10**: Tests

Tasks 3-7 are independent and can be done in parallel. The key constraint is Task 8 (final wiring) depends on all handlers being converted.

---

## Migration Pattern (Per Handler)

Every handler follows the same mechanical transformation:

```diff
-func HandleFoo(deps *Deps) http.HandlerFunc {
-    return func(w http.ResponseWriter, r *http.Request) {
-        id := r.PathValue("id")
+func HandleFoo(deps *Deps) gin.HandlerFunc {
+    return func(c *gin.Context) {
+        id := c.Param("id")

         // ... business logic unchanged ...

-        w.Header().Set("Content-Type", "application/json")
-        w.WriteHeader(http.StatusOK)
-        json.NewEncoder(w).Encode(resp)
+        c.JSON(http.StatusOK, resp)
     }
 }
```

For error responses:
```diff
-        trackingJSONError(w, http.StatusNotFound, "User not found")
-        return
+        c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "User not found"})
+        return
```

For request body parsing:
```diff
-        var body MyStruct
-        if err := readJSONBody(r, &body); err != nil {
-            trackingJSONError(w, http.StatusBadRequest, err.Error())
-            return
-        }
+        var body MyStruct
+        if err := c.ShouldBindJSON(&body); err != nil {
+            c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
+            return
+        }
```

Note: Some handlers use `json.RawMessage` for flexible body parsing (single item vs array). These need `c.GetRawData()` instead of `c.ShouldBindJSON()`.

---

## Verification

- `go build ./...` — clean build
- `go test ./...` — all tests pass
- PoracleWeb connects and all features work
- Webhook reception works (POST /)
- Discord/Telegram bot commands work (POST /api/command)
- All tracking CRUD works
- Role management works
- Health check works
- Prometheus metrics still served
- No route conflicts or panics at startup

package api

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/humanlock"
	"github.com/pokemon/poracleng/processor/internal/mute"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func TestHumanLockMiddleware_SerialisesSameHuman(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(HumanLock())
	var inside, overlaps int32
	r.POST("/api/x/:id", func(c *gin.Context) {
		if atomic.AddInt32(&inside, 1) != 1 {
			atomic.AddInt32(&overlaps, 1)
		}
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt32(&inside, -1)
		c.Status(http.StatusOK)
	})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/x/u1", nil))
			if w.Code != http.StatusOK {
				t.Errorf("code %d", w.Code)
			}
		}()
	}
	wg.Wait()
	if overlaps != 0 {
		t.Fatalf("%d overlapping handlers", overlaps)
	}
}

func TestHumanLockMiddleware_GetNotLocked(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(HumanLock())
	r.GET("/api/x/:id", func(c *gin.Context) { c.Status(http.StatusOK) })
	defer humanlock.Lock("u1")()
	done := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/x/u1", nil))
		done <- w.Code
	}()
	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Fatalf("code %d", code)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("GET blocked on human lock")
	}
}

// HumanLock reads the human id from gin's ":id" param. The huma routes are
// declared as {id} and converted by humagin, so exercise the real huma
// instance mounted on a group that carries the middleware, exactly as main.go
// wires it.
func TestHumanLockMiddleware_CoversRealHumaRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	apiGroup := r.Group("/api")
	apiGroup.Use(HumanLock())
	humaAPI := NewHumaAPI(r, apiGroup, "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "U1", Enabled: true, CurrentProfileNo: 1})
	RegisterV2Mutes(humaAPI, &TrackingDeps{Humans: humans, Config: &config.Config{}, Mutes: mute.NewStore()})

	unlock := humanlock.Lock("u1")
	done := make(chan int, 1)
	go func() {
		w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/mutes", `{"scope":"gym","value":"g1"}`)
		done <- w.Code
	}()
	select {
	case code := <-done:
		unlock()
		t.Fatalf("POST completed (%d) while u1's lock was held: middleware did not see the huma {id} param", code)
	case <-time.After(150 * time.Millisecond):
	}
	unlock()
	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Fatalf("code %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("POST did not finish after unlock")
	}
}

package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/snapshots"
)

// HandleSnapshotGet returns the stored Snapshot for a given message ID.
//
// The endpoint is admin-protected via the existing x-poracle-secret
// middleware. It's intended for operator diagnostics ("why didn't this
// button work?", "what view did the user see?") rather than client-side
// rendering — the actual button click handlers read snapshots directly
// from the store, not via this API.
//
// Responses:
//   - 200 with the Snapshot JSON if found.
//   - 404 if no snapshot exists for the message ID.
//   - 503 if [snapshots] enabled = false (no store wired in).
//
// The handler accepts the target as a query parameter when the same
// message ID may exist across multiple destinations (channels and DMs).
// Without ?target=..., the handler tries the path component as a raw key
// first, falling back to a scan of the configured target prefix is NOT
// done — operators provide both halves when they want the answer.
//
// Path: GET /api/snapshots/:messageID?target=<human_id>
func HandleSnapshotGet(store snapshots.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if store == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "snapshots disabled",
				"hint":  "set [snapshots] enabled = true in config.toml",
			})
			return
		}
		messageID := c.Param("messageID")
		if messageID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "messageID is required"})
			return
		}
		target := c.Query("target")
		if target == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "target query parameter is required",
				"hint":  "GET /api/snapshots/<messageID>?target=<human_id>",
			})
			return
		}

		key := snapshots.MakeKey(target, messageID)
		snap, err := store.Read(c.Request.Context(), key)
		if err != nil {
			if errors.Is(err, snapshots.ErrNotFound) {
				metrics.SnapshotReadsTotal.WithLabelValues("miss").Inc()
				c.JSON(http.StatusNotFound, gin.H{"error": "snapshot not found", "key": key})
				return
			}
			if errors.Is(err, snapshots.ErrClosed) {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "snapshot store closing"})
				return
			}
			metrics.SnapshotReadsTotal.WithLabelValues("error").Inc()
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		metrics.SnapshotReadsTotal.WithLabelValues("hit").Inc()
		c.JSON(http.StatusOK, snap)
	}
}

// Ensure context import isn't unused (gin.Context.Request.Context already
// returns one, but this comment exists so a future refactor that drops the
// import doesn't break Go's import lints).
var _ = context.Background

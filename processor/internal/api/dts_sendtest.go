package api

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	raymond "github.com/mailgun/raymond/v2"

	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/dts"
)

// HandleDTSSendTest renders a template with provided variables and delivers to a user.
// POST /api/dts/sendtest
func HandleDTSSendTest(dispatcher *delivery.Dispatcher, ts *dts.TemplateStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		if dispatcher == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "delivery dispatcher not configured"})
			return
		}

		var req struct {
			Template  any            `json:"template"`
			Variables map[string]any `json:"variables"`
			Target    struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"target"`
			Language string `json:"language"`
			Platform string `json:"platform"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
			return
		}

		if req.Template == nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "template is required"})
			return
		}
		if req.Target.ID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "target.id is required"})
			return
		}
		if req.Target.Type == "" {
			req.Target.Type = "discord:user"
		}
		if req.Language == "" {
			req.Language = "en"
		}
		if req.Platform == "" {
			req.Platform = "discord"
		}

		// JSON-stringify the template object, then compile as Handlebars
		templateJSON, err := json.Marshal(req.Template)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "invalid template: " + err.Error()})
			return
		}

		compiled, err := raymond.Parse(string(templateJSON))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "template compile error: " + err.Error()})
			return
		}

		// Register partials so {{> partialName}} works
		partials := ts.Partials()
		if len(partials) > 0 {
			compiled.RegisterPartials(partials)
		}

		df := raymond.NewDataFrame()
		df.Set("language", req.Language)
		df.Set("platform", req.Platform)

		rendered, err := compiled.ExecWith(req.Variables, df)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "render error: " + err.Error()})
			return
		}

		// Parse rendered JSON into message object
		var message json.RawMessage
		if err := json.Unmarshal([]byte(rendered), &message); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "rendered template is not valid JSON: " + err.Error()})
			return
		}

		// Dispatch delivery job
		job := &delivery.Job{
			Target:       req.Target.ID,
			Type:         req.Target.Type,
			Message:      message,
			Name:         "DTS Editor Test",
			LogReference: "dts-editor",
		}
		dispatcher.Dispatch(job)

		c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "sent"})
	}
}

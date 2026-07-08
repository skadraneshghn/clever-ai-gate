package admin

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/skadraneshghn/clever-ai-gate/internal/telemetry"
)

// LogAdminController exposes admin-only endpoints for observing and downloading
// the gateway's structured log output.
type LogAdminController struct {
	hub *telemetry.LogHub
}

// NewLogAdminController creates a controller backed by the given LogHub.
func NewLogAdminController(hub *telemetry.LogHub) *LogAdminController {
	return &LogAdminController{hub: hub}
}

// StreamLiveCoreLogs upgrades the connection to an HTTP Server-Sent Events stream
// and relays raw JSON log lines from the gateway's zap logger in real-time.
//
// Each SSE frame is formatted as:
//
//	data: <raw-json-log-line>\n\n
//
// The stream terminates automatically when the browser tab is closed or the
// client's request context is cancelled.
//
// Protected by AdminAuth middleware — requires Authorization: Bearer <admin-key>.
func (ctrl *LogAdminController) StreamLiveCoreLogs(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no") // Disable Nginx buffering for SSE

	// Buffered channel: 256 entries. The LogHub will drop frames (not block)
	// if the consumer falls behind, keeping the proxy hot-path unaffected.
	logChan := make(chan []byte, 256)
	ctrl.hub.RegisterListener(logChan)
	defer func() {
		ctrl.hub.UnregisterListener(logChan)
		close(logChan)
	}()

	// Send an initial ping so the browser EventSource knows the connection
	// is alive immediately (avoids a blank UI for up to 3 s on reconnect).
	fmt.Fprintf(c.Writer, ": ping\n\n")
	c.Writer.Flush()

	c.Stream(func(w io.Writer) bool {
		select {
		case logLine, ok := <-logChan:
			if !ok {
				return false
			}
			// Strip trailing newline from zap output (zap appends \n) before
			// wrapping in the SSE frame so the frontend gets clean JSON.
			line := logLine
			if len(line) > 0 && line[len(line)-1] == '\n' {
				line = line[:len(line)-1]
			}
			_, writeErr := fmt.Fprintf(w, "data: %s\n\n", line)
			return writeErr == nil

		case <-c.Request.Context().Done():
			// Browser disconnected — tear down the loop cleanly.
			return false
		}
	})
}

// DownloadTodayLogFile serves today's rotating log file as a binary attachment.
// The filename will be in the format gateway-YYYY-MM-DD.log.
//
// Protected by AdminAuth middleware — requires Authorization: Bearer <admin-key>.
func (ctrl *LogAdminController) DownloadTodayLogFile(c *gin.Context) {
	path := ctrl.hub.GetTodayLogPath()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "no log file found for today",
		})
		return
	}

	filename := filepath.Base(path)
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Cache-Control", "no-store")

	c.File(path)
}

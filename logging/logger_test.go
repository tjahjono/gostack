package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/tjahjono/gostack/config"
	"github.com/tjahjono/gostack/logging"
)

func TestRedactionAndLevel(t *testing.T) {
	var output bytes.Buffer
	l, err := logging.NewJSONLogger(&output, "info")
	if err != nil {
		t.Fatal(err)
	}
	l.Log(context.Background(), slog.LevelDebug, "hidden")
	l.Log(context.Background(), slog.LevelInfo, "connected", slog.String("password", "sensitive"), slog.Any("connection", config.NewSecret("sensitive")), slog.Group("nested", slog.String("session_token", "sensitive")))
	if strings.Contains(output.String(), "sensitive") || strings.Contains(output.String(), "hidden") {
		t.Fatal("redaction or level filtering failed")
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["msg"] != "connected" {
		t.Fatal("missing structured message")
	}
}

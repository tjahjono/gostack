// Package logging provides an injected, structured, redacting logger.
package logging

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
)

type Logger interface {
	Log(context.Context, slog.Level, string, ...slog.Attr)
}
type JSONLogger struct{ logger *slog.Logger }

func NewJSONLogger(writer io.Writer, level string) (*JSONLogger, error) {
	if writer == nil {
		return nil, errors.New("log writer is required")
	}
	var threshold slog.Level
	if err := threshold.UnmarshalText([]byte(level)); err != nil {
		return nil, errors.New("invalid log level")
	}
	h := slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: threshold, ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
		key := strings.ToLower(strings.Join(append(append([]string{}, groups...), a.Key), "."))
		for _, sensitive := range []string{"password", "secret", "token", "authorization", "cookie", "database_url", "dsn"} {
			if strings.Contains(key, sensitive) {
				return slog.String(a.Key, "[REDACTED]")
			}
		}
		return a
	}})
	return &JSONLogger{logger: slog.New(h)}, nil
}

func (l *JSONLogger) Log(ctx context.Context, level slog.Level, message string, attrs ...slog.Attr) {
	l.logger.LogAttrs(ctx, level, message, attrs...)
}

// Writer adapts standard-library transport logs to the injected logger.
type Writer struct{ logger Logger }

func NewWriter(logger Logger) (*Writer, error) {
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	return &Writer{logger: logger}, nil
}
func (w *Writer) Write(data []byte) (int, error) {
	w.logger.Log(context.Background(), slog.LevelError, "HTTP transport", slog.String("detail", strings.TrimSpace(string(data))))
	return len(data), nil
}

var _ Logger = (*JSONLogger)(nil)
var _ io.Writer = (*Writer)(nil)

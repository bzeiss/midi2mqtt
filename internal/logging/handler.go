package logging

import (
	"context"
	"io"
	"log/slog"
)

type CustomHandler struct {
	out  io.Writer
	opts slog.HandlerOptions
}

func NewCustomHandler(w io.Writer, opts *slog.HandlerOptions) *CustomHandler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	return &CustomHandler{
		out:  w,
		opts: *opts,
	}
}

func (h *CustomHandler) Enabled(ctx context.Context, level slog.Level) bool {
	minLevel := h.opts.Level.Level()
	return level >= minLevel
}

func (h *CustomHandler) Handle(ctx context.Context, r slog.Record) error {
	level := r.Level.String()
	timeStr := r.Time.Format("15:04:05")

	// Format: [HH:MM:SS] [LEVEL] Message attr1=value1 attr2=value2
	msg := r.Message
	var attrs []string
	r.Attrs(func(a slog.Attr) bool {
		if a.Key != "" && a.Value.String() != "" {
			attrs = append(attrs, a.Key+"="+a.Value.String())
		}
		return true
	})

	attrStr := ""
	if len(attrs) > 0 {
		attrStr = " " + joinAttrs(attrs)
	}

	_, err := io.WriteString(h.out,
		"["+timeStr+"] "+
			"["+level+"] "+
			msg+attrStr+"\n")
	return err
}

func (h *CustomHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *CustomHandler) WithGroup(name string) slog.Handler {
	return h
}

func joinAttrs(attrs []string) string {
	result := ""
	for i, attr := range attrs {
		if i > 0 {
			result += " "
		}
		result += attr
	}
	return result
}

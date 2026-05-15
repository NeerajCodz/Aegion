package xlog

import (
	"context"
	"log/slog"
)

// SlogHandler bridges slog records into xlog system events.
type SlogHandler struct {
	logger *Logger
	attrs  []slog.Attr
	group  string
}

// NewSlogHandler creates a slog handler backed by xlog.
func NewSlogHandler(l *Logger) slog.Handler {
	if l == nil {
		l = Default()
	}
	return &SlogHandler{logger: l}
}

func (h *SlogHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *SlogHandler) Handle(ctx context.Context, r slog.Record) error {
	name := "log." + r.Level.String()
	if r.Message != "" {
		name = normalizeEventName(r.Message)
	}
	event := h.logger.Start(ctx, name, WithKind(KindSystem)).
		Set("log.level", r.Level.String()).
		Set("log.message", r.Message)
	for _, attr := range h.attrs {
		addSlogAttr(event, h.group, attr)
	}
	r.Attrs(func(attr slog.Attr) bool {
		addSlogAttr(event, h.group, attr)
		return true
	})
	if r.Level >= slog.LevelError {
		event.Error(nil)
	} else {
		event.Success()
	}
	return event.Emit()
}

func (h *SlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	copied := append([]slog.Attr{}, h.attrs...)
	copied = append(copied, attrs...)
	return &SlogHandler{logger: h.logger, attrs: copied, group: h.group}
}

func (h *SlogHandler) WithGroup(name string) slog.Handler {
	next := *h
	if next.group == "" {
		next.group = name
	} else {
		next.group += "." + name
	}
	return &next
}

func addSlogAttr(event *Event, group string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	key := attr.Key
	if group != "" {
		key = group + "." + key
	}
	switch attr.Value.Kind() {
	case slog.KindString:
		event.Set(key, attr.Value.String())
	case slog.KindInt64:
		event.Set(key, attr.Value.Int64())
	case slog.KindUint64:
		event.Set(key, attr.Value.Uint64())
	case slog.KindFloat64:
		event.Set(key, attr.Value.Float64())
	case slog.KindBool:
		event.Set(key, attr.Value.Bool())
	case slog.KindDuration:
		event.Set(key, attr.Value.Duration().Milliseconds())
	case slog.KindTime:
		event.Set(key, attr.Value.Time().Format("2006-01-02T15:04:05.999999999Z07:00"))
	default:
		event.Set(key, attr.Value.Any())
	}
}

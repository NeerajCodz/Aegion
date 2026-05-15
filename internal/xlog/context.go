package xlog

import "context"

type eventContextKey struct{}

// WithEvent attaches an event to context.
func WithEvent(ctx context.Context, e *Event) context.Context {
	return context.WithValue(ctx, eventContextKey{}, e)
}

// EventFromContext returns the active event from context.
func EventFromContext(ctx context.Context) *Event {
	if ctx == nil {
		return nil
	}
	if e, ok := ctx.Value(eventContextKey{}).(*Event); ok {
		return e
	}
	return nil
}

// Set adds a field to the active context event.
func Set(ctx context.Context, key string, value any) {
	if e := EventFromContext(ctx); e != nil {
		e.Set(key, value)
	}
}

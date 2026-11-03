package api

import (
	"context"
	"strings"
	"unicode/utf8"
)

// AppContext locates the user's current app view for one message. Future apps
// share the discriminator and can add their own optional identifiers.
type AppContext struct {
	App      string `json:"app"`
	CourseID string `json:"course_id,omitempty"`
	TopicID  string `json:"topic_id,omitempty"`
}

// Validate keeps contextual identifiers small and allows optional classroom course and topic IDs.
func (c *AppContext) Validate() error {
	if c == nil {
		return nil
	}
	if strings.TrimSpace(c.App) == "" || len(c.App) > 64 || !utf8.ValidString(c.App) || len(c.CourseID) > 200 || len(c.TopicID) > 200 || !utf8.ValidString(c.CourseID) || !utf8.ValidString(c.TopicID) {
		return &Error{Code: CodeInvalidParams, Message: "invalid app context identifiers"}
	}
	return nil
}

// CloneAppContext gives queued turns ownership of their context value.
func CloneAppContext(c *AppContext) *AppContext {
	if c == nil {
		return nil
	}
	copy := *c
	return &copy
}

type appContextKey struct{}

func WithAppContext(ctx context.Context, app AppContext) context.Context {
	return context.WithValue(ctx, appContextKey{}, app)
}
func AppContextFrom(ctx context.Context) *AppContext {
	app, ok := ctx.Value(appContextKey{}).(AppContext)
	if !ok {
		return nil
	}
	return &app
}

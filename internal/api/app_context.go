package api

import "context"

// AppContext accompanies one conversation message. It selects application
// context without creating another conversation or changing its stored text.
type AppContext struct {
	App      string `json:"app"`
	CourseID string `json:"course_id,omitempty"`
	TopicID  string `json:"topic_id,omitempty"`
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
func CloneAppContext(app *AppContext) *AppContext {
	if app == nil {
		return nil
	}
	copy := *app
	return &copy
}

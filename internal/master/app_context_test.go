package master

import (
	"context"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

func TestMessageAppContextDoesNotAliasCallerOrHistory(t *testing.T) {
	store := NewMemStore()
	app := &api.AppContext{App: "classroom", CourseID: "course"}
	if _, err := store.SaveMessage(context.Background(), "conversation", Normalized{Text: "Explain", AppContext: app}); err != nil {
		t.Fatal(err)
	}
	app.CourseID = "changed"
	history := store.History("conversation", 0)
	if history[0].AppContext.CourseID != "course" {
		t.Fatal("caller changed stored context")
	}
	history[0].AppContext.CourseID = "changed again"
	if store.History("conversation", 0)[0].AppContext.CourseID != "course" {
		t.Fatal("history changed stored context")
	}
}

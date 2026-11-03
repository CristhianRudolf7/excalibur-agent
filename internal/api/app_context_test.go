package api

import (
	"strings"
	"testing"
)

func TestAppContextValidation(t *testing.T) {
	for _, tc := range []struct {
		c  *AppContext
		ok bool
	}{
		{nil, true}, {&AppContext{App: "classroom", CourseID: "c"}, true}, {&AppContext{App: "classroom", CourseID: "c", TopicID: "t"}, true}, {&AppContext{App: "future"}, true},
		{&AppContext{}, false}, {&AppContext{App: "classroom"}, true}, {&AppContext{App: "classroom", TopicID: "t"}, true}, {&AppContext{App: strings.Repeat("a", 65)}, false}, {&AppContext{App: "classroom", CourseID: strings.Repeat("a", 201)}, false}, {&AppContext{App: "classroom", CourseID: "c", TopicID: strings.Repeat("a", 201)}, false},
	} {
		if err := tc.c.Validate(); (err == nil) != tc.ok {
			t.Fatalf("%+v: %v", tc.c, err)
		}
	}
}

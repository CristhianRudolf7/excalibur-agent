package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSPAAndAssets(t *testing.T) {
	h := handler(fstest.MapFS{"index.html": {Data: []byte("<html>product</html>")}, "assets/main.js": {Data: []byte("export {}")}})
	for _, tc := range []struct {
		path, method string
		code         int
		content      string
	}{
		{"/", "GET", 200, "text/html"}, {"/settings", "GET", 200, "text/html"},
		{"/chat/thread", "HEAD", 200, "text/html"}, {"/assets/main.js", "GET", 200, "javascript"},
		{"/assets/missing.js", "GET", 404, "text/plain"}, {"/rpc/missing", "GET", 404, "text/plain"},
		{"/settings", "POST", 405, ""},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.code || !strings.Contains(w.Header().Get("Content-Type"), tc.content) {
			t.Errorf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Header())
		}
	}
}
func TestEmbeddedDefault(t *testing.T) {
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest("GET", "/settings", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Excalibur") {
		t.Fatal(w.Code, w.Body.String())
	}
}

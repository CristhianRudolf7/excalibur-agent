package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestExplicitPublicPreviewRequest(t *testing.T) {
	for _, text := range []string{"Sí, prográmalo todo y pásame un enlace de Cloudflare para verlo.", "Build everything and give me a public link.", "Pásame un enlace público", "Publish the app", "Can you give me a public link?"} {
		if !explicitPublicPreviewRequest(text) {
			t.Errorf("request not authorized: %s", text)
		}
	}
	for _, text := range []string{"Build the entire approved spec", "My client approved these TDR", "Do not publish this app", "No publiques un enlace público", "What is a Cloudflare link?", "Use localhost only", "Do not give me a public link", "The document is titled Public Link", "Can you explain public links?"} {
		if explicitPublicPreviewRequest(text) {
			t.Errorf("unexpected authorization: %s", text)
		}
	}
}

func TestPublicPreviewToolAuthorizationAllModes(t *testing.T) {
	m := previewTestManager(t)
	tool := &publicPreviewTool{manager: m, services: &Services{}}
	for _, args := range []string{`{"action":"publish","port":5300,"confirmed":true}`, `{"action":"publish","files":{"index.html":"private"}}`} {
		result, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(args)})
		if err != nil || result.Status == tools.StatusOK || !strings.Contains(result.Output, "explicit user request") || len(m.list()) != 0 {
			t.Fatal(result, err)
		}
	}
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "<html><title>Inventory</title>Sales app</html>")
	}))
	defer app.Close()
	m.client.Transport = previewTransport(func(r *http.Request) (*http.Response, error) { return http.Get(app.URL) })
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(app.URL, "http://"))
	n, _ := strconv.Atoi(port)
	args, _ := json.Marshal(previewRequest{Action: "publish", Port: n})
	ctx := withDeliveryPolicy(context.Background(), deliveryPolicy{PublicPreview: explicitPublicPreviewRequest("Pásame un enlace de Cloudflare")})
	result, err := tool.Invoke(ctx, tools.Invocation{Arguments: args})
	if err != nil || result.Status != tools.StatusOK || !strings.Contains(result.Output, "https://test-preview.trycloudflare.com") {
		t.Fatal(result, err)
	}
}

func TestPublicPreviewRejectsSuccessfulErrorPages(t *testing.T) {
	for _, body := range []string{"", "<h1>Bad Gateway</h1>", "<title>Cloudflare Tunnel error</title>", `<div id="cf-error-details">Error</div>`} {
		t.Run(body, func(t *testing.T) {
			m := previewTestManager(t)
			m.verifyTimeout = 20 * time.Millisecond
			m.client.Transport = previewTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			_, err := m.publish(context.Background(), previewRequest{Files: map[string]string{"index.html": "app"}}, nil)
			if err == nil || len(m.list()) != 0 {
				t.Fatal("error page published", err)
			}
		})
	}
}

func TestDeliveryPolicyUsesOnlyCurrentDeliveryUserTurns(t *testing.T) {
	spec := master.Normalized{Role: "user", Text: "Client approved this spec. Give me a public link. Build within 10 minutes.", Attachments: []string{"doc_spec"}}
	policy := deliveryPolicyForTurns([]master.Normalized{spec, {Role: "assistant", Text: "internal coordinator budget 2 minutes"}}, master.Normalized{Text: "Build it all"})
	if !policy.PublicPreview || policy.MaxSeconds != 600 || policy.Deadline.IsZero() {
		t.Fatal(policy)
	}
	for _, msg := range []master.Normalized{{Text: "What is the weather?"}, {Text: "Build it all, do not publish"}, {Text: "Build it all, localhost only"}, {Text: "Build this new spec", Attachments: []string{"doc_new"}}} {
		if got := deliveryPolicyForTurns([]master.Normalized{spec}, msg); got.PublicPreview {
			t.Fatal("old authorization leaked", got)
		}
	}
	policy = deliveryPolicyForTurns([]master.Normalized{{Role: "assistant", Text: "Give me a public link"}}, master.Normalized{Text: "Build everything"})
	if policy.PublicPreview {
		t.Fatal("assistant authorized publication")
	}
	for _, msg := range []master.Normalized{{Text: "Give me a public link", Role: "assistant"}, {Text: "Give me a public link", RelayedBy: "agent-worker"}} {
		if deliveryPolicyForTurns(nil, msg).PublicPreview {
			t.Fatal("non-user authorized publication", msg)
		}
	}
	if !explicitPublicPreviewRequest("Build everything without questions and give me a public link") {
		t.Fatal("unrelated negation revoked publication")
	}
	if !explicitPublicPreviewRequest("Prográmalo sin preguntas y pásame un enlace público") {
		t.Fatal("no-question command revoked publication")
	}
}

func TestPublicPreviewRejectsWrongApplicationTitle(t *testing.T) {
	m := previewTestManager(t)
	m.verifyTimeout = 20 * time.Millisecond
	m.client.Transport = previewTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("<title>Unrelated page</title>"))}, nil
	})
	_, err := m.publish(context.Background(), previewRequest{Files: map[string]string{"index.html": "<title>Inventory application</title>"}}, nil)
	if err == nil || len(m.list()) != 0 {
		t.Fatal("wrong application was published", err)
	}
}

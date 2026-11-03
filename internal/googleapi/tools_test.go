package googleapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

type fakeProvider struct {
	connected bool
	err       error
}

func (p fakeProvider) Status() Status {
	return Status{Connected: p.connected, Email: "sender@example.test"}
}
func (p fakeProvider) AccessToken(context.Context) (string, error) { return "fake-access-token", p.err }
func harness(t *testing.T, handler http.HandlerFunc) (*tools.MapRegistry, *httptest.Server) {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	r := tools.NewMapRegistry()
	err := RegisterTools(r, fakeProvider{connected: true}, ToolOptions{HTTPClient: s.Client(), GmailBaseURL: s.URL, DriveBaseURL: s.URL, DriveUploadBaseURL: s.URL, DocsBaseURL: s.URL, Blobs: gateway.NewMemoryBlobs()})
	if err != nil {
		t.Fatal(err)
	}
	return r, s
}
func invoke(t *testing.T, r tools.Registry, name string, args any) tools.Result {
	t.Helper()
	tool, err := r.Get(name)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(args)
	result, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: b})
	if err != nil {
		t.Fatalf("%s %s: %v (%s)", name, b, err, result.Output)
	}
	return result
}
func readJSON(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}
func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func checkRequest(t *testing.T, r *http.Request, method, path string) {
	t.Helper()
	if r.Method != method || r.URL.Path != path {
		t.Errorf("got %s %s; want %s %s", r.Method, r.URL.Path, method, path)
	}
	if r.Header.Get("Authorization") != "Bearer fake-access-token" {
		t.Error("missing bearer token")
	}
}
func messageFixture() map[string]any {
	return map[string]any{"id": "m1", "threadId": "t1", "labelIds": []string{"INBOX"}, "snippet": "snippet", "payload": map[string]any{"mimeType": "multipart/mixed", "headers": []map[string]string{{"name": "From", "value": "author@example.test"}, {"name": "To", "value": "reader@example.test"}, {"name": "Subject", "value": mime.QEncoding.Encode("utf-8", "Reunión 🌍")}, {"name": "Date", "value": "Mon, 02 Nov 2026 10:00:00 +0000"}, {"name": "Message-ID", "value": "<parent@example.test>"}, {"name": "References", "value": "<ancestor@example.test>"}}, "parts": []any{map[string]any{"mimeType": "multipart/alternative", "parts": []any{map[string]any{"mimeType": "text/html", "body": map[string]any{"data": base64.RawURLEncoding.EncodeToString([]byte("<p>HTML</p>"))}}, map[string]any{"mimeType": "text/plain", "body": map[string]any{"data": base64.RawURLEncoding.EncodeToString([]byte("Hola, reunión 🌍"))}}}}, map[string]any{"mimeType": "application/pdf", "filename": "reunión.pdf", "body": map[string]any{"attachmentId": "a1", "size": 123}}}}}
}
func TestGmailSearchAndRead(t *testing.T) {
	for _, action := range []string{"search", "read", "thread"} {
		t.Run(action, func(t *testing.T) {
			requests := 0
			r, _ := harness(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				switch r.URL.Path {
				case "/users/me/messages":
					checkRequest(t, r, "GET", r.URL.Path)
					if r.URL.Query().Get("q") != "is:unread" || r.URL.Query().Get("maxResults") != "3" || r.URL.Query().Get("pageToken") != "next" {
						t.Error(r.URL.RawQuery)
					}
					respond(w, map[string]any{"messages": []any{map[string]string{"id": "m1"}}, "nextPageToken": "page2"})
				case "/users/me/messages/m1":
					want := "full"
					if action == "search" {
						want = "metadata"
					}
					if r.URL.Query().Get("format") != want {
						t.Error(r.URL.RawQuery)
					}
					respond(w, messageFixture())
				case "/users/me/threads/t1":
					respond(w, map[string]any{"messages": []any{messageFixture()}})
				default:
					t.Errorf("unexpected URL %s", r.URL)
					http.NotFound(w, r)
				}
			})
			args := map[string]any{"action": action, "id": "m1", "query": "is:unread", "max_results": 3, "page_token": "next"}
			if action == "thread" {
				args["action"] = "read"
				args["thread_id"] = "t1"
			}
			out := invoke(t, r, "gmail", args).Output
			for _, s := range []string{"m1", "t1", "Reunión 🌍", "author@example.test", "INBOX"} {
				if !strings.Contains(out, s) {
					t.Errorf("missing %s: %s", s, out)
				}
			}
			if action != "search" {
				for _, s := range []string{"Hola, reunión 🌍", "reunión.pdf", "123"} {
					if !strings.Contains(out, s) {
						t.Error(out)
					}
				}
				if strings.Contains(out, "HTML") {
					t.Error("did not prefer plain text")
				}
			}
			if action == "search" && requests != 2 {
				t.Error(requests)
			}
		})
	}
}
func TestGmailMailboxActions(t *testing.T) {
	cases := []struct {
		action, method, path string
		args                 map[string]any
	}{
		{"modify", "POST", "/users/me/messages/m1/modify", map[string]any{"read": true, "starred": true, "add_labels": []string{"CUSTOM"}}},
		{"archive", "POST", "/users/me/messages/m1/modify", nil},
		{"trash", "POST", "/users/me/messages/m1/trash", nil},
		{"untrash", "POST", "/users/me/messages/m1/untrash", nil},
		{"delete", "DELETE", "/users/me/messages/m1", nil},
		{"labels", "GET", "/users/me/labels", nil},
		{"labels", "POST", "/users/me/labels", map[string]any{"name": "Projects"}},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			called := false
			r, _ := harness(t, func(w http.ResponseWriter, r *http.Request) {
				called = true
				checkRequest(t, r, tc.method, tc.path)
				if tc.action == "modify" || tc.action == "archive" {
					v := readJSON(t, r)
					b, _ := json.Marshal(v)
					s := string(b)
					if tc.action == "modify" && (!strings.Contains(s, "UNREAD") || !strings.Contains(s, "STARRED") || !strings.Contains(s, "CUSTOM")) {
						t.Error(s)
					}
					if tc.action == "archive" && !strings.Contains(s, "INBOX") {
						t.Error(s)
					}
				}
				if tc.action == "labels" && tc.method == "POST" {
					if readJSON(t, r)["name"] != "Projects" {
						t.Error("label name")
					}
				}
				respond(w, map[string]any{"id": "result"})
			})
			a := map[string]any{"action": tc.action, "id": "m1", "confirmed": true}
			for k, v := range tc.args {
				a[k] = v
			}
			invoke(t, r, "gmail", a)
			if !called {
				t.Fatal("no request")
			}
		})
	}
}
func TestGmailSendAndDraftMIME(t *testing.T) {
	for _, mode := range []string{"send", "draft", "update", "reply", "send_draft"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			r, _ := harness(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path == "/users/me/messages/parent" {
					respond(w, messageFixture())
					return
				}
				method, path := "POST", "/users/me/messages/send"
				if mode == "draft" {
					path = "/users/me/drafts"
				}
				if mode == "update" {
					method = "PUT"
					path = "/users/me/drafts/d1"
				}
				if mode == "send_draft" {
					path = "/users/me/drafts/send"
				}
				checkRequest(t, r, method, path)
				v := readJSON(t, r)
				if mode == "send_draft" {
					if v["id"] != "d1" {
						t.Error(v)
					}
				} else {
					if mode == "draft" || mode == "update" {
						v = v["message"].(map[string]any)
					}
					raw, err := base64.RawURLEncoding.DecodeString(v["raw"].(string))
					if err != nil {
						t.Fatal(err)
					}
					m, err := mail.ReadMessage(strings.NewReader(string(raw)))
					if err != nil {
						t.Fatal(err)
					}
					subject, err := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
					if err != nil || subject != "Reunión 🌍" {
						t.Errorf("subject %q: %v", subject, err)
					}
					for _, key := range []string{"To", "Cc", "Bcc", "From"} {
						if _, err := mail.ParseAddressList(m.Header.Get(key)); err != nil {
							t.Errorf("%s: %v", key, err)
						}
					}
					b, _ := io.ReadAll(quotedprintable.NewReader(m.Body))
					if string(b) != "Hola, ¿qué tal? 😀\r\nSegunda línea" {
						t.Errorf("body %q", b)
					}
					if mode == "reply" {
						if v["threadId"] != "t1" || m.Header.Get("In-Reply-To") != "<parent@example.test>" || m.Header.Get("References") != "<ancestor@example.test> <parent@example.test>" {
							t.Error("lost threading headers")
						}
					}
				}
				respond(w, map[string]any{"id": "result", "threadId": "t1"})
			})
			a := map[string]any{"action": "send", "to": []string{"José <jose@example.test>"}, "cc": []string{"cc@example.test"}, "bcc": []string{"bcc@example.test"}, "subject": "Reunión 🌍", "body": "Hola, ¿qué tal? 😀\nSegunda línea", "confirmed": true}
			if mode == "draft" || mode == "update" {
				a["action"] = "draft"
			}
			if mode == "update" || mode == "send_draft" {
				a["draft_id"] = "d1"
			}
			if mode == "reply" {
				a["reply_to_id"] = "parent"
				delete(a, "subject")
			}
			invoke(t, r, "gmail", a)
			if calls == 0 {
				t.Error("no request")
			}
		})
	}
}
func TestGmailAttachments(t *testing.T) {
	r, _ := harness(t, func(w http.ResponseWriter, r *http.Request) {
		v := readJSON(t, r)
		raw, _ := base64.RawURLEncoding.DecodeString(v["raw"].(string))
		m, err := mail.ReadMessage(strings.NewReader(string(raw)))
		if err != nil {
			t.Fatal(err)
		}
		mt, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
		if err != nil || mt != "multipart/mixed" {
			t.Fatal(mt, err)
		}
		mr := multipart.NewReader(m.Body, params["boundary"])
		p, err := mr.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		text, _ := io.ReadAll(p)
		if string(text) != "Body 😀" {
			t.Errorf("%q", text)
		}
		p, err = mr.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		_, par, _ := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
		if par["filename"] != "informe ñ.pdf" {
			t.Error(par)
		}
		data, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, p))
		if err != nil || string(data) != "PDF bytes" {
			t.Error(string(data), err)
		}
		respond(w, map[string]string{"id": "sent"})
	})
	tool, _ := r.Get("gmail")
	c := tool.(*googleTool).c
	digest, err := c.saveBlob([]byte("PDF bytes"))
	if err != nil {
		t.Fatal(err)
	}
	invoke(t, r, "gmail", map[string]any{"action": "send", "confirmed": true, "to": []string{"a@example.test"}, "body": "Body 😀", "attachments": []attachment{{Digest: digest, Name: "informe ñ.pdf", MIMEType: "application/pdf"}}})
}
func TestGoogleConfirmationAndConnection(t *testing.T) {
	t.Setenv("EXCALIBUR_GOOGLE_CONFIRM", "1")
	for _, name := range []string{"gmail", "drive", "docs"} {
		for _, action := range map[string][]string{"gmail": {"send", "delete", "trash"}, "drive": {"delete", "trash", "share"}, "docs": {"replace_text"}}[name] {
			t.Run(name+action, func(t *testing.T) {
				r, _ := harness(t, func(http.ResponseWriter, *http.Request) { t.Error("confirmation made HTTP call") })
				tl, _ := r.Get(name)
				result, err := tl.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(fmt.Sprintf(`{"action":%q,"id":"file1","subject":"Review","to":["a@example.test"]}`, action))})
				if err == nil || result.Status != tools.StatusDenied || !strings.Contains(result.Output, "explicit approval in chat") || !strings.Contains(result.Output, "file1") {
					t.Fatal(result, err)
				}
			})
		}
	}
	for _, p := range []fakeProvider{{}, {connected: true, err: ErrNotConnected}, {connected: true, err: errors.New("invalid_grant SECRET")}} {
		r := tools.NewMapRegistry()
		if err := RegisterTools(r, p, ToolOptions{}); err != nil {
			t.Fatal(err)
		}
		tl, _ := r.Get("gmail")
		result, err := tl.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"action":"search"}`)})
		if err == nil || !strings.Contains(result.Output, "Settings → Connections →") || strings.Contains(result.Output, "SECRET") {
			t.Fatal(result, err)
		}
		if p.err != nil && !errors.Is(p.err, ErrNotConnected) && !strings.Contains(result.Output, "Reconnect") {
			t.Error(result.Output)
		}
	}
}
func TestGoogleConfirmationDisabled(t *testing.T) {
	for _, setting := range []bool{false, true} {
		t.Run(fmt.Sprint(setting), func(t *testing.T) {
			if !setting {
				t.Setenv("EXCALIBUR_GOOGLE_CONFIRM", "0")
			} else {
				t.Setenv("EXCALIBUR_GOOGLE_CONFIRM", "1")
			}
			r, _ := harness(t, func(w http.ResponseWriter, r *http.Request) { respond(w, map[string]any{}) })
			tl, _ := r.Get("gmail")
			tl.(*googleTool).c.opts.DisableConfirmation = setting
			invoke(t, r, "gmail", map[string]any{"action": "trash", "id": "m1"})
		})
	}
}
func TestGmailHTMLAndValidation(t *testing.T) {
	r, _ := harness(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]any{"id": "html", "payload": map[string]any{"mimeType": "text/html", "body": map[string]string{"data": base64.RawURLEncoding.EncodeToString([]byte("<p>Hola &amp; adiós 😀</p><script>secret()</script><div>Second</div>"))}}})
	})
	out := invoke(t, r, "gmail", map[string]any{"action": "read", "id": "html"}).Output
	if !strings.Contains(out, "Hola \\u0026 adiós 😀") || strings.Contains(out, "secret()") {
		t.Error(out)
	}
	tl, _ := r.Get("gmail")
	for _, a := range []string{`{"action":"send","confirmed":true,"to":["a@example.test"],"subject":"bad\r\nBcc: b@example.test"}`, `{"action":"send","confirmed":true,"to":["bad"]}`, `{"action":"read"}`, `{"action":"unknown"}`, `{"action":"search","unknown":true}`, `{"action":"search"} {}`} {
		if _, err := tl.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(a)}); err == nil {
			t.Error(a)
		}
	}
}

func TestGmailLongHeadersFoldAndDecode(t *testing.T) {
	r, _ := harness(t, func(w http.ResponseWriter, r *http.Request) { respond(w, map[string]string{"id": "sent"}) })
	tl, _ := r.Get("gmail")
	subject := strings.Repeat("Reunión 😀 ", 50)
	raw, _, err := tl.(*googleTool).c.compose(context.Background(), arguments{To: []string{"José <jose@example.test>"}, Subject: subject, Body: "Hello"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
	if err != nil || decoded != subject {
		t.Fatalf("subject round trip: %q %v", decoded, err)
	}
	for _, line := range strings.Split(strings.SplitN(string(raw), "\r\n\r\n", 2)[0], "\r\n") {
		if len(line) > 998 {
			t.Fatalf("header line too long: %d", len(line))
		}
	}
}

func TestGmailExternalTextPart(t *testing.T) {
	calls := 0
	r, _ := harness(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			respond(w, map[string]any{"id": "m1", "payload": map[string]any{"mimeType": "text/plain", "body": map[string]any{"attachmentId": "text1", "size": 100}}})
			return
		}
		checkRequest(t, r, "GET", "/users/me/messages/m1/attachments/text1")
		respond(w, map[string]string{"data": base64.RawURLEncoding.EncodeToString([]byte("External body 😀"))})
	})
	out := invoke(t, r, "gmail", map[string]any{"action": "read", "id": "m1"})
	if !strings.Contains(out.Output, "External body 😀") || calls != 2 {
		t.Error(out, calls)
	}
}

func TestGoogleCancellation(t *testing.T) {
	r, _ := harness(t, func(w http.ResponseWriter, r *http.Request) { t.Error("cancelled request reached server") })
	tl, _ := r.Get("gmail")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tl.Invoke(ctx, tools.Invocation{Arguments: json.RawMessage(`{"action":"search"}`)})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

package googleapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestDriveActions(t *testing.T) {
	for _, action := range []string{"search", "plain_search", "natural_search", "list", "get", "create_folder", "move", "rename", "copy", "share", "share_link", "trash", "delete"} {
		t.Run(action, func(t *testing.T) {
			mutations := 0
			r, _ := harness(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fake-access-token" {
					t.Error("missing auth")
				}
				if r.Method == "GET" && r.URL.Path == "/files" {
					q := r.URL.Query().Get("q")
					want := "name contains 'report'"
					if action == "plain_search" {
						want = `fullText contains 'O\'Brien\\notes'`
					}
					if action == "natural_search" {
						want = "fullText contains 'budget report'"
					}
					if action == "list" {
						want = "'folder' in parents"
					}
					if !strings.Contains(q, want) || !strings.Contains(q, "trashed = false") {
						t.Errorf("query %s want %s", q, want)
					}
					if r.URL.Query().Get("pageSize") != "4" || r.URL.Query().Get("pageToken") != "next" {
						t.Error(r.URL.RawQuery)
					}
					respond(w, map[string]any{"files": []any{map[string]string{"id": "f1", "name": "Report", "mimeType": "text/plain"}}, "nextPageToken": "next2"})
					return
				}
				if r.Method == "GET" && r.URL.Path == "/files/f1" {
					respond(w, map[string]any{"id": "f1", "name": "Report", "mimeType": "text/plain", "parents": []string{"old"}})
					return
				}
				mutations++
				switch action {
				case "create_folder":
					checkRequest(t, r, "POST", "/files")
					v := readJSON(t, r)
					if v["mimeType"] != "application/vnd.google-apps.folder" || v["name"] != "New" {
						t.Error(v)
					}
				case "move":
					checkRequest(t, r, "PATCH", "/files/f1")
					if r.URL.Query().Get("addParents") != "folder" || r.URL.Query().Get("removeParents") != "old" {
						t.Error(r.URL.RawQuery)
					}
					readJSON(t, r)
				case "rename":
					checkRequest(t, r, "PATCH", "/files/f1")
					if readJSON(t, r)["name"] != "New" {
						t.Error("name")
					}
				case "copy":
					checkRequest(t, r, "POST", "/files/f1/copy")
					v := readJSON(t, r)
					if v["name"] != "New" || v["parents"].([]any)[0] != "folder" {
						t.Error(v)
					}
				case "share", "share_link":
					checkRequest(t, r, "POST", "/files/f1/permissions")
					v := readJSON(t, r)
					if v["role"] != "reader" {
						t.Error(v)
					}
					if action == "share" && (v["type"] != "user" || v["emailAddress"] != "friend@example.test") {
						t.Error(v)
					}
					if action == "share_link" && (v["type"] != "anyone" || v["allowFileDiscovery"] != false) {
						t.Error(v)
					}
				case "trash":
					checkRequest(t, r, "PATCH", "/files/f1")
					if readJSON(t, r)["trashed"] != true {
						t.Error("trashed")
					}
				case "delete":
					checkRequest(t, r, "DELETE", "/files/f1")
				default:
					t.Errorf("unexpected mutation %s", r.URL)
				}
				respond(w, map[string]any{"id": "new", "name": "New", "mimeType": "text/plain", "role": "reader"})
			})
			a := map[string]any{"action": action, "id": "f1", "name": "New", "folder_id": "folder", "max_results": 4, "page_token": "next", "query": "name contains 'report'", "confirmed": true, "role": "reader", "email": "friend@example.test"}
			if action == "plain_search" {
				a["action"] = "search"
				a["plain_text"] = true
				a["query"] = "O'Brien\\notes"
			}
			if action == "natural_search" {
				a["action"] = "search"
				a["query"] = "budget report"
			}
			if action == "share_link" {
				a["action"] = "share"
				a["anyone"] = true
				delete(a, "email")
			}
			out := invoke(t, r, "drive", a)
			if out.Output == "" {
				t.Error("empty output")
			}
			if action != "search" && action != "plain_search" && action != "natural_search" && action != "list" && action != "get" && mutations != 1 {
				t.Error("missing mutation", mutations)
			}
		})
	}
}
func TestDriveDownloadExports(t *testing.T) {
	cases := []struct{ kind, format, mime string }{
		{"document", "pdf", "application/pdf"},
		{"document", "docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"document", "markdown", "text/markdown"},
		{"document", "txt", "text/plain"},
		{"spreadsheet", "xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"spreadsheet", "pdf", "application/pdf"},
		{"presentation", "pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
		{"presentation", "pdf", "application/pdf"},
		{"binary", "", "application/pdf"},
	}
	for _, tc := range cases {
		t.Run(tc.kind+tc.format, func(t *testing.T) {
			download := false
			r, _ := harness(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/files/f1" && r.URL.Query().Get("alt") == "" {
					mt := "application/vnd.google-apps." + tc.kind
					if tc.kind == "binary" {
						mt = "application/pdf"
					}
					respond(w, map[string]any{"id": "f1", "name": "Informe", "mimeType": mt})
					return
				}
				download = true
				if tc.kind == "binary" {
					checkRequest(t, r, "GET", "/files/f1")
					if r.URL.Query().Get("alt") != "media" {
						t.Error(r.URL.RawQuery)
					}
				} else {
					checkRequest(t, r, "GET", "/files/f1/export")
					if r.URL.Query().Get("mimeType") != tc.mime {
						t.Error(r.URL.RawQuery)
					}
					if r.URL.Query().Has("supportsAllDrives") {
						t.Error("export does not accept supportsAllDrives")
					}
				}
				_, _ = io.WriteString(w, "downloaded bytes")
			})
			out := invoke(t, r, "drive", map[string]any{"action": "download", "id": "f1", "format": tc.format})
			var v map[string]any
			if err := json.Unmarshal([]byte(out.Output), &v); err != nil {
				t.Fatal(err)
			}
			tl, _ := r.Get("drive")
			data, err := tl.(*googleTool).c.blob(v["digest"].(string))
			if err != nil || string(data) != "downloaded bytes" || !download || v["mime_type"] != tc.mime || !strings.HasPrefix(v["url"].(string), "/blobs/"+v["digest"].(string)+"?mime=") {
				t.Fatal(v, string(data), err)
			}
		})
	}
	if _, _, err := exportType("application/vnd.google-apps.spreadsheet", "docx"); err == nil {
		t.Fatal("invalid spreadsheet export accepted")
	}
}
func TestDriveUpload(t *testing.T) {
	for _, blob := range []bool{false, true} {
		t.Run(fmt.Sprint(blob), func(t *testing.T) {
			called := false
			r, _ := harness(t, func(w http.ResponseWriter, r *http.Request) {
				called = true
				checkRequest(t, r, "POST", "/files")
				if r.URL.Query().Get("uploadType") != "multipart" {
					t.Error(r.URL.RawQuery)
				}
				mt, p, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if err != nil || mt != "multipart/related" {
					t.Fatal(mt, err)
				}
				mr := multipart.NewReader(r.Body, p["boundary"])
				part, err := mr.NextPart()
				if err != nil {
					t.Fatal(err)
				}
				var metadata map[string]any
				if err = json.NewDecoder(part).Decode(&metadata); err != nil {
					t.Fatal(err)
				}
				if metadata["name"] != "Report" || metadata["parents"].([]any)[0] != "folder" {
					t.Error(metadata)
				}
				part, err = mr.NextPart()
				if err != nil {
					t.Fatal(err)
				}
				b, _ := io.ReadAll(part)
				if string(b) != "Hola 😀" {
					t.Error(string(b))
				}
				respond(w, map[string]string{"id": "uploaded", "name": "Report", "mimeType": "text/plain"})
			})
			a := map[string]any{"action": "upload", "name": "Report", "folder_id": "folder", "text": "Hola 😀"}
			if blob {
				tl, _ := r.Get("drive")
				digest, err := tl.(*googleTool).c.saveBlob([]byte("Hola 😀"))
				if err != nil {
					t.Fatal(err)
				}
				a["digest"] = digest
				delete(a, "text")
			}
			invoke(t, r, "drive", a)
			if !called {
				t.Error("no upload")
			}
		})
	}
}
func docFixture() map[string]any {
	return map[string]any{"documentId": "doc1", "title": "Notes", "revisionId": "rev1", "body": map[string]any{"content": []any{
		map[string]any{"startIndex": 1, "endIndex": 9, "paragraph": map[string]any{"paragraphStyle": map[string]string{"namedStyleType": "HEADING_1"}, "elements": []any{map[string]any{"textRun": map[string]string{"content": "Section\n"}}}}},
		map[string]any{"startIndex": 9, "endIndex": 20, "paragraph": map[string]any{"elements": []any{map[string]any{"textRun": map[string]string{"content": "Hola 😀\n"}}}}},
	}}}
}
func TestDocsActions(t *testing.T) {
	for _, action := range []string{"read", "create", "append", "insert", "heading", "replace_text"} {
		t.Run(action, func(t *testing.T) {
			batches := 0
			r, _ := harness(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					checkRequest(t, r, "GET", "/documents/doc1")
					if r.URL.Query().Get("includeTabsContent") != "true" {
						t.Error(r.URL.RawQuery)
					}
					respond(w, docFixture())
					return
				}
				if r.URL.Path == "/documents" {
					checkRequest(t, r, "POST", "/documents")
					if readJSON(t, r)["title"] != "Notes" {
						t.Error("title")
					}
					respond(w, map[string]string{"documentId": "doc1", "title": "Notes"})
					return
				}
				checkRequest(t, r, "POST", "/documents/doc1:batchUpdate")
				v := readJSON(t, r)
				batches++
				requests := v["requests"].([]any)
				req := requests[0].(map[string]any)
				if action == "replace_text" {
					p := req["replaceAllText"].(map[string]any)
					if p["replaceText"] != "Nuevo 😀" || p["containsText"].(map[string]any)["text"] != "Old" {
						t.Error(p)
					}
					respond(w, map[string]any{"replies": []any{map[string]any{"replaceAllText": map[string]int{"occurrencesChanged": 2}}}})
					return
				}
				insert := req["insertText"].(map[string]any)
				if insert["text"] != "New 😀\n" {
					t.Error(insert)
				}
				if action == "heading" {
					if insert["location"].(map[string]any)["index"] != float64(9) || v["writeControl"].(map[string]any)["requiredRevisionId"] != "rev1" {
						t.Error(v)
					}
				} else {
					if _, ok := insert["endOfSegmentLocation"]; !ok {
						t.Error(insert)
					}
				}
				respond(w, map[string]any{})
			})
			a := map[string]any{"action": action, "id": "doc1", "title": "Notes", "text": "New 😀\n", "find": "Old", "replacement": "Nuevo 😀", "confirmed": true}
			if action == "heading" {
				a["action"] = "insert"
				a["after_heading"] = "Section"
			}
			out := invoke(t, r, "docs", a).Output
			if action == "read" {
				if !strings.Contains(out, "# Section") || !strings.Contains(out, "Hola 😀") {
					t.Error(out)
				}
			} else if batches != 1 {
				t.Error(batches)
			}
			if action == "replace_text" && !strings.Contains(out, `"occurrences_changed":2`) {
				t.Error(out)
			}
		})
	}
}
func TestDocsTabsAndMissingHeading(t *testing.T) {
	r, _ := harness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("unexpected mutation")
		}
		d := docFixture()
		d["tabs"] = []any{map[string]any{"tabProperties": map[string]string{"tabId": "tab1", "title": "Tab One"}, "documentTab": map[string]any{"body": d["body"]}}}
		delete(d, "body")
		respond(w, d)
	})
	out := invoke(t, r, "docs", map[string]any{"action": "read", "id": "doc1"}).Output
	if !strings.Contains(out, "# Section") {
		t.Error(out)
	}
	tl, _ := r.Get("docs")
	_, err := tl.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"action":"insert","id":"doc1","text":"insert","after_heading":"Missing"}`)})
	if err == nil || !strings.Contains(err.Error(), "found 0") {
		t.Fatal(err)
	}
}
func TestHTTPFailuresNeverExposeCredentials(t *testing.T) {
	for _, code := range []int{401, 403, 429, 500, 302} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			r, _ := harness(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "/other")
				w.WriteHeader(code)
				_, _ = io.WriteString(w, "fake-access-token secret server details")
			})
			tl, _ := r.Get("drive")
			out, err := tl.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"action":"get","id":"f1"}`)})
			if err == nil || strings.Contains(out.Output, "fake-access-token") || strings.Contains(out.Output, "secret") {
				t.Fatal(out, err)
			}
			if code == 401 && !strings.Contains(out.Output, "Reconnect") {
				t.Error(out.Output)
			}
		})
	}
}
func TestRegistrationProfiles(t *testing.T) {
	r := tools.NewMapRegistry()
	if err := RegisterTools(r, nil, ToolOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"gmail", "drive", "docs"} {
		tl, err := r.Get(name)
		if err != nil {
			t.Fatal(err)
		}
		d := tl.Definition()
		if len(d.Profiles) != 1 || d.Profiles[0] != "default" {
			t.Error(d.Profiles)
		}
		if err = d.Validate(); err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err = json.Unmarshal(d.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
	}
	if err := RegisterTools(r, nil, ToolOptions{}); err == nil {
		t.Fatal("duplicate registration accepted")
	}
}

func TestDocsInsertAfterFinalHeadingInTab(t *testing.T) {
	r, _ := harness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			d := docFixture()
			body := d["body"].(map[string]any)
			body["content"] = body["content"].([]any)[:1]
			d["tabs"] = []any{map[string]any{"tabProperties": map[string]string{"tabId": "tab1"}, "documentTab": map[string]any{"body": body}}}
			delete(d, "body")
			respond(w, d)
			return
		}
		v := readJSON(t, r)
		insert := v["requests"].([]any)[0].(map[string]any)["insertText"].(map[string]any)
		location := insert["location"].(map[string]any)
		if location["tabId"] != "tab1" || location["index"] != float64(8) || insert["text"] != "\nNew text" {
			t.Errorf("invalid final-paragraph insertion: %v", v)
		}
		respond(w, map[string]any{})
	})
	invoke(t, r, "docs", map[string]any{"action": "insert", "id": "doc1", "after_heading": "Section", "text": "New text"})
}

func TestDocsCreatePartialFailureRetainsDocumentID(t *testing.T) {
	r, _ := harness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/documents" {
			respond(w, map[string]string{"documentId": "created"})
			return
		}
		w.WriteHeader(403)
	})
	tl, _ := r.Get("docs")
	out, err := tl.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"action":"create","title":"Title","text":"Body"}`)})
	if err == nil || !strings.Contains(out.Output, "created document created") {
		t.Fatal(out, err)
	}
}

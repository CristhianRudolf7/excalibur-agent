package googleapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"regexp"
	"strings"
)

type driveFile struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	MIMEType     string   `json:"mimeType"`
	Size         string   `json:"size,omitempty"`
	Parents      []string `json:"parents,omitempty"`
	WebViewLink  string   `json:"webViewLink,omitempty"`
	ModifiedTime string   `json:"modifiedTime,omitempty"`
	Trashed      bool     `json:"trashed,omitempty"`
}

const fileFields = "id,name,mimeType,size,parents,webViewLink,modifiedTime,trashed"

// Recognize Drive's field/operator grammar; ordinary text becomes fullText.
var driveQuerySyntax = regexp.MustCompile(`(?i)(\b(?:name|fullText|mimeType|modifiedTime|createdTime|viewedByMeTime|trashed|starred|sharedWithMe|visibility|properties|appProperties)\s*(?:[!<>=]+|contains\b|has\b)|(?:'[^']*'|"[^"]*")\s+in\s+(?:parents|owners|writers|readers)|^\s*sharedWithMe\s*$)`)

func driveQuote(s string) string {
	return "'" + strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(s) + "'"
}
func (c *client) file(ctx context.Context, id string) (driveFile, error) {
	var f driveFile
	err := c.json(ctx, "GET", endpoint(c.opts.DriveBaseURL, "/files/"+url.PathEscape(id), url.Values{"fields": {fileFields}, "supportsAllDrives": {"true"}}), nil, &f)
	return f, err
}

var exportFormats = map[string]struct{ mime, ext string }{
	"pdf":      {"application/pdf", "pdf"},
	"docx":     {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "docx"},
	"xlsx":     {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "xlsx"},
	"pptx":     {"application/vnd.openxmlformats-officedocument.presentationml.presentation", "pptx"},
	"markdown": {"text/markdown", "md"}, "md": {"text/markdown", "md"}, "txt": {"text/plain", "txt"},
}

func exportType(mimeType, format string) (string, string, error) {
	if format == "" {
		format = "pdf"
	}
	v, ok := exportFormats[format]
	if !ok {
		return "", "", fmt.Errorf("drive: unsupported export format %q", format)
	}
	allowed := map[string][]string{
		"application/vnd.google-apps.document":     {"pdf", "docx", "markdown", "md", "txt"},
		"application/vnd.google-apps.spreadsheet":  {"pdf", "xlsx"},
		"application/vnd.google-apps.presentation": {"pdf", "pptx", "txt"},
	}
	for _, f := range allowed[mimeType] {
		if f == format {
			return v.mime, v.ext, nil
		}
	}
	return "", "", fmt.Errorf("drive: %s cannot be exported as %s", mimeType, format)
}
func (c *client) drive(ctx context.Context, a arguments) (any, error) {
	base := c.opts.DriveBaseURL
	path := "/files/" + url.PathEscape(a.ID)
	switch a.Action {
	case "search", "list":
		query := a.Query
		if a.Action == "list" {
			folder := a.FolderID
			if folder == "" {
				folder = "root"
			}
			query = driveQuote(folder) + " in parents"
		} else if query != "" && (a.PlainText || !driveQuerySyntax.MatchString(query)) {
			query = "fullText contains " + driveQuote(query)
		}
		if query != "" {
			query = "(" + query + ") and trashed = false"
		} else {
			query = "trashed = false"
		}
		q := url.Values{"q": {query}, "pageSize": {fmt.Sprint(limit(a.MaxResults))}, "fields": {"nextPageToken,files(" + fileFields + ")"}, "includeItemsFromAllDrives": {"true"}, "supportsAllDrives": {"true"}}
		if a.PageToken != "" {
			q.Set("pageToken", a.PageToken)
		}
		var out struct {
			Files []driveFile `json:"files"`
			Next  string      `json:"nextPageToken,omitempty"`
		}
		err := c.json(ctx, "GET", endpoint(base, "/files", q), nil, &out)
		return out, err
	case "upload":
		return c.upload(ctx, a)
	case "create_folder":
		if err := required(a.Name, "name"); err != nil {
			return nil, err
		}
		in := map[string]any{"name": a.Name, "mimeType": "application/vnd.google-apps.folder"}
		if a.FolderID != "" {
			in["parents"] = []string{a.FolderID}
		}
		var out driveFile
		err := c.json(ctx, "POST", endpoint(base, "/files", url.Values{"fields": {fileFields}, "supportsAllDrives": {"true"}}), in, &out)
		return out, err
	}
	if err := required(a.ID, "id"); err != nil {
		return nil, err
	}
	q := url.Values{"fields": {fileFields}, "supportsAllDrives": {"true"}}
	switch a.Action {
	case "get":
		return c.file(ctx, a.ID)
	case "download":
		if c.opts.Blobs == nil {
			return nil, fmt.Errorf("drive: daemon blob storage unavailable")
		}
		f, err := c.file(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		mimeType, name := f.MIMEType, f.Name
		download := endpoint(base, path, url.Values{"alt": {"media"}, "supportsAllDrives": {"true"}})
		if strings.HasPrefix(f.MIMEType, "application/vnd.google-apps.") {
			mt, ext, err := exportType(f.MIMEType, strings.ToLower(a.Format))
			if err != nil {
				return nil, err
			}
			mimeType = mt
			name += "." + ext
			download = endpoint(base, path+"/export", url.Values{"mimeType": {mt}})
		}
		b, err := c.request(ctx, "GET", download, "", nil)
		if err != nil {
			return nil, err
		}
		digest, err := c.saveBlob(b)
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": f.ID, "name": name, "mime_type": mimeType, "size": len(b), "digest": digest, "url": "/blobs/" + digest + "?mime=" + url.QueryEscape(mimeType)}, nil
	case "move", "rename", "copy", "trash":
		method := "PATCH"
		in := map[string]any{}
		switch a.Action {
		case "move":
			if err := required(a.FolderID, "folder_id"); err != nil {
				return nil, err
			}
			f, err := c.file(ctx, a.ID)
			if err != nil {
				return nil, err
			}
			if len(f.Parents) == 1 && f.Parents[0] == a.FolderID {
				return f, nil
			}
			q.Set("addParents", a.FolderID)
			if len(f.Parents) > 0 {
				q.Set("removeParents", strings.Join(f.Parents, ","))
			}
		case "rename":
			if err := required(a.Name, "name"); err != nil {
				return nil, err
			}
			in["name"] = a.Name
		case "copy":
			method = "POST"
			path += "/copy"
			if a.Name != "" {
				in["name"] = a.Name
			}
			if a.FolderID != "" {
				in["parents"] = []string{a.FolderID}
			}
		case "trash":
			in["trashed"] = true
		}
		var out driveFile
		err := c.json(ctx, method, endpoint(base, path, q), in, &out)
		return out, err
	case "delete":
		q.Del("fields")
		err := c.json(ctx, "DELETE", endpoint(base, path, q), nil, nil)
		return map[string]any{"id": a.ID, "deleted": err == nil}, err
	case "share":
		switch a.Role {
		case "reader", "commenter", "writer":
		default:
			return nil, fmt.Errorf("drive: role must be reader, commenter or writer")
		}
		in := map[string]any{"role": a.Role, "type": "user"}
		if a.Anyone {
			if a.Email != "" {
				return nil, fmt.Errorf("drive: choose email or anyone, not both")
			}
			in["type"] = "anyone"
			in["allowFileDiscovery"] = false
		} else {
			if err := required(a.Email, "email"); err != nil {
				return nil, err
			}
			in["emailAddress"] = a.Email
		}
		var out struct {
			ID    string `json:"id"`
			Type  string `json:"type"`
			Role  string `json:"role"`
			Email string `json:"emailAddress,omitempty"`
		}
		err := c.json(ctx, "POST", endpoint(base, path+"/permissions", url.Values{"fields": {"id,type,role,emailAddress"}, "supportsAllDrives": {"true"}}), in, &out)
		return out, err
	}
	return nil, fmt.Errorf("drive: unknown action %q", a.Action)
}
func (c *client) upload(ctx context.Context, a arguments) (any, error) {
	if err := required(a.Name, "name"); err != nil {
		return nil, err
	}
	if a.Digest != "" && a.Text != "" {
		return nil, fmt.Errorf("drive: choose text or digest")
	}
	data := []byte(a.Text)
	var err error
	if a.Digest != "" {
		data, err = c.blob(a.Digest)
		if err != nil {
			return nil, err
		}
	}
	if len(data) > maxData {
		return nil, fmt.Errorf("drive: upload exceeds 25 MiB")
	}
	mt := a.MIMEType
	if mt == "" {
		mt = "text/plain; charset=utf-8"
	}
	if safeHeader(mt) != nil {
		return nil, fmt.Errorf("drive: invalid MIME type")
	}
	metadata := map[string]any{"name": a.Name}
	if a.FolderID != "" {
		metadata["parents"] = []string{a.FolderID}
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	p, err := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/json; charset=UTF-8"}})
	if err != nil {
		return nil, err
	}
	if err = json.NewEncoder(p).Encode(metadata); err != nil {
		return nil, err
	}
	p, err = mw.CreatePart(textproto.MIMEHeader{"Content-Type": {mt}})
	if err != nil {
		return nil, err
	}
	if _, err = p.Write(data); err != nil {
		return nil, err
	}
	if err = mw.Close(); err != nil {
		return nil, err
	}
	b, err := c.request(ctx, "POST", endpoint(c.opts.DriveUploadBaseURL, "/files", url.Values{"uploadType": {"multipart"}, "fields": {fileFields}, "supportsAllDrives": {"true"}}), "multipart/related; boundary="+mw.Boundary(), &body)
	if err != nil {
		return nil, err
	}
	var out driveFile
	if json.Unmarshal(b, &out) != nil {
		return nil, fmt.Errorf("drive: invalid upload response")
	}
	return out, nil
}

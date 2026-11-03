package googleapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/html"
)

type gmailHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type gmailPart struct {
	MIMEType string        `json:"mimeType"`
	Filename string        `json:"filename"`
	Headers  []gmailHeader `json:"headers"`
	Body     struct {
		Data         string `json:"data"`
		Size         int    `json:"size"`
		AttachmentID string `json:"attachmentId"`
	} `json:"body"`
	Parts []gmailPart `json:"parts"`
}
type gmailMessage struct {
	ID       string    `json:"id"`
	ThreadID string    `json:"threadId"`
	Snippet  string    `json:"snippet"`
	LabelIDs []string  `json:"labelIds"`
	Payload  gmailPart `json:"payload"`
}

func (m gmailMessage) header(key string) string {
	for _, h := range m.Payload.Headers {
		if strings.EqualFold(h.Name, key) {
			s, err := new(mime.WordDecoder).DecodeHeader(h.Value)
			if err == nil {
				return s
			}
			return h.Value
		}
	}
	return ""
}
func (m gmailMessage) summary() map[string]any {
	return map[string]any{"id": m.ID, "thread_id": m.ThreadID, "from": m.header("From"), "to": m.header("To"), "subject": m.header("Subject"), "date": m.header("Date"), "snippet": truncate(m.Snippet, 500), "labels": m.LabelIDs}
}
func (c *client) message(ctx context.Context, id, format string) (gmailMessage, error) {
	var m gmailMessage
	err := c.json(ctx, "GET", endpoint(c.opts.GmailBaseURL, "/users/me/messages/"+url.PathEscape(id), url.Values{"format": {format}}), nil, &m)
	return m, err
}
func (c *client) gmail(ctx context.Context, a arguments) (any, error) {
	base := c.opts.GmailBaseURL
	path := "/users/me/messages/" + url.PathEscape(a.ID)
	switch a.Action {
	case "search":
		q := url.Values{"q": {a.Query}, "maxResults": {fmt.Sprint(limit(a.MaxResults))}}
		if a.PageToken != "" {
			q.Set("pageToken", a.PageToken)
		}
		var list struct {
			Messages []gmailMessage `json:"messages"`
			Next     string         `json:"nextPageToken"`
		}
		if err := c.json(ctx, "GET", endpoint(base, "/users/me/messages", q), nil, &list); err != nil {
			return nil, err
		}
		out := []map[string]any{}
		for _, v := range list.Messages {
			m, err := c.message(ctx, v.ID, "metadata")
			if err != nil {
				return nil, err
			}
			out = append(out, m.summary())
		}
		return map[string]any{"messages": out, "next_page_token": list.Next}, nil
	case "read":
		var messages []gmailMessage
		if a.ThreadID != "" {
			var thread struct {
				Messages []gmailMessage `json:"messages"`
			}
			if err := c.json(ctx, "GET", endpoint(base, "/users/me/threads/"+url.PathEscape(a.ThreadID), url.Values{"format": {"full"}}), nil, &thread); err != nil {
				return nil, err
			}
			messages = thread.Messages
		} else {
			if err := required(a.ID, "id or thread_id"); err != nil {
				return nil, err
			}
			m, err := c.message(ctx, a.ID, "full")
			if err != nil {
				return nil, err
			}
			messages = []gmailMessage{m}
		}
		out := []map[string]any{}
		budget := 12000
		for _, m := range messages {
			if err := c.hydrateText(ctx, m.ID, &m.Payload); err != nil {
				return nil, err
			}
			plain, htmlBody, attachments, err := decodeParts(m.Payload)
			if err != nil {
				return nil, err
			}
			if plain == "" {
				plain = stripHTML(htmlBody)
			}
			v := m.summary()
			v["body"] = truncate(plain, max(budget, 0))
			budget -= len([]rune(plain))
			v["attachments"] = attachments
			out = append(out, v)
			if len(out) >= 100 {
				break
			}
		}
		return map[string]any{"messages": out, "truncated": len(messages) > len(out) || budget < 0}, nil
	case "draft", "send":
		if a.Action == "send" && a.DraftID != "" {
			var out map[string]any
			err := c.json(ctx, "POST", endpoint(base, "/users/me/drafts/send", nil), map[string]string{"id": a.DraftID}, &out)
			return compactMessage(out), err
		}
		raw, thread, err := c.compose(ctx, a)
		if err != nil {
			return nil, err
		}
		msg := map[string]any{"raw": base64.RawURLEncoding.EncodeToString(raw)}
		if thread != "" {
			msg["threadId"] = thread
		}
		var out map[string]any
		if a.Action == "draft" {
			method, p := "POST", "/users/me/drafts"
			if a.DraftID != "" {
				method = "PUT"
				p += "/" + url.PathEscape(a.DraftID)
			}
			err = c.json(ctx, method, endpoint(base, p, nil), map[string]any{"message": msg}, &out)
		} else {
			err = c.json(ctx, "POST", endpoint(base, "/users/me/messages/send", nil), msg, &out)
		}
		return compactMessage(out), err
	case "labels":
		var out any
		method := "GET"
		var in any
		if a.Name != "" {
			method = "POST"
			in = map[string]string{"name": a.Name, "labelListVisibility": "labelShow", "messageListVisibility": "show"}
		}
		err := c.json(ctx, method, endpoint(base, "/users/me/labels", nil), in, &out)
		return out, err
	case "modify", "archive", "trash", "untrash", "delete":
		if err := required(a.ID, "id"); err != nil {
			return nil, err
		}
		method := "POST"
		var in any
		switch a.Action {
		case "modify", "archive":
			add, remove := append([]string{}, a.AddLabels...), append([]string{}, a.RemoveLabels...)
			if a.Action == "archive" {
				remove = append(remove, "INBOX")
			}
			if a.Read != nil {
				if *a.Read {
					remove = append(remove, "UNREAD")
				} else {
					add = append(add, "UNREAD")
				}
			}
			if a.Starred != nil {
				if *a.Starred {
					add = append(add, "STARRED")
				} else {
					remove = append(remove, "STARRED")
				}
			}
			for _, label := range add {
				for _, r := range remove {
					if label == r {
						return nil, fmt.Errorf("label %s cannot be both added and removed", label)
					}
				}
			}
			in = map[string]any{"addLabelIds": add, "removeLabelIds": remove}
			path += "/modify"
		case "delete":
			method = "DELETE"
		default:
			path += "/" + a.Action
		}
		if err := c.json(ctx, method, endpoint(base, path, nil), in, nil); err != nil {
			return nil, err
		}
		return map[string]any{"id": a.ID, "action": a.Action, "ok": true}, nil
	}
	return nil, fmt.Errorf("gmail: unknown action %q", a.Action)
}
func compactMessage(in map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range []string{"id", "threadId", "labelIds"} {
		if v, ok := in[k]; ok {
			out[k] = v
		}
	}
	if msg, ok := in["message"].(map[string]any); ok {
		out["message"] = compactMessage(msg)
	}
	return out
}

// Gmail can store large text parts in the attachment endpoint even without a
// filename. Fetch those as body text; named attachments remain metadata only.
func (c *client) hydrateText(ctx context.Context, id string, p *gmailPart) error {
	if p.Filename == "" && (p.MIMEType == "text/plain" || p.MIMEType == "text/html") && p.Body.Data == "" && p.Body.AttachmentID != "" {
		var body struct {
			Data string `json:"data"`
		}
		if err := c.json(ctx, "GET", endpoint(c.opts.GmailBaseURL, "/users/me/messages/"+url.PathEscape(id)+"/attachments/"+url.PathEscape(p.Body.AttachmentID), nil), nil, &body); err != nil {
			return err
		}
		p.Body.Data = body.Data
	}
	for i := range p.Parts {
		if err := c.hydrateText(ctx, id, &p.Parts[i]); err != nil {
			return err
		}
	}
	return nil
}

type messageAttachment struct {
	Name     string `json:"name"`
	Size     int    `json:"size"`
	MIMEType string `json:"mime_type"`
	ID       string `json:"attachment_id,omitempty"`
}

func decodeParts(p gmailPart) (string, string, []messageAttachment, error) {
	attachments := []messageAttachment{}
	if p.Filename != "" || p.Body.AttachmentID != "" && p.MIMEType != "text/plain" && p.MIMEType != "text/html" {
		return "", "", []messageAttachment{{p.Filename, p.Body.Size, p.MIMEType, p.Body.AttachmentID}}, nil
	}
	plain, htmlBody := "", ""
	if p.Body.Data != "" {
		data, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(p.Body.Data, "="))
		if err != nil {
			return "", "", nil, fmt.Errorf("gmail: invalid message body encoding")
		}
		if p.MIMEType == "text/plain" {
			plain = string(data)
		}
		if p.MIMEType == "text/html" {
			htmlBody = string(data)
		}
	}
	for _, part := range p.Parts {
		text, h, files, err := decodeParts(part)
		if err != nil {
			return "", "", nil, err
		}
		plain += text
		htmlBody += h
		attachments = append(attachments, files...)
	}
	return plain, htmlBody, attachments, nil
}
func stripHTML(s string) string {
	root, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return ""
	}
	var out strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		if n.Type == html.TextNode {
			out.WriteString(n.Data)
		}
		if n.Type == html.ElementNode && n.Data == "br" {
			out.WriteByte('\n')
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if n.Type == html.ElementNode && (n.Data == "p" || n.Data == "div" || n.Data == "li") {
			out.WriteByte('\n')
		}
	}
	walk(root)
	return strings.TrimSpace(out.String())
}
func safeHeader(s string) error {
	if strings.ContainsAny(s, "\r\n") {
		return fmt.Errorf("gmail: header must not contain newlines")
	}
	return nil
}
func addressHeader(values []string) (string, error) {
	out := []string{}
	for _, s := range values {
		if err := safeHeader(s); err != nil {
			return "", err
		}
		a, err := mail.ParseAddress(s)
		if err != nil {
			return "", fmt.Errorf("gmail: invalid email address")
		}
		out = append(out, a.String())
	}
	return strings.Join(out, ", "), nil
}
func (c *client) compose(ctx context.Context, a arguments) ([]byte, string, error) {
	if len(a.To) == 0 {
		return nil, "", fmt.Errorf("gmail: to is required")
	}
	if len(a.Body) > maxData {
		return nil, "", fmt.Errorf("gmail: body exceeds 25 MiB")
	}
	headers := [][2]string{{"MIME-Version", "1.0"}, {"Date", time.Now().Format(time.RFC1123Z)}, {"Message-ID", "<" + uuid.NewString() + "@excalibur.local>"}}
	for _, v := range []struct {
		key    string
		values []string
	}{{"To", a.To}, {"Cc", a.CC}, {"Bcc", a.BCC}} {
		if len(v.values) > 0 {
			s, err := addressHeader(v.values)
			if err != nil {
				return nil, "", err
			}
			headers = append(headers, [2]string{v.key, s})
		}
	}
	if email := c.p.Status().Email; email != "" {
		s, err := addressHeader([]string{email})
		if err != nil {
			return nil, "", err
		}
		headers = append(headers, [2]string{"From", s})
	}
	thread := a.ThreadID
	if thread != "" && a.ReplyToID == "" {
		return nil, "", fmt.Errorf("gmail: reply_to_id is required when using thread_id")
	}
	if a.ReplyToID != "" {
		parent, err := c.message(ctx, a.ReplyToID, "metadata")
		if err != nil {
			return nil, "", err
		}
		if thread != "" && thread != parent.ThreadID {
			return nil, "", fmt.Errorf("gmail: reply message belongs to another thread")
		}
		thread = parent.ThreadID
		mid := parent.header("Message-ID")
		if mid == "" {
			return nil, "", fmt.Errorf("gmail: reply parent has no Message-ID")
		}
		if a.Subject == "" {
			a.Subject = parent.header("Subject")
		}
		refs := strings.TrimSpace(parent.header("References") + " " + mid)
		headers = append(headers, [2]string{"In-Reply-To", mid}, [2]string{"References", refs})
	}
	if err := safeHeader(a.Subject); err != nil {
		return nil, "", err
	}
	headers = append(headers, [2]string{"Subject", mime.QEncoding.Encode("utf-8", a.Subject)})
	var body bytes.Buffer
	if len(a.Attachments) == 0 {
		headers = append(headers, [2]string{"Content-Type", "text/plain; charset=utf-8"}, [2]string{"Content-Transfer-Encoding", "quoted-printable"})
		w := quotedprintable.NewWriter(&body)
		if _, err := io.WriteString(w, a.Body); err != nil {
			return nil, "", err
		}
		if err := w.Close(); err != nil {
			return nil, "", err
		}
	} else {
		mw := multipart.NewWriter(&body)
		headers = append(headers, [2]string{"Content-Type", mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": mw.Boundary()})})
		part, err := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/plain; charset=utf-8"}, "Content-Transfer-Encoding": {"quoted-printable"}})
		if err != nil {
			return nil, "", err
		}
		w := quotedprintable.NewWriter(part)
		if _, err = w.Write([]byte(a.Body)); err != nil {
			return nil, "", err
		}
		if err = w.Close(); err != nil {
			return nil, "", err
		}
		for _, f := range a.Attachments {
			if err := required(f.Name, "attachment name"); err != nil {
				return nil, "", err
			}
			data, err := c.blob(f.Digest)
			if err != nil {
				return nil, "", err
			}
			mt := f.MIMEType
			if mt == "" {
				mt = "application/octet-stream"
			}
			if _, _, err := mime.ParseMediaType(mt); err != nil {
				return nil, "", fmt.Errorf("gmail: invalid attachment MIME type")
			}
			if safeHeader(mt) != nil || safeHeader(f.Name) != nil {
				return nil, "", fmt.Errorf("gmail: invalid attachment header")
			}
			part, err := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {mt}, "Content-Disposition": {mime.FormatMediaType("attachment", map[string]string{"filename": f.Name})}, "Content-Transfer-Encoding": {"base64"}})
			if err != nil {
				return nil, "", err
			}
			encoded := base64.StdEncoding.EncodeToString(data)
			for len(encoded) > 0 {
				n := min(76, len(encoded))
				if _, err := io.WriteString(part, encoded[:n]+"\r\n"); err != nil {
					return nil, "", err
				}
				encoded = encoded[n:]
			}
			if body.Len() > maxData {
				return nil, "", fmt.Errorf("gmail: MIME message exceeds 25 MiB")
			}
		}
		if err := mw.Close(); err != nil {
			return nil, "", err
		}
	}
	var out bytes.Buffer
	for _, h := range headers {
		if err := safeHeader(h[1]); err != nil {
			return nil, "", err
		}
		if err := writeHeader(&out, h[0], h[1]); err != nil {
			return nil, "", err
		}
	}
	out.WriteString("\r\n")
	out.Write(body.Bytes())
	return out.Bytes(), thread, nil
}

// Fold at whitespace to keep encoded words and mailbox tokens intact. RFC 5322
// permits folding whitespace inside quoted display names as well as between
// encoded words. No physical header line may exceed 998 octets.
func writeHeader(w io.Writer, name, value string) error {
	line := name + ": " + value
	for len(line) > 78 {
		cut := strings.LastIndexByte(line[:79], ' ')
		if cut <= 1 {
			next := strings.IndexByte(line[79:], ' ')
			if next < 0 {
				break
			}
			cut = 79 + next
		}
		if cut > 998 {
			return fmt.Errorf("gmail: header token exceeds RFC line limit")
		}
		if _, err := io.WriteString(w, line[:cut]+"\r\n"); err != nil {
			return err
		}
		line = " " + line[cut+1:]
	}
	if len(line) > 998 {
		return fmt.Errorf("gmail: header token exceeds RFC line limit")
	}
	_, err := io.WriteString(w, line+"\r\n")
	return err
}

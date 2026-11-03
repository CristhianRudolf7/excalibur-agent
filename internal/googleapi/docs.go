package googleapi

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type docElement struct {
	StartIndex int `json:"startIndex"`
	EndIndex   int `json:"endIndex"`
	Paragraph  *struct {
		Elements []struct {
			TextRun *struct {
				Content string `json:"content"`
			} `json:"textRun"`
		} `json:"elements"`
		ParagraphStyle struct {
			NamedStyleType string `json:"namedStyleType"`
		} `json:"paragraphStyle"`
	} `json:"paragraph"`
	Table *struct {
		Rows []struct {
			Cells []struct {
				Content []docElement `json:"content"`
			} `json:"tableCells"`
		} `json:"tableRows"`
	} `json:"table"`
	TableOfContents *struct {
		Content []docElement `json:"content"`
	} `json:"tableOfContents"`
}
type docTab struct {
	Properties struct {
		ID    string `json:"tabId"`
		Title string `json:"title"`
	} `json:"tabProperties"`
	DocumentTab struct {
		Body struct {
			Content []docElement `json:"content"`
		} `json:"body"`
	} `json:"documentTab"`
	Children []docTab `json:"childTabs"`
}
type document struct {
	ID       string `json:"documentId"`
	Title    string `json:"title"`
	Revision string `json:"revisionId"`
	Body     struct {
		Content []docElement `json:"content"`
	} `json:"body"`
	Tabs []docTab `json:"tabs"`
}

func (c *client) document(ctx context.Context, id string) (document, error) {
	var d document
	err := c.json(ctx, "GET", endpoint(c.opts.DocsBaseURL, "/documents/"+url.PathEscape(id), url.Values{"includeTabsContent": {"true"}}), nil, &d)
	return d, err
}
func paragraphText(e docElement) string {
	var b strings.Builder
	if e.Paragraph != nil {
		for _, v := range e.Paragraph.Elements {
			if v.TextRun != nil {
				b.WriteString(v.TextRun.Content)
			}
		}
	}
	return b.String()
}
func docMarkdown(elements []docElement) string {
	var b strings.Builder
	for _, e := range elements {
		if e.Paragraph != nil {
			text := paragraphText(e)
			style := e.Paragraph.ParagraphStyle.NamedStyleType
			if strings.HasPrefix(style, "HEADING_") {
				n, err := strconv.Atoi(strings.TrimPrefix(style, "HEADING_"))
				if err == nil && n >= 1 && n <= 6 {
					b.WriteString(strings.Repeat("#", n) + " ")
				}
			}
			b.WriteString(text)
		}
		if e.Table != nil {
			for _, r := range e.Table.Rows {
				for i, cell := range r.Cells {
					if i > 0 {
						b.WriteString(" | ")
					}
					b.WriteString(strings.TrimSpace(docMarkdown(cell.Content)))
				}
				b.WriteString("\n")
			}
		}
		if e.TableOfContents != nil {
			b.WriteString(docMarkdown(e.TableOfContents.Content))
		}
	}
	return b.String()
}
func flatTabs(tabs []docTab) []docTab {
	var out []docTab
	for _, t := range tabs {
		out = append(out, t)
		out = append(out, flatTabs(t.Children)...)
	}
	return out
}
func (c *client) docs(ctx context.Context, a arguments) (any, error) {
	base := c.opts.DocsBaseURL
	if a.Action == "create" {
		if err := required(a.Title, "title"); err != nil {
			return nil, err
		}
		var d document
		if err := c.json(ctx, "POST", endpoint(base, "/documents", nil), map[string]string{"title": a.Title}, &d); err != nil {
			return nil, err
		}
		if a.Text != "" {
			if err := c.batch(ctx, d.ID, []any{map[string]any{"insertText": map[string]any{"text": a.Text, "endOfSegmentLocation": map[string]any{}}}}, ""); err != nil {
				return nil, fmt.Errorf("docs: created document %s but content insertion failed: %w", d.ID, err)
			}
		}
		return map[string]any{"id": d.ID, "title": d.Title, "url": "https://docs.google.com/document/d/" + url.PathEscape(d.ID) + "/edit"}, nil
	}
	if err := required(a.ID, "id"); err != nil {
		return nil, err
	}
	switch a.Action {
	case "read":
		d, err := c.document(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		text := docMarkdown(d.Body.Content)
		tabs := flatTabs(d.Tabs)
		for _, tab := range tabs {
			if len(tabs) > 1 {
				text += "# " + tab.Properties.Title + "\n"
			}
			text += docMarkdown(tab.DocumentTab.Body.Content)
		}
		return map[string]any{"id": d.ID, "title": d.Title, "markdown": truncate(text, 16000), "truncated": len([]rune(text)) > 16000}, nil
	case "append", "insert":
		if err := required(a.Text, "text"); err != nil {
			return nil, err
		}
		insert := map[string]any{"text": a.Text, "endOfSegmentLocation": map[string]any{}}
		revision := ""
		if a.AfterHeading != "" {
			d, err := c.document(ctx, a.ID)
			if err != nil {
				return nil, err
			}
			revision = d.Revision
			type match struct {
				index int
				tab   string
				final bool
			}
			matches := []match{}
			find := func(elements []docElement, tab string) {
				for _, e := range elements {
					if e.Paragraph != nil && strings.HasPrefix(e.Paragraph.ParagraphStyle.NamedStyleType, "HEADING_") && strings.TrimSpace(paragraphText(e)) == strings.TrimSpace(a.AfterHeading) {
						lastEnd := elements[len(elements)-1].EndIndex
						matches = append(matches, match{index: e.EndIndex, tab: tab, final: e.EndIndex >= lastEnd})
					}
				}
			}
			find(d.Body.Content, "")
			for _, tab := range flatTabs(d.Tabs) {
				find(tab.DocumentTab.Body.Content, tab.Properties.ID)
			}
			if len(matches) != 1 {
				return nil, fmt.Errorf("docs: after_heading must match exactly one heading (found %d)", len(matches))
			}
			delete(insert, "endOfSegmentLocation")
			index := matches[0].index
			if matches[0].final {
				index--
				insert["text"] = "\n" + a.Text
			}
			loc := map[string]any{"index": index}
			if matches[0].tab != "" {
				loc["tabId"] = matches[0].tab
			}
			insert["location"] = loc
		}
		if err := c.batch(ctx, a.ID, []any{map[string]any{"insertText": insert}}, revision); err != nil {
			return nil, err
		}
		return map[string]any{"id": a.ID, "action": a.Action, "ok": true}, nil
	case "replace_text":
		if err := required(a.Find, "find"); err != nil {
			return nil, err
		}
		in := map[string]any{"replaceAllText": map[string]any{"containsText": map[string]any{"text": a.Find, "matchCase": true}, "replaceText": a.Replacement}}
		var out struct {
			Replies []struct {
				ReplaceAllText *struct {
					Occurrences int `json:"occurrencesChanged"`
				} `json:"replaceAllText,omitempty"`
			} `json:"replies"`
		}
		err := c.json(ctx, "POST", endpoint(base, "/documents/"+url.PathEscape(a.ID)+":batchUpdate", nil), map[string]any{"requests": []any{in}}, &out)
		count := 0
		for _, r := range out.Replies {
			if r.ReplaceAllText != nil {
				count += r.ReplaceAllText.Occurrences
			}
		}
		return map[string]any{"id": a.ID, "occurrences_changed": count}, err
	}
	return nil, fmt.Errorf("docs: unknown action %q", a.Action)
}
func (c *client) batch(ctx context.Context, id string, requests []any, revision string) error {
	in := map[string]any{"requests": requests}
	if revision != "" {
		in["writeControl"] = map[string]string{"requiredRevisionId": revision}
	}
	return c.json(ctx, "POST", endpoint(c.opts.DocsBaseURL, "/documents/"+url.PathEscape(id)+":batchUpdate", nil), in, nil)
}

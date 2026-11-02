package websearch

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// SourceSearch uses public APIs. PyPI supports exact package lookup only.
func (b *Backend) SourceSearch(ctx context.Context, source, q string, k int) ([]tools.SearchResult, error) {
	source = strings.ToLower(strings.TrimSpace(source))
	known := false
	for _, s := range tools.KnownSources {
		if s == source {
			known = true
		}
	}
	if !known {
		return nil, fmt.Errorf("unknown source %q", source)
	}
	if capabilities.BlockedBenchmarkSource(q) {
		return nil, errors.New("blocked: query names a benchmark source (Section 23.2)")
	}
	ctx, cancel := context.WithTimeout(ctx, b.limits.Timeout)
	defer cancel()
	n := strconv.Itoa(count(k))
	v := url.Values{}
	endpoint := ""
	switch source {
	case "wikipedia":
		endpoint = "https://en.wikipedia.org/w/api.php"
		v = url.Values{"action": {"query"}, "list": {"search"}, "srsearch": {q}, "srlimit": {n}, "format": {"json"}, "utf8": {"1"}}
	case "arxiv":
		endpoint = "https://export.arxiv.org/api/query"
		v = url.Values{"search_query": {"all:" + q}, "start": {"0"}, "max_results": {n}}
	case "hackernews":
		endpoint = "https://hn.algolia.com/api/v1/search"
		v = url.Values{"query": {q}, "hitsPerPage": {n}, "tags": {"story"}}
	case "github":
		endpoint = "https://api.github.com/search/repositories"
		v = url.Values{"q": {q}, "per_page": {n}}
	case "stackoverflow":
		endpoint = "https://api.stackexchange.com/2.3/search/advanced"
		v = url.Values{"q": {q}, "site": {"stackoverflow"}, "pagesize": {n}, "order": {"desc"}, "sort": {"relevance"}}
	case "openalex":
		endpoint = "https://api.openalex.org/works"
		v = url.Values{"search": {q}, "per-page": {n}}
	case "pubmed":
		return b.pubmed(ctx, q, k)
	case "semantic-scholar":
		endpoint = "https://api.semanticscholar.org/graph/v1/paper/search"
		v = url.Values{"query": {q}, "limit": {n}, "fields": {"title,url,abstract,publicationDate"}}
	case "huggingface":
		endpoint = "https://huggingface.co/api/models"
		v = url.Values{"search": {q}, "limit": {n}}
	case "npm":
		endpoint = "https://registry.npmjs.org/-/v1/search"
		v = url.Values{"text": {q}, "size": {n}}
	case "pypi":
		// The JSON API has no search endpoint; never scrape its search UI.
		if q == "" || strings.ContainsAny(q, "/\\?#\x00") {
			return nil, errors.New("websearch: PyPI requires an exact package name")
		}
		endpoint = "https://pypi.org/pypi/" + url.PathEscape(q) + "/json"
	case "crates":
		endpoint = "https://crates.io/api/v1/crates"
		v = url.Values{"q": {q}, "per_page": {n}}
	case "dev":
		endpoint = "https://dev.to/api/articles/search"
		v = url.Values{"q": {q}, "per_page": {n}, "page": {"1"}}
	case "techmeme", "digg", "youtube":
		domain := map[string]string{"techmeme": "techmeme.com", "digg": "digg.com", "youtube": "youtube.com"}[source]
		r, e := b.Search(ctx, q, tools.SearchOptions{K: k, Domains: []string{domain}})
		for i := range r {
			r[i].SourceID = source
		}
		return r, e
	case "polymarket":
		return nil, errors.New("websearch: polymarket not supported without a provider")
	}
	raw, err := b.sourceRequest(ctx, endpoint, v, source == "github")
	if err != nil {
		return nil, err
	}
	if source == "arxiv" {
		var feed struct {
			XMLName xml.Name `xml:"feed"`
			Entries []struct {
				ID        string `xml:"id"`
				Title     string `xml:"title"`
				Summary   string `xml:"summary"`
				Published string `xml:"published"`
			} `xml:"entry"`
		}
		if xml.Unmarshal(raw, &feed) != nil {
			return nil, errors.New("websearch: invalid Atom response")
		}
		out := []tools.SearchResult{}
		for _, e := range feed.Entries {
			out = append(out, tools.SearchResult{URL: e.ID, Title: strings.Join(strings.Fields(e.Title), " "), Snippet: strings.TrimSpace(e.Summary), PublishedAt: e.Published, SourceID: source})
		}
		return filtered(out, k), nil
	}
	var root any
	if json.Unmarshal(raw, &root) != nil {
		return nil, errors.New("websearch: invalid source response")
	}
	obj, _ := root.(map[string]any)
	if obj != nil {
		if obj["error"] != nil || obj["error_id"] != nil || obj["errors"] != nil {
			return nil, errors.New("websearch: source API reported an error")
		}
	}
	path := map[string][]string{"wikipedia": {"query", "search"}, "hackernews": {"hits"}, "github": {"items"}, "stackoverflow": {"items"}, "openalex": {"results"}, "semantic-scholar": {"data"}, "npm": {"objects"}, "crates": {"crates"}}
	var rows []any
	switch source {
	case "huggingface", "dev":
		rows, _ = root.([]any)
	case "pypi":
		if objectAt(obj, "info") == nil {
			return nil, errors.New("websearch: invalid PyPI response")
		}
		rows = []any{obj}
	default:
		rows = arrayAt(obj, path[source]...)
	}
	if rows == nil {
		return nil, errors.New("websearch: invalid source response shape")
	}
	out := []tools.SearchResult{}
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		r := tools.SearchResult{SourceID: source}
		switch source {
		case "wikipedia":
			r.Title = str(m, "title")
			r.URL = "https://en.wikipedia.org/?curid=" + str(m, "pageid")
			r.Snippet = plain(str(m, "snippet"))
			r.PublishedAt = str(m, "timestamp")
		case "hackernews":
			r.Title = str(m, "title")
			r.URL = str(m, "url")
			if r.URL == "" {
				r.URL = "https://news.ycombinator.com/item?id=" + str(m, "objectID")
			}
			r.Snippet = plain(str(m, "story_text"))
			r.PublishedAt = str(m, "created_at")
		case "github":
			r.Title = str(m, "full_name")
			r.URL = str(m, "html_url")
			r.Snippet = str(m, "description")
			r.PublishedAt = str(m, "created_at")
		case "stackoverflow":
			r.Title = html.UnescapeString(str(m, "title"))
			r.URL = str(m, "link")
			r.Snippet = plain(str(m, "excerpt"))
			r.PublishedAt = str(m, "creation_date")
		case "openalex":
			r.Title = str(m, "display_name")
			r.URL = str(m, "doi")
			if r.URL == "" {
				r.URL = str(m, "id")
			}
			r.PublishedAt = str(m, "publication_date")
		case "semantic-scholar":
			r.Title = str(m, "title")
			r.URL = str(m, "url")
			r.Snippet = str(m, "abstract")
			r.PublishedAt = str(m, "publicationDate")
		case "huggingface":
			r.Title = str(m, "id")
			r.URL = "https://huggingface.co/" + r.Title
			r.Snippet = str(m, "pipeline_tag")
			r.PublishedAt = str(m, "lastModified")
		case "npm":
			p := objectAt(m, "package")
			r.Title = str(p, "name")
			r.URL = str(objectAt(p, "links"), "npm")
			r.Snippet = str(p, "description")
			r.PublishedAt = str(p, "date")
		case "pypi":
			p := objectAt(m, "info")
			r.Title = str(p, "name")
			r.URL = str(p, "package_url")
			r.Snippet = "Exact package lookup (PyPI has no public search API). " + str(p, "summary")
		case "crates":
			r.Title = str(m, "id")
			r.URL = "https://crates.io/crates/" + r.Title
			r.Snippet = str(m, "description")
			r.PublishedAt = str(m, "updated_at")
		case "dev":
			r.Title = str(m, "title")
			r.URL = str(m, "url")
			r.Snippet = str(m, "description")
			r.PublishedAt = str(m, "published_at")
		}
		if r.URL != "" {
			out = append(out, r)
		}
	}
	return filtered(out, k), nil
}
func (b *Backend) sourceRequest(ctx context.Context, endpoint string, v url.Values, github bool) ([]byte, error) {
	if len(v) > 0 {
		endpoint += "?" + v.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, errors.New("websearch: invalid source request")
	}
	req.Header.Set("User-Agent", b.userAgent)
	req.Header.Set("Accept", "application/json,application/atom+xml;q=0.9")
	if req.URL.Hostname() == "dev.to" {
		req.Header.Set("Accept", "application/vnd.forem.api-v1+json")
	}
	key := ""
	if github && b.githubKey != nil {
		key, err = b.githubKey(ctx)
		if err != nil {
			return nil, errors.New("websearch: GitHub key resolution failed")
		}
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	resp, err := b.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("websearch: source request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("websearch: source HTTP status %d", resp.StatusCode)
	}
	raw, err := readBounded(resp.Body, b.limits.ResponseBytes)
	if err != nil {
		return nil, err
	}
	if key != "" {
		var payload any
		if json.Unmarshal(raw, &payload) != nil {
			return nil, errors.New("websearch: invalid source response")
		}
		raw, err = json.Marshal(redactJSON(payload, key))
		if err != nil {
			return nil, errors.New("websearch: invalid source response")
		}
	}
	return raw, nil
}
func (b *Backend) pubmed(ctx context.Context, q string, k int) ([]tools.SearchResult, error) {
	base := "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/"
	raw, err := b.sourceRequest(ctx, base+"esearch.fcgi", url.Values{"db": {"pubmed"}, "term": {q}, "retmode": {"json"}, "retmax": {strconv.Itoa(count(k))}}, false)
	if err != nil {
		return nil, err
	}
	var search struct {
		Result *struct {
			IDs []string `json:"idlist"`
		} `json:"esearchresult"`
	}
	if json.Unmarshal(raw, &search) != nil || search.Result == nil || search.Result.IDs == nil {
		return nil, errors.New("websearch: invalid PubMed search response")
	}
	if len(search.Result.IDs) == 0 {
		return []tools.SearchResult{}, nil
	}
	if len(search.Result.IDs) > count(k) {
		search.Result.IDs = search.Result.IDs[:count(k)]
	}
	raw, err = b.sourceRequest(ctx, base+"esummary.fcgi", url.Values{"db": {"pubmed"}, "id": {strings.Join(search.Result.IDs, ",")}, "retmode": {"json"}}, false)
	if err != nil {
		return nil, err
	}
	var summary struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if json.Unmarshal(raw, &summary) != nil {
		return nil, errors.New("websearch: invalid PubMed summary response")
	}
	out := []tools.SearchResult{}
	for _, id := range search.Result.IDs {
		var m struct{ Title, PubDate string }
		if json.Unmarshal(summary.Result[id], &m) != nil {
			return nil, errors.New("websearch: invalid PubMed record")
		}
		out = append(out, tools.SearchResult{Title: m.Title, URL: "https://pubmed.ncbi.nlm.nih.gov/" + url.PathEscape(id) + "/", PublishedAt: m.PubDate, SourceID: "pubmed"})
	}
	return filtered(out, k), nil
}
func objectAt(m map[string]any, key string) map[string]any { o, _ := m[key].(map[string]any); return o }
func arrayAt(m map[string]any, path ...string) []any {
	for i, key := range path {
		if i == len(path)-1 {
			a, _ := m[key].([]any)
			return a
		}
		m = objectAt(m, key)
	}
	return nil
}
func str(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}
func plain(s string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(s))
	if err != nil {
		return ""
	}
	doc.Find("script,style").Remove()
	return strings.TrimSpace(doc.Text())
}

// Redact decoded strings as well as raw text, including JSON-escaped credentials.
func redactJSON(value any, key string) any {
	switch v := value.(type) {
	case string:
		return strings.ReplaceAll(v, key, "[redacted]")
	case []any:
		for i, item := range v {
			v[i] = redactJSON(item, key)
		}
	case map[string]any:
		out := make(map[string]any, len(v))
		for name, item := range v {
			out[strings.ReplaceAll(name, key, "[redacted]")] = redactJSON(item, key)
		}
		return out
	}
	return value
}

package edit

import (
	"encoding/xml"
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/opc"
	"io"
	"regexp"
	"sort"
	"strings"
)

// node records exact byte spans; edits splice original XML rather than marshal it.
type node struct {
	name                    xml.Name
	attrs                   []xml.Attr
	start, open, close, end int
	children                []*node
	text                    string
	parent                  *node
}

func parse(s string) (*node, error) {
	d := xml.NewDecoder(strings.NewReader(s))
	root := &node{}
	cur := root
	depth, tokens := 0, 0
	for {
		start := int(d.InputOffset())
		t, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		end := int(d.InputOffset())
		switch v := t.(type) {
		case xml.StartElement:
			depth++
			tokens++
			if depth > 256 || tokens > 1000000 {
				return nil, fmt.Errorf("XML complexity limit exceeded")
			}
			n := &node{name: v.Name, attrs: v.Attr, start: start, open: end, parent: cur}
			cur.children = append(cur.children, n)
			cur = n
		case xml.EndElement:
			depth--
			cur.close = start
			cur.end = end
			if strings.HasSuffix(strings.TrimSpace(s[cur.start:cur.open]), "/>") {
				cur.close = cur.open
			}
			cur = cur.parent
		case xml.CharData:
			cur.text += string(v)
		}
	}
	if cur != root {
		return nil, fmt.Errorf("unclosed XML")
	}
	return root, nil
}
func (n *node) attr(name string) string {
	for _, a := range n.attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
func (n *node) find(local string) []*node {
	var out []*node
	for _, c := range n.children {
		if c.name.Local == local {
			out = append(out, c)
		}
		out = append(out, c.find(local)...)
	}
	return out
}
func (n *node) content() string {
	s := n.text
	for _, c := range n.children {
		s += c.content()
	}
	return s
}

type patch struct {
	start, end int
	text       string
}

func splice(s string, ps []patch) string {
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].start < ps[j].start })
	var out strings.Builder
	cursor := 0
	for _, p := range ps {
		out.WriteString(s[cursor:p.start])
		out.WriteString(p.text)
		cursor = p.end
	}
	out.WriteString(s[cursor:])
	return out.String()
}

func attr(s, name, value string) string {
	re := regexp.MustCompile(`(\s` + regexp.QuoteMeta(name) + `\s*=\s*)("[^"]*"|'[^']*')`)
	if re.MatchString(s) {
		return re.ReplaceAllStringFunc(s, func(m string) string { i := strings.Index(m, "="); return m[:i+1] + `"` + opc.Escape(value) + `"` })
	}
	i := strings.LastIndex(s, ">")
	if i > 0 && s[i-1] == '/' {
		i--
	}
	return s[:i] + ` ` + name + `="` + opc.Escape(value) + `"` + s[i:]
}
func insertEnd(s string, n *node, text string) string {
	if n.close == n.open && strings.HasSuffix(strings.TrimSpace(s[n.start:n.open]), "/>") {
		tag := s[n.start:n.open]
		q := strings.Fields(strings.TrimPrefix(tag, "<"))[0]
		q = strings.TrimSuffix(strings.TrimSuffix(q, ">"), "/")
		return s[:n.start] + strings.TrimSuffix(tag, "/>") + ">" + text + "</" + q + ">" + s[n.end:]
	}
	return s[:n.close] + text + s[n.close:]
}
func texts(n *node) string {
	var b strings.Builder
	for _, t := range n.find("t") {
		b.WriteString(t.content())
	}
	return b.String()
}

// replaceIn finds across run boundaries, putting replacement text in the first
// participating run and leaving every unrelated run and property byte-for-byte.
func replaceIn(s string, container *node, find, replacement string, all bool) (string, int) {
	ts := container.find("t")
	var joinedText strings.Builder
	starts := make([]int, len(ts))
	values := make([]string, len(ts))
	for i, t := range ts {
		starts[i] = joinedText.Len()
		values[i] = t.content()
		joinedText.WriteString(values[i])
	}
	joined := joinedText.String()
	if find == "" {
		return s, 0
	}
	count := 0
	positions := []int{}
	for off := 0; off <= len(joined); {
		i := strings.Index(joined[off:], find)
		if i < 0 {
			break
		}
		i += off
		positions = append(positions, i)
		count++
		if count > 100000 {
			return s, -1
		}
		if !all {
			break
		}
		off = i + len(find)
	}
	mi := 0
	for i, t := range ts {
		original := t.content()
		lo, hi := starts[i], starts[i]+len(original)
		for mi < len(positions) && positions[mi]+len(find) <= lo {
			mi++
		}
		var value strings.Builder
		cursor := 0
		for j := mi; j < len(positions) && positions[j] < hi; j++ {
			a, b := positions[j], positions[j]+len(find)
			if b <= lo {
				continue
			}
			x, y := a-lo, b-lo
			if x < 0 {
				x = 0
			}
			if y > len(original) {
				y = len(original)
			}
			value.WriteString(original[cursor:x])
			if a >= lo {
				value.WriteString(replacement)
			}
			cursor = y
		}
		value.WriteString(original[cursor:])
		values[i] = value.String()
	}

	ps := []patch{}
	for i, t := range ts {
		if values[i] == t.content() {
			continue
		}
		tag := s[t.start:t.open]
		if strings.TrimSpace(values[i]) != values[i] {
			tag = attr(tag, "xml:space", "preserve")
		}
		if t.close == t.open && strings.HasSuffix(tag, "/>") {
			q := strings.Fields(strings.TrimPrefix(tag, "<"))[0]
			q = strings.TrimSuffix(strings.TrimSuffix(q, ">"), "/")
			ps = append(ps, patch{t.start, t.end, strings.TrimSuffix(tag, "/>") + ">" + opc.Escape(values[i]) + "</" + q + ">"})
		} else {
			ps = append(ps, patch{t.start, t.end, tag + opc.Escape(values[i]) + s[t.close:t.end]})
		}
	}
	return splice(s, ps), count
}

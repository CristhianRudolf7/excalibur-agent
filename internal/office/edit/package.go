package edit

import (
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/opc"
	"path"
	"regexp"
)

func addElement(p *opc.Package, part, text string) error {
	s := string(p.Parts[part])
	r, e := parse(s)
	if e != nil {
		return e
	}
	if len(r.children) != 1 {
		return fmt.Errorf("invalid root in %s", part)
	}
	p.Parts[part] = []byte(insertEnd(s, r.children[0], text))
	return nil
}
func addRel(p *opc.Package, part, kind, target string) (string, error) {
	if _, ok := p.Parts[part]; !ok {
		p.SetRelationships(part, nil)
	}
	rels, e := p.Relationships(part)
	if e != nil {
		return "", e
	}
	used := map[string]bool{}
	for _, r := range rels {
		used[r.ID] = true
	}
	id := ""
	for i := 1; ; i++ {
		id = fmt.Sprintf("office%d", i)
		if !used[id] {
			break
		}
	}
	e = addElement(p, part, `<Relationship xmlns="`+opc.RelNS+`" Id="`+id+`" Type="`+opc.OfficeRel+kind+`" Target="`+opc.Escape(target)+`"/>`)
	return id, e
}
func addType(p *opc.Package, part, typ string) error {
	return addElement(p, "[Content_Types].xml", `<Override xmlns="`+opc.ContentNS+`" PartName="/`+opc.Escape(part)+`" ContentType="`+typ+`"/>`)
}
func uniquePart(p *opc.Package, dir, prefix string) string {
	for i := 1; ; i++ {
		n := path.Join(dir, fmt.Sprintf("%s%d.xml", prefix, i))
		if _, ok := p.Parts[n]; !ok {
			return n
		}
	}
}
func attributePattern(name string) *regexp.Regexp {
	return regexp.MustCompile(`\s` + regexp.QuoteMeta(name) + `\s*=\s*("[^"]*"|'[^']*')`)
}
func removeAttr(tag, name string) string { return attributePattern(name).ReplaceAllString(tag, "") }

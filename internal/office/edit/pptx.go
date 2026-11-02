package edit

import (
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/create"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/opc"
	"path"
	"strconv"
	"strings"
)

type slideEntry struct {
	part string
	n    *node
}

func slideList(p *opc.Package) ([]slideEntry, error) {
	r, e := parse(string(p.Parts["ppt/presentation.xml"]))
	if e != nil {
		return nil, e
	}
	rels, e := p.Relationships("ppt/_rels/presentation.xml.rels")
	if e != nil {
		return nil, e
	}
	targets := map[string]string{}
	for _, v := range rels {
		targets[v.ID] = opc.Resolve("ppt/_rels/presentation.xml.rels", v.Target)
	}
	out := []slideEntry{}
	for _, n := range r.find("sldId") {
		id := ""
		for _, a := range n.attrs {
			if a.Name.Local == "id" && a.Name.Space == create.R {
				id = a.Value
			}
		}
		if targets[id] == "" {
			return nil, fmt.Errorf("slide relationship missing")
		}
		out = append(out, slideEntry{targets[id], n})
	}
	return out, nil
}
func powerpoint(p *opc.Package, o Operation) error {
	slides, e := slideList(p)
	if e != nil {
		return e
	}
	num := o.Slide
	if o.Anchor != "" {
		_, _ = fmt.Sscanf(o.Anchor, "slide:%d/shape:", &num)
	}
	if num < 1 || num > len(slides) {
		return fmt.Errorf("slide index out of range")
	}
	sl := slides[num-1]
	s := string(p.Parts[sl.part])
	r, e := parse(s)
	if e != nil {
		return e
	}
	switch o.Action {
	case "replace_text":
		count := 0
		paras := r.find("p")
		for i := len(paras) - 1; i >= 0; i-- {
			n := paras[i]
			if o.Anchor != "" {
				id := ""
				for parent := n.parent; parent != nil; parent = parent.parent {
					if parent.name.Local == "sp" {
						ns := parent.find("cNvPr")
						if len(ns) > 0 {
							id = ns[0].attr("id")
						}
						break
					}
				}
				if o.Anchor != fmt.Sprintf("slide:%d/shape:%s", num, id) {
					continue
				}
			}
			if strings.Contains(texts(n), o.Match) && o.Match != "" {
				if !o.All { // select the earliest paragraph in reading order
					earlier := false
					for _, prior := range paras[:i] {
						if strings.Contains(texts(prior), o.Match) {
							if o.Anchor == "" {
								earlier = true
								break
							}
							for par := prior.parent; par != nil; par = par.parent {
								if par.name.Local == "sp" {
									ns := par.find("cNvPr")
									if len(ns) > 0 && o.Anchor == fmt.Sprintf("slide:%d/shape:%s", num, ns[0].attr("id")) {
										earlier = true
									}
									break
								}
							}
						}
					}
					if earlier {
						continue
					}
				}
				var c int
				s, c = replaceIn(s, n, o.Match, o.Text, o.All)
				if c < 0 {
					return fmt.Errorf("replacement exceeds 100000 matches in one paragraph")
				}
				count += c
			}
		}
		if count == 0 {
			return fmt.Errorf("text not found")
		}
		p.Parts[sl.part] = []byte(s)
	case "set_notes":
		rels, _ := p.Relationships(opc.RelsPath(sl.part))
		note := ""
		for _, v := range rels {
			if strings.HasSuffix(v.Type, "/notesSlide") {
				note = opc.Resolve(opc.RelsPath(sl.part), v.Target)
			}
		}
		if note != "" {
			s := string(p.Parts[note])
			nr, e := parse(s)
			if e != nil {
				return e
			}
			var body *node
			for _, sp := range nr.find("sp") {
				for _, ph := range sp.find("ph") {
					if ph.attr("type") == "body" {
						tb := sp.find("txBody")
						if len(tb) > 0 {
							body = tb[0]
						}
					}
				}
			}
			if body == nil {
				return fmt.Errorf("notes body placeholder missing")
			}
			ts := body.find("t")
			ps := []patch{}
			if len(ts) == 0 {
				s = insertEnd(s, body, `<a:p xmlns:a="`+create.A+`"><a:r><a:t>`+opc.Escape(o.Text)+`</a:t></a:r></a:p>`)
			} else {
				for i, n := range ts {
					text := ""
					if i == 0 {
						text = o.Text
					}
					if n.close == n.open && strings.HasSuffix(s[n.start:n.open], "/>") {
						raw := s[n.start:n.open]
						q := strings.TrimSuffix(strings.TrimSuffix(strings.Fields(strings.TrimPrefix(raw, "<"))[0], ">"), "/")
						ps = append(ps, patch{n.start, n.end, strings.TrimSuffix(raw, "/>") + ">" + opc.Escape(text) + "</" + q + ">"})
						continue
					}
					ps = append(ps, patch{n.open, n.close, opc.Escape(text)})
				}
				s = splice(s, ps)
			}
			p.Parts[note] = []byte(s)
		} else {
			note = uniquePart(p, "ppt/notesSlides", "notesSlide")
			p.Parts[note] = []byte(create.NotesXML(o.Text))
			if _, e = addRel(p, opc.RelsPath(sl.part), "notesSlide", "../notesSlides/"+path.Base(note)); e != nil {
				return e
			}
			p.SetRelationships(opc.RelsPath(note), []opc.Relationship{{ID: "slide", Type: opc.OfficeRel + "slide", Target: "../slides/" + path.Base(sl.part)}})
			master, err := ensureNotesMaster(p)
			if err != nil {
				return err
			}
			if _, e = addRel(p, opc.RelsPath(note), "notesMaster", "../notesMasters/"+path.Base(master)); e != nil {
				return e
			}

			if e = addType(p, note, "application/vnd.openxmlformats-officedocument.presentationml.notesSlide+xml"); e != nil {
				return e
			}
		}
	case "duplicate_slide":
		np := uniquePart(p, "ppt/slides", "slide")
		p.Parts[np] = append([]byte(nil), p.Parts[sl.part]...)
		if relData, ok := p.Parts[opc.RelsPath(sl.part)]; ok {
			p.Parts[opc.RelsPath(np)] = append([]byte(nil), relData...)
			rs, _ := p.Relationships(opc.RelsPath(np))
			for _, v := range rs {
				if strings.HasSuffix(v.Type, "/notesSlide") {
					old := opc.Resolve(opc.RelsPath(np), v.Target)
					nn := uniquePart(p, "ppt/notesSlides", "notesSlide")
					p.Parts[nn] = append([]byte(nil), p.Parts[old]...)
					if raw, ok := p.Parts[opc.RelsPath(old)]; ok {
						p.Parts[opc.RelsPath(nn)] = append([]byte(nil), raw...)
						if e = retarget(p, opc.RelsPath(nn), "slide", "../slides/"+path.Base(np)); e != nil {
							return e
						}
					}
					if e = retarget(p, opc.RelsPath(np), "notesSlide", "../notesSlides/"+path.Base(nn)); e != nil {
						return e
					}
					if e = addType(p, nn, "application/vnd.openxmlformats-officedocument.presentationml.notesSlide+xml"); e != nil {
						return e
					}
				}
			}
		}
		id, e := addRel(p, "ppt/_rels/presentation.xml.rels", "slide", "slides/"+path.Base(np))
		if e != nil {
			return e
		}
		if e = addType(p, np, "application/vnd.openxmlformats-officedocument.presentationml.slide+xml"); e != nil {
			return e
		}
		pres := string(p.Parts["ppt/presentation.xml"])
		max := 255
		for _, s := range slides {
			for _, a := range s.n.attrs {
				if a.Name.Local == "id" && a.Name.Space == "" {
					n, _ := strconv.Atoi(a.Value)
					if n > max {
						max = n
					}
				}
			}
		}
		f := fmt.Sprintf(`<p:sldId xmlns:p="%s" xmlns:r="%s" id="%d" r:id="%s"/>`, create.P, create.R, max+1, id)
		p.Parts["ppt/presentation.xml"] = []byte(pres[:sl.n.end] + f + pres[sl.n.end:])
	case "delete_slide":
		if len(slides) == 1 {
			return fmt.Errorf("cannot delete the last slide")
		}
		pres := string(p.Parts["ppt/presentation.xml"])
		p.Parts["ppt/presentation.xml"] = []byte(pres[:sl.n.start] + pres[sl.n.end:])
	case "move_slide":
		if o.To < 1 || o.To > len(slides) {
			return fmt.Errorf("destination out of range")
		}
		if num == o.To {
			return nil
		}
		pres := string(p.Parts["ppt/presentation.xml"])
		fragment := pres[sl.n.start:sl.n.end]
		idx := slides[o.To-1].n.start
		if o.To > num {
			idx = slides[o.To-1].n.end
		}
		p.Parts["ppt/presentation.xml"] = []byte(splice(pres, []patch{{sl.n.start, sl.n.end, ""}, {idx, idx, fragment}}))
	default:
		return fmt.Errorf("unknown PPTX action %q", o.Action)
	}
	return nil
}
func retarget(p *opc.Package, part, kind, target string) error {
	s := string(p.Parts[part])
	r, e := parse(s)
	if e != nil {
		return e
	}
	ps := []patch{}
	for _, n := range r.find("Relationship") {
		if strings.HasSuffix(n.attr("Type"), "/"+kind) {
			ps = append(ps, patch{n.start, n.open, attr(s[n.start:n.open], "Target", target)})
		}
	}
	p.Parts[part] = []byte(splice(s, ps))
	return nil
}

// ensureNotesMaster adds only new parts and token-splices presentation metadata
// when a previously note-free input has no notes master to reuse.
func ensureNotesMaster(p *opc.Package) (string, error) {
	const relPart = "ppt/_rels/presentation.xml.rels"
	rels, e := p.Relationships(relPart)
	if e != nil {
		return "", e
	}
	for _, r := range rels {
		if strings.HasSuffix(r.Type, "/notesMaster") {
			n := opc.Resolve(relPart, r.Target)
			if _, ok := p.Parts[n]; ok {
				return n, nil
			}
		}
	}
	template, e := create.Build("pptx", create.Spec{Slides: []create.Slide{{Kind: "title"}}}, nil)
	if e != nil {
		return "", e
	}
	master := uniquePart(p, "ppt/notesMasters", "notesMaster")
	theme := uniquePart(p, "ppt/theme", "theme")
	p.Parts[master] = template.Parts["ppt/notesMasters/notesMaster1.xml"]
	p.Parts[theme] = template.Parts["ppt/theme/theme1.xml"]
	p.SetRelationships(opc.RelsPath(master), []opc.Relationship{{ID: "theme", Type: opc.OfficeRel + "theme", Target: "../theme/" + path.Base(theme)}})
	if e = addType(p, master, "application/vnd.openxmlformats-officedocument.presentationml.notesMaster+xml"); e != nil {
		return "", e
	}
	if e = addType(p, theme, "application/vnd.openxmlformats-officedocument.theme+xml"); e != nil {
		return "", e
	}
	id, e := addRel(p, relPart, "notesMaster", "notesMasters/"+path.Base(master))
	if e != nil {
		return "", e
	}
	const part = "ppt/presentation.xml"
	s := string(p.Parts[part])
	r, e := parse(s)
	if e != nil {
		return "", e
	}
	fragment := `<p:notesMasterId xmlns:p="` + create.P + `" xmlns:r="` + create.R + `" r:id="` + id + `"/>`
	lists := r.find("notesMasterIdLst")
	if len(lists) > 0 {
		s = insertEnd(s, lists[0], fragment)
	} else {
		fragment = `<p:notesMasterIdLst xmlns:p="` + create.P + `">` + fragment + `</p:notesMasterIdLst>`
		if len(r.children) != 1 {
			return "", fmt.Errorf("invalid presentation")
		}
		idx := r.children[0].open
		masters := r.find("sldMasterIdLst")
		if len(masters) > 0 {
			idx = masters[0].end
		}
		s = s[:idx] + fragment + s[idx:]
	}
	p.Parts[part] = []byte(s)
	return master, nil
}

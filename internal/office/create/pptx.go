package create

import (
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/opc"
)

const groupTree = `<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>`
const namespaces = ` xmlns:p="` + P + `" xmlns:a="` + A + `" xmlns:r="` + R + `"`

// NotesXML generates the editable speaker-notes part for a slide.
func NotesXML(text string) string {
	return `<p:notes` + namespaces + `><p:cSld><p:spTree>` + groupTree + `<p:sp><p:nvSpPr><p:cNvPr id="2" name="Notes"/><p:cNvSpPr/><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:t>` + esc(text) + `</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:notes>`
}
func pptx(s Spec, load ImageLoader) (*opc.Package, error) {
	if len(s.Slides) == 0 && s.Title == "" {
		return nil, fmt.Errorf("at least one slide required")
	}
	palette, err := presentationPalette(s.Theme)
	if err != nil {
		return nil, err
	}
	slides := s.Slides
	if s.Title != "" {
		slides = append([]Slide{{Kind: "title", Title: s.Title}}, slides...)
	}
	p := opc.New()
	root(p, "ppt/presentation.xml")
	types := map[string]string{}
	add := func(n, kind, xml string) {
		p.Parts[n] = []byte(xml)
		types[n] = "application/vnd.openxmlformats-officedocument.presentationml." + kind + "+xml"
	}
	pres := `<p:presentation` + namespaces + `><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="master"/></p:sldMasterIdLst><p:notesMasterIdLst><p:notesMasterId r:id="notesMaster"/></p:notesMasterIdLst><p:sldIdLst>`
	rels := []opc.Relationship{relation("master", "slideMaster", "slideMasters/slideMaster1.xml"), relation("notesMaster", "notesMaster", "notesMasters/notesMaster1.xml")}
	for i, slide := range slides {
		n := i + 1
		for _, list := range [][]Bullet{slide.Bullets, slide.Left, slide.Right} {
			for _, b := range list {
				if b.Level < 0 || b.Level > 8 {
					return nil, fmt.Errorf("bullet level must be 0..8")
				}
			}
		}
		id := fmt.Sprintf("slide%d", n)
		pres += fmt.Sprintf(`<p:sldId id="%d" r:id="%s"/>`, 255+n, id)
		rels = append(rels, relation(id, "slide", fmt.Sprintf("slides/slide%d.xml", n)))
		sr := []opc.Relationship{relation("layout", "slideLayout", "../slideLayouts/slideLayout1.xml")}
		shapes, background, data, ext, err := renderSlide(slide, n, palette, load)
		if err != nil {
			return nil, fmt.Errorf("slide %d: %w", n, err)
		}
		if data != nil {
			media := fmt.Sprintf("image%d.%s", n, ext)
			p.Parts["ppt/media/"+media] = data
			sr = append(sr, relation("image", "image", "../media/"+media))
		}
		sn := fmt.Sprintf("ppt/slides/slide%d.xml", n)
		add(sn, "slide", `<p:sld`+namespaces+`><p:cSld><p:bg><p:bgPr>`+solid(background)+`<a:effectLst/></p:bgPr></p:bg><p:spTree>`+groupTree+shapes+`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`)

		if slide.Notes != "" {
			nn := fmt.Sprintf("ppt/notesSlides/notesSlide%d.xml", n)
			add(nn, "notesSlide", NotesXML(slide.Notes))
			sr = append(sr, relation("notes", "notesSlide", fmt.Sprintf("../notesSlides/notesSlide%d.xml", n)))
			p.SetRelationships(opc.RelsPath(nn), []opc.Relationship{relation("slide", "slide", fmt.Sprintf("../slides/slide%d.xml", n)), relation("master", "notesMaster", "../notesMasters/notesMaster1.xml")})
		}
		p.SetRelationships(opc.RelsPath(sn), sr)
	}
	add("ppt/presentation.xml", "presentation.main", pres+`</p:sldIdLst><p:sldSz cx="12192000" cy="6858000" type="screen16x9"/><p:notesSz cx="6858000" cy="9144000"/></p:presentation>`)
	p.SetRelationships("ppt/_rels/presentation.xml.rels", rels)
	cmap := `<p:clrMap accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" bg1="lt1" bg2="lt2" folHlink="folHlink" hlink="hlink" tx1="dk1" tx2="dk2"/>`
	add("ppt/slideMasters/slideMaster1.xml", "slideMaster", `<p:sldMaster`+namespaces+`><p:cSld name="Default"><p:spTree>`+groupTree+`</p:spTree></p:cSld>`+cmap+`<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="layout"/></p:sldLayoutIdLst><p:txStyles><p:titleStyle/><p:bodyStyle/><p:otherStyle/></p:txStyles></p:sldMaster>`)
	p.SetRelationships("ppt/slideMasters/_rels/slideMaster1.xml.rels", []opc.Relationship{relation("layout", "slideLayout", "../slideLayouts/slideLayout1.xml"), relation("theme", "theme", "../theme/theme1.xml")})
	add("ppt/slideLayouts/slideLayout1.xml", "slideLayout", `<p:sldLayout`+namespaces+` type="blank" preserve="1"><p:cSld name="Blank"><p:spTree>`+groupTree+`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`)
	p.SetRelationships("ppt/slideLayouts/_rels/slideLayout1.xml.rels", []opc.Relationship{relation("master", "slideMaster", "../slideMasters/slideMaster1.xml")})
	add("ppt/notesMasters/notesMaster1.xml", "notesMaster", `<p:notesMaster`+namespaces+`><p:cSld><p:spTree>`+groupTree+`</p:spTree></p:cSld>`+cmap+`<p:notesStyle/></p:notesMaster>`)
	p.SetRelationships("ppt/notesMasters/_rels/notesMaster1.xml.rels", []opc.Relationship{relation("theme", "theme", "../theme/theme1.xml")})
	theme := `<a:theme xmlns:a="` + A + `" name="Excalibur"><a:themeElements><a:clrScheme name="Excalibur">`
	for i, key := range []string{"dk1", "lt1", "dk2", "lt2", "accent1", "accent2", "accent3", "accent4", "accent5", "accent6", "hlink", "folHlink"} {
		colors := []string{"0F172A", "FFFFFF", "475569", "F8FAFC", palette.accent, "14B8A6", "818CF8", "38BDF8", "F59E0B", "94A3B8", palette.accent, "818CF8"}
		theme += `<a:` + key + `><a:srgbClr val="` + colors[i] + `"/></a:` + key + `>`
	}
	theme += `</a:clrScheme><a:fontScheme name="Excalibur"><a:majorFont><a:latin typeface="Calibri"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont><a:minorFont><a:latin typeface="Calibri"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont></a:fontScheme><a:fmtScheme name="Default"><a:fillStyleLst>`
	for i := 0; i < 3; i++ {
		theme += `<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>`
	}
	theme += `</a:fillStyleLst><a:lnStyleLst>`
	for i := 0; i < 3; i++ {
		theme += `<a:ln w="12700"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:prstDash val="solid"/></a:ln>`
	}
	theme += `</a:lnStyleLst><a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst><a:bgFillStyleLst>`
	for i := 0; i < 3; i++ {
		theme += `<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>`
	}
	theme += `</a:bgFillStyleLst></a:fmtScheme></a:themeElements></a:theme>`
	p.Parts["ppt/theme/theme1.xml"] = []byte(theme)
	types["ppt/theme/theme1.xml"] = "application/vnd.openxmlformats-officedocument.theme+xml"
	p.Types(types)
	return p, nil
}

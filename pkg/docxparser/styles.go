package docxparser

import (
	"bytes"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
)

type StyleInfo struct {
	ID           string
	Name         string
	BasedOn      string
	HeadingLevel int
	IsTOC        bool
	IsTitle      bool
	IsCode       bool
	HasShading   bool
}

type StyleSheet struct {
	styles map[string]*StyleInfo
}

func ParseStyles(data []byte) *StyleSheet {
	ss := &StyleSheet{styles: make(map[string]*StyleInfo)}
	if len(data) == 0 {
		return ss
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	var cur *StyleInfo
	inPPr := false
	inRPr := false

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ss
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "style":
				id := attrVal(el.Attr, "styleId")
				if id != "" {
					cur = &StyleInfo{ID: id}
				}
			case "name":
				if cur != nil {
					cur.Name = attrVal(el.Attr, "val")
				}
			case "basedOn":
				if cur != nil {
					cur.BasedOn = attrVal(el.Attr, "val")
				}
			case "pPr":
				if cur != nil {
					inPPr = true
				}
			case "rPr":
				if cur != nil {
					inRPr = true
				}
			case "outlineLvl":
				if cur != nil && inPPr {
					if lvl, convErr := strconv.Atoi(attrVal(el.Attr, "val")); convErr == nil && lvl >= 0 && lvl <= 5 {
						cur.HeadingLevel = lvl + 1
					}
				}
			case "rFonts":
				if cur != nil && inRPr {
					for _, a := range el.Attr {
						if isMonospaceFont(a.Value) {
							cur.IsCode = true
							break
						}
					}
				}
			case "shd":
				if cur != nil && inPPr {
					if isNonWhiteShading(attrVal(el.Attr, "fill")) {
						cur.HasShading = true
					}
				}
			}
		case xml.EndElement:
			switch el.Name.Local {
			case "style":
				if cur != nil {
					classifyStyle(cur)
					ss.styles[cur.ID] = cur
					cur = nil
				}
				inPPr = false
				inRPr = false
			case "pPr":
				inPPr = false
			case "rPr":
				inRPr = false
			}
		}
	}
	return ss
}

func classifyStyle(s *StyleInfo) {
	lowerID := strings.ToLower(strings.TrimSpace(s.ID))
	lowerName := strings.ToLower(strings.TrimSpace(s.Name))

	if strings.HasPrefix(lowerID, "toc") || strings.HasPrefix(lowerName, "toc") || strings.Contains(s.Name, "目录") {
		s.IsTOC = true
	}
	if lowerID == "title" || lowerName == "title" || s.Name == "标题" {
		s.IsTitle = true
	}
	for _, kw := range []string{"code", "source", "listing", "verbatim", "preformatted", "program"} {
		if strings.Contains(lowerID, kw) || strings.Contains(lowerName, kw) {
			s.IsCode = true
			break
		}
	}
	if !s.IsCode {
		for _, kw := range []string{"代码", "源码", "程序", "命令行", "终端"} {
			if strings.Contains(s.Name, kw) {
				s.IsCode = true
				break
			}
		}
	}
	if s.HeadingLevel == 0 {
		if lvl := extractHeadingLevelFromName(lowerID); lvl > 0 {
			s.HeadingLevel = lvl
		} else if lvl := extractHeadingLevelFromName(lowerName); lvl > 0 {
			s.HeadingLevel = lvl
		} else if lvl := extractChineseHeadingLevel(s.Name); lvl > 0 {
			s.HeadingLevel = lvl
		}
	}
}

func extractHeadingLevelFromName(s string) int {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "heading") {
		return 0
	}
	rest := strings.TrimSpace(strings.TrimPrefix(s, "heading"))
	if n, err := strconv.Atoi(rest); err == nil && n >= 1 && n <= 6 {
		return n
	}
	return 0
}

func extractChineseHeadingLevel(s string) int {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "标题") {
		return 0
	}
	rest := strings.TrimSpace(strings.TrimPrefix(s, "标题"))
	if n, err := strconv.Atoi(rest); err == nil && n >= 1 && n <= 6 {
		return n
	}
	return 0
}

func isMonospaceFont(name string) bool {
	l := strings.ToLower(strings.TrimSpace(name))
	if l == "" {
		return false
	}
	fonts := []string{
		"consolas", "courier", "mono", "menlo", "monaco",
		"fira code", "source code", "cascadia", "jetbrains",
		"inconsolata", "hack", "lucida console", "sf mono",
		"sarasa", "iosevka", "victor", "operator",
		"anonymous pro", "ubuntu mono", "dejavu sans mono",
		"liberation mono", "droid sans mono", "noto sans mono",
		"roboto mono", "andale", "fixedsys", "terminal",
	}
	for _, f := range fonts {
		if strings.Contains(l, f) {
			return true
		}
	}
	return false
}

func isNonWhiteShading(fill string) bool {
	f := strings.ToLower(strings.TrimSpace(fill))
	return f != "" && f != "auto" && f != "ffffff" && f != "fff" && f != "none" && f != "clear"
}

func (ss *StyleSheet) ResolveHeadingLevel(styleID string) int {
	if ss == nil || styleID == "" {
		return 0
	}
	cur := styleID
	for depth := 0; depth < 16 && cur != ""; depth++ {
		st, ok := ss.styles[cur]
		if !ok {
			if lvl := extractHeadingLevelFromName(strings.ToLower(cur)); lvl > 0 {
				return lvl
			}
			return 0
		}
		if st.IsTOC {
			return 0
		}
		if st.HeadingLevel > 0 {
			return st.HeadingLevel
		}
		cur = st.BasedOn
	}
	return 0
}

func (ss *StyleSheet) IsTOCStyle(styleID string) bool {
	if styleID == "" {
		return false
	}
	lower := strings.ToLower(styleID)
	if strings.HasPrefix(lower, "toc") {
		return true
	}
	if ss == nil {
		return false
	}
	cur := styleID
	for depth := 0; depth < 16 && cur != ""; depth++ {
		st, ok := ss.styles[cur]
		if !ok {
			return false
		}
		if st.IsTOC {
			return true
		}
		cur = st.BasedOn
	}
	return false
}

func (ss *StyleSheet) IsTitleStyle(styleID string) bool {
	if styleID == "" {
		return false
	}
	if strings.EqualFold(styleID, "Title") {
		return true
	}
	if ss == nil {
		return false
	}
	st, ok := ss.styles[styleID]
	return ok && st.IsTitle
}

func (ss *StyleSheet) IsCodeStyle(styleID string) bool {
	if styleID == "" || ss == nil {
		return false
	}
	cur := styleID
	for depth := 0; depth < 16 && cur != ""; depth++ {
		st, ok := ss.styles[cur]
		if !ok {
			return false
		}
		if st.IsCode {
			return true
		}
		cur = st.BasedOn
	}
	return false
}

func (ss *StyleSheet) HasShading(styleID string) bool {
	if styleID == "" || ss == nil {
		return false
	}
	cur := styleID
	for depth := 0; depth < 16 && cur != ""; depth++ {
		st, ok := ss.styles[cur]
		if !ok {
			return false
		}
		if st.HasShading {
			return true
		}
		cur = st.BasedOn
	}
	return false
}

// NumberingMap maps numId -> ilvl -> ordered(bool).
type NumberingMap struct {
	abstract map[string]map[string]bool
	numToAbs map[string]string
	override map[string]map[string]bool
}

func ParseNumbering(data []byte) *NumberingMap {
	nm := &NumberingMap{
		abstract: make(map[string]map[string]bool),
		numToAbs: make(map[string]string),
		override: make(map[string]map[string]bool),
	}
	if len(data) == 0 {
		return nm
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	var curAbsID, curNumID, curIlvl string
	inOverride := false

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nm
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "abstractNum":
				curAbsID = attrVal(el.Attr, "abstractNumId")
				if curAbsID != "" && nm.abstract[curAbsID] == nil {
					nm.abstract[curAbsID] = make(map[string]bool)
				}
			case "num":
				curNumID = attrVal(el.Attr, "numId")
			case "abstractNumId":
				if curNumID != "" {
					nm.numToAbs[curNumID] = attrVal(el.Attr, "val")
				}
			case "lvlOverride":
				if curNumID != "" {
					inOverride = true
					curIlvl = attrVal(el.Attr, "ilvl")
				}
			case "lvl":
				curIlvl = attrVal(el.Attr, "ilvl")
			case "numFmt":
				fmtVal := strings.ToLower(attrVal(el.Attr, "val"))
				ordered := fmtVal != "" && fmtVal != "bullet" && fmtVal != "none"
				if inOverride && curNumID != "" && curIlvl != "" {
					if nm.override[curNumID] == nil {
						nm.override[curNumID] = make(map[string]bool)
					}
					nm.override[curNumID][curIlvl] = ordered
				} else if curAbsID != "" && curIlvl != "" {
					nm.abstract[curAbsID][curIlvl] = ordered
				}
			}
		case xml.EndElement:
			switch el.Name.Local {
			case "abstractNum":
				curAbsID = ""
			case "num":
				curNumID = ""
				inOverride = false
			case "lvlOverride":
				inOverride = false
				curIlvl = ""
			case "lvl":
				if !inOverride {
					curIlvl = ""
				}
			}
		}
	}
	return nm
}

func (nm *NumberingMap) IsOrdered(numID string, ilvl int) bool {
	if nm == nil || numID == "" {
		return false
	}
	lvlKey := strconv.Itoa(ilvl)
	if ov, ok := nm.override[numID]; ok {
		if v, exists := ov[lvlKey]; exists {
			return v
		}
	}
	absID := nm.numToAbs[numID]
	if absID == "" {
		return false
	}
	if lvls, ok := nm.abstract[absID]; ok {
		return lvls[lvlKey]
	}
	return false
}

func attrVal(attrs []xml.Attr, local string) string {
	for _, a := range attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

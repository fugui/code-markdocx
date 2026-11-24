package docxparser

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

type relTarget struct {
	Target   string
	External bool
}

var (
	captionPattern       = regexp.MustCompile(`^(图|表|Figure|Fig\.?|Diagram)\s*\d*[\s:：.\-]*`)
	numberedHeadingRegex = regexp.MustCompile(`^(\d+(?:\.\d+){0,4})\s+(.{2,60})$`)
)

func ParseFile(filePath string, opts ParseOptions) (*DocumentIR, error) {
	sz, err := OpenSafeZipFile(filePath)
	if err != nil {
		return nil, err
	}
	return parseSafeZip(sz, opts)
}

func ParseBytes(data []byte, opts ParseOptions) (*DocumentIR, error) {
	sz, err := OpenSafeZip(data)
	if err != nil {
		return nil, err
	}
	return parseSafeZip(sz, opts)
}

func parseSafeZip(sz *SafeZip, opts ParseOptions) (*DocumentIR, error) {
	var styles *StyleSheet
	if sBytes, err := sz.ReadEntry("word/styles.xml"); err == nil {
		styles = ParseStyles(sBytes)
	} else {
		styles = ParseStyles(nil)
	}

	var numbering *NumberingMap
	if nBytes, err := sz.ReadEntry("word/numbering.xml"); err == nil {
		numbering = ParseNumbering(nBytes)
	} else {
		numbering = ParseNumbering(nil)
	}

	rels := make(map[string]relTarget)
	if rBytes, err := sz.ReadEntry("word/_rels/document.xml.rels"); err == nil {
		rels = parseRelationships(rBytes)
	}

	docBytes, err := sz.ReadEntry("word/document.xml")
	if err != nil {
		return nil, err
	}

	p := &docWalker{
		sz:        sz,
		styles:    styles,
		numbering: numbering,
		rels:      rels,
		opts:      opts,
		ir:        &DocumentIR{},
	}
	if err := p.walkDocument(docBytes); err != nil {
		return nil, err
	}
	p.finalizeBlocks()
	p.populateImageContexts()
	return p.ir, nil
}

func parseRelationships(data []byte) map[string]relTarget {
	rels := make(map[string]relTarget)
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err == io.EOF || err != nil {
			break
		}
		if el, ok := tok.(xml.StartElement); ok && el.Name.Local == "Relationship" {
			id := attrVal(el.Attr, "Id")
			target := attrVal(el.Attr, "Target")
			mode := attrVal(el.Attr, "TargetMode")
			if id != "" && target != "" {
				external := strings.EqualFold(mode, "External")
				if !external {
					target = normalizeWordTarget(target)
				}
				rels[id] = relTarget{Target: target, External: external}
			}
		}
	}
	return rels
}

func normalizeWordTarget(target string) string {
	target = strings.ReplaceAll(target, "\\", "/")
	if strings.HasPrefix(target, "/word/") {
		return strings.TrimPrefix(target, "/")
	}
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(target, "/")
	}
	if strings.HasPrefix(target, "word/") {
		return path.Clean(target)
	}
	return path.Clean(path.Join("word", target))
}

type xmlNode struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Content string     `xml:",chardata"`
	Nodes   []xmlNode  `xml:",any"`
}

type docWalker struct {
	sz           *SafeZip
	styles       *StyleSheet
	numbering    *NumberingMap
	rels         map[string]relTarget
	opts         ParseOptions
	ir           *DocumentIR
	rawItems     []rawBlockItem
	headingStack [6]string
	seenMainHead bool
	blockCounter int
	imageCounter int
	inTOCField   bool
	inInstrText  bool
	pendingInstr strings.Builder
}

func (w *docWalker) walkDocument(data []byte) error {
	var root xmlNode
	if err := xml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parse word/document.xml: %w", err)
	}
	for _, child := range root.Nodes {
		if child.XMLName.Local == "body" {
			w.walkBodyChildren(child.Nodes)
			break
		}
	}
	return nil
}

func (w *docWalker) walkBodyChildren(nodes []xmlNode) {
	for _, n := range nodes {
		switch n.XMLName.Local {
		case "p":
			w.handleParagraph(n)
		case "tbl":
			w.handleTable(n)
		case "sdt":
			for _, sub := range n.Nodes {
				if sub.XMLName.Local == "sdtContent" {
					w.walkBodyChildren(sub.Nodes)
				}
			}
		case "AlternateContent":
			for _, chosen := range pickAlternateContent(n) {
				w.walkBodyChildren(chosen.Nodes)
			}
		}
	}
}

func pickAlternateContent(n xmlNode) []xmlNode {
	var choiceNodes, fallbackNodes []xmlNode
	for _, c := range n.Nodes {
		switch c.XMLName.Local {
		case "Choice":
			choiceNodes = append(choiceNodes, c)
		case "Fallback":
			fallbackNodes = append(fallbackNodes, c)
		}
	}
	if len(choiceNodes) > 0 {
		return choiceNodes
	}
	return fallbackNodes
}

func (w *docWalker) appendItem(item rawBlockItem) {
	w.rawItems = append(w.rawItems, item)
}

func (w *docWalker) finalizeBlocks() {
	w.ir.Blocks = postProcessCodeBlocks(w.rawItems)
}

func (w *docWalker) lastNonEmptyText() string {
	for i := len(w.rawItems) - 1; i >= 0; i-- {
		txt := strings.TrimSpace(blockSummaryText(w.rawItems[i].block))
		if txt != "" {
			return txt
		}
	}
	return ""
}

func (w *docWalker) handleParagraph(p xmlNode) {
	w.blockCounter++
	var styleID string
	var outlineLvl int
	var numID string
	var ilvl int
	hasList := false

	for _, child := range p.Nodes {
		if child.XMLName.Local == "pPr" {
			for _, prop := range child.Nodes {
				switch prop.XMLName.Local {
				case "pStyle":
					styleID = attrVal(prop.Attrs, "val")
				case "outlineLvl":
					if v, err := strconv.Atoi(attrVal(prop.Attrs, "val")); err == nil && v >= 0 && v <= 5 {
						outlineLvl = v + 1
					}
				case "numPr":
					for _, np := range prop.Nodes {
						switch np.XMLName.Local {
						case "numId":
							numID = attrVal(np.Attrs, "val")
							if numID != "" && numID != "0" {
								hasList = true
							}
						case "ilvl":
							if v, err := strconv.Atoi(attrVal(np.Attrs, "val")); err == nil {
								ilvl = v
							}
						}
					}
				}
			}
		}
	}

	isTOC := w.styles.IsTOCStyle(styleID)
	meta, imgIDs, txbxNodes, tocFieldDetected := w.extractParagraphMeta(p, styleID)
	if tocFieldDetected {
		isTOC = true
	}

	trimmed := strings.TrimSpace(meta.inlineMD)

	if w.opts.StripTOC && (isTOC || w.inTOCField) {
		if trimmed != "" {
			w.ir.NoiseStats.StrippedTOCBlocks++
		}
		return
	}

	if w.styles.IsTitleStyle(styleID) && w.ir.Title == "" && trimmed != "" {
		w.ir.Title = trimmed
	}

	headingLvl := w.styles.ResolveHeadingLevel(styleID)
	if headingLvl == 0 && outlineLvl > 0 {
		headingLvl = outlineLvl
	}
	hasStrongCodeHint := meta.isCodeStyle || meta.monoRatio >= 0.80 || meta.hasShading || meta.distinctCols >= 2
	if headingLvl == 0 && !hasList && !hasStrongCodeHint && trimmed != "" && len(imgIDs) == 0 {
		// Detect common numbered headings when paragraph lacks formal Heading style
		plainTrimmed := strings.TrimSpace(meta.plainText)
		if m := numberedHeadingRegex.FindStringSubmatch(plainTrimmed); len(m) == 3 {
			dots := strings.Count(m[1], ".") + 1
			if dots >= 1 && dots <= 4 && !strings.HasSuffix(plainTrimmed, "。") && !strings.HasSuffix(plainTrimmed, ".") {
				headingLvl = dots
			}
		}
	}

	if headingLvl > 0 && trimmed != "" {
		w.seenMainHead = true
		w.updateHeadingStack(headingLvl, trimmed)
		if w.ir.Title == "" && headingLvl == 1 {
			w.ir.Title = trimmed
		}
		w.appendItem(rawBlockItem{
			block: Block{
				Kind:  BlockHeading,
				Level: headingLvl,
				Text:  trimmed,
			},
		})
		for _, imgID := range imgIDs {
			w.appendItem(rawBlockItem{block: Block{Kind: BlockImage, ImageID: imgID}})
		}
		for _, tb := range txbxNodes {
			w.walkBodyChildren(tb.Nodes)
		}
		return
	}

	if w.opts.StripCover && !w.seenMainHead {
		if trimmed != "" || len(imgIDs) > 0 {
			w.ir.NoiseStats.StrippedCoverBlocks++
		}
		return
	}

	if meta.isEmpty && len(imgIDs) == 0 {
		if len(w.rawItems) > 0 {
			w.rawItems[len(w.rawItems)-1].blankLinesAfter++
		}
		for _, tb := range txbxNodes {
			w.walkBodyChildren(tb.Nodes)
		}
		return
	}

	if trimmed != "" {
		if hasList {
			w.appendItem(rawBlockItem{
				block: Block{
					Kind:    BlockListItem,
					Text:    trimmed,
					Ordered: w.numbering.IsOrdered(numID, ilvl),
					Indent:  ilvl,
				},
			})
		} else {
			w.appendItem(rawBlockItem{
				block: Block{
					Kind: BlockParagraph,
					Text: trimmed,
				},
				meta: &meta,
			})
		}
	}
	for _, imgID := range imgIDs {
		w.appendItem(rawBlockItem{block: Block{Kind: BlockImage, ImageID: imgID}})
	}
	for _, tb := range txbxNodes {
		w.walkBodyChildren(tb.Nodes)
	}
}

func (w *docWalker) updateHeadingStack(level int, text string) {
	if level < 1 || level > 6 {
		return
	}
	w.headingStack[level-1] = text
	for i := level; i < 6; i++ {
		w.headingStack[i] = ""
	}
}

func (w *docWalker) currentSectionPath() string {
	var parts []string
	for _, h := range w.headingStack {
		if h != "" {
			parts = append(parts, h)
		}
	}
	return strings.Join(parts, " > ")
}

func (w *docWalker) extractParagraphMeta(p xmlNode, styleID string) (paraMeta, []string, []xmlNode, bool) {
	meta := paraMeta{
		styleID:     styleID,
		isCodeStyle: w.styles.IsCodeStyle(styleID),
		hasShading:  w.styles.HasShading(styleID),
	}

	for _, child := range p.Nodes {
		if child.XMLName.Local != "pPr" {
			continue
		}
		for _, prop := range child.Nodes {
			switch prop.XMLName.Local {
			case "pStyle":
				if meta.styleID == "" {
					meta.styleID = attrVal(prop.Attrs, "val")
					meta.isCodeStyle = w.styles.IsCodeStyle(meta.styleID)
					meta.hasShading = meta.hasShading || w.styles.HasShading(meta.styleID)
				}
			case "shd":
				fill := attrVal(prop.Attrs, "fill")
				if isNonWhiteShading(fill) {
					meta.hasShading = true
					meta.shadingFill = fill
				}
			case "pBdr":
				for _, side := range prop.Nodes {
					val := strings.ToLower(strings.TrimSpace(attrVal(side.Attrs, "val")))
					if val != "" && val != "none" && val != "nil" {
						meta.hasBorder = true
						break
					}
				}
			case "ind":
				if leftStr := attrVal(prop.Attrs, "left"); leftStr != "" {
					if v, err := strconv.Atoi(leftStr); err == nil && v > 0 {
						meta.indentLeft = v
					}
				} else if leftStr := attrVal(prop.Attrs, "start"); leftStr != "" {
					if v, err := strconv.Atoi(leftStr); err == nil && v > 0 {
						meta.indentLeft = v
					}
				}
			}
		}
	}

	spans, imgIDs, txbxNodes, sawTOC := w.extractParagraphSpans(p)
	meta.spans = spans
	meta.plainText = buildPlainCodeText(spans)
	meta.inlineMD = renderInlineMarkdown(spans)
	meta.hasSoftBreak = strings.Contains(meta.plainText, "\n")
	meta.isEmpty = strings.TrimSpace(meta.plainText) == ""

	var asciiTotal, asciiMono, allTotal, allMono int
	colors := make(map[string]struct{})
	for _, sp := range spans {
		hasVisible := false
		for _, r := range sp.text {
			if unicode.IsSpace(r) || r == '\u00a0' {
				continue
			}
			hasVisible = true
			allTotal++
			if sp.isMono || meta.isCodeStyle {
				allMono++
			}
			if r < 128 {
				asciiTotal++
				if sp.isMono || meta.isCodeStyle {
					asciiMono++
				}
			}
		}
		if hasVisible && sp.color != "" {
			colors[sp.color] = struct{}{}
		}
	}
	if asciiTotal > 0 {
		meta.monoRatio = float64(asciiMono) / float64(asciiTotal)
	} else if allTotal > 0 {
		meta.monoRatio = float64(allMono) / float64(allTotal)
	}
	meta.distinctCols = len(colors)

	return meta, imgIDs, txbxNodes, sawTOC
}

func (w *docWalker) extractParagraphSpans(p xmlNode) ([]textSpan, []string, []xmlNode, bool) {
	var spans []textSpan
	var imgIDs []string
	var txbxNodes []xmlNode
	sawTOC := false

	var walk func(nodes []xmlNode)
	walk = func(nodes []xmlNode) {
		for _, n := range nodes {
			switch n.XMLName.Local {
			case "pPr":
				continue
			case "r":
				rSpans, runImgs, runTxbx, runTOC := w.extractRun(n)
				if runTOC {
					sawTOC = true
				}
				spans = append(spans, rSpans...)
				imgIDs = append(imgIDs, runImgs...)
				txbxNodes = append(txbxNodes, runTxbx...)
			case "hyperlink":
				linkID := attrVal(n.Attrs, "id")
				var linkSpans []textSpan
				for _, child := range n.Nodes {
					if child.XMLName.Local == "r" {
						rs, rImgs, rTxbx, _ := w.extractRun(child)
						linkSpans = append(linkSpans, rs...)
						imgIDs = append(imgIDs, rImgs...)
						txbxNodes = append(txbxNodes, rTxbx...)
					}
				}
				linkText := strings.TrimSpace(buildPlainCodeText(linkSpans))
				if linkText != "" {
					if rel, ok := w.rels[linkID]; ok && rel.External && rel.Target != "" {
						spans = append(spans, textSpan{
							text:  fmt.Sprintf("[%s](%s)", linkText, rel.Target),
							rawMD: true,
						})
					} else {
						spans = append(spans, linkSpans...)
					}
				}
			case "oMath", "oMathPara":
				if mt := extractMathText(n); mt != "" {
					spans = append(spans, textSpan{text: mt, rawMD: true})
				}
			case "br", "cr":
				brType := strings.ToLower(attrVal(n.Attrs, "type"))
				if brType == "" || brType == "textwrapping" {
					spans = append(spans, textSpan{text: "\n"})
				} else {
					spans = append(spans, textSpan{text: " "})
				}
			case "sdt", "sdtContent", "smartTag", "ins":
				walk(n.Nodes)
			case "AlternateContent":
				for _, chosen := range pickAlternateContent(n) {
					walk(chosen.Nodes)
				}
			}
		}
	}
	walk(p.Nodes)
	return spans, imgIDs, txbxNodes, sawTOC
}

func (w *docWalker) extractRun(r xmlNode) ([]textSpan, []string, []xmlNode, bool) {
	var bold, italic, code, isMono bool
	var color string
	var textBuf strings.Builder
	var imgIDs []string
	var txbxNodes []xmlNode
	sawTOC := false

	for _, n := range r.Nodes {
		switch n.XMLName.Local {
		case "rPr":
			for _, rp := range n.Nodes {
				switch rp.XMLName.Local {
				case "b":
					val := attrVal(rp.Attrs, "val")
					if val != "0" && !strings.EqualFold(val, "false") {
						bold = true
					}
				case "i":
					val := attrVal(rp.Attrs, "val")
					if val != "0" && !strings.EqualFold(val, "false") {
						italic = true
					}
				case "rStyle":
					if w.styles.IsCodeStyle(attrVal(rp.Attrs, "val")) {
						code = true
						isMono = true
					}
				case "rFonts":
					for _, a := range rp.Attrs {
						if isMonospaceFont(a.Value) {
							code = true
							isMono = true
							break
						}
					}
				case "color":
					val := strings.ToLower(strings.TrimSpace(attrVal(rp.Attrs, "val")))
					if val != "" && val != "auto" && val != "000000" && val != "ffffff" && val != "000" && val != "fff" {
						color = val
					}
				}
			}
		case "fldChar":
			ft := attrVal(n.Attrs, "fldCharType")
			switch ft {
			case "begin":
				w.pendingInstr.Reset()
				w.inInstrText = true
			case "separate":
				w.inInstrText = false
				if strings.Contains(strings.ToUpper(w.pendingInstr.String()), "TOC") {
					w.inTOCField = true
					sawTOC = true
				}
			case "end":
				w.inInstrText = false
				w.inTOCField = false
				w.pendingInstr.Reset()
			}
		case "instrText":
			w.pendingInstr.WriteString(n.Content)
			if strings.Contains(strings.ToUpper(n.Content), "TOC") {
				sawTOC = true
			}
		case "t":
			if !w.inInstrText {
				textBuf.WriteString(n.Content)
			}
		case "tab":
			if !w.inInstrText {
				textBuf.WriteByte('\t')
			}
		case "br", "cr":
			if !w.inInstrText {
				brType := strings.ToLower(attrVal(n.Attrs, "type"))
				if brType == "" || brType == "textwrapping" {
					textBuf.WriteByte('\n')
				} else {
					textBuf.WriteByte(' ')
				}
			}
		case "drawing", "pict":
			rIDs := collectImageRelIDs(n)
			for _, rid := range rIDs {
				if id := w.registerImageRel(rid); id != "" {
					imgIDs = append(imgIDs, id)
				}
			}
			txbxNodes = append(txbxNodes, collectTextBoxContents(n)...)
		case "AlternateContent":
			for _, chosen := range pickAlternateContent(n) {
				for _, cn := range chosen.Nodes {
					if cn.XMLName.Local == "drawing" || cn.XMLName.Local == "pict" {
						for _, rid := range collectImageRelIDs(cn) {
							if id := w.registerImageRel(rid); id != "" {
								imgIDs = append(imgIDs, id)
							}
						}
						txbxNodes = append(txbxNodes, collectTextBoxContents(cn)...)
					}
				}
			}
		}
	}

	raw := textBuf.String()
	if raw == "" {
		return nil, imgIDs, txbxNodes, sawTOC
	}
	return []textSpan{{
		text:   raw,
		bold:   bold,
		italic: italic,
		code:   code,
		isMono: isMono,
		color:  color,
	}}, imgIDs, txbxNodes, sawTOC
}

func collectTextBoxContents(node xmlNode) []xmlNode {
	var out []xmlNode
	var walk func(n xmlNode)
	walk = func(n xmlNode) {
		if n.XMLName.Local == "txbxContent" {
			out = append(out, n)
			return
		}
		if n.XMLName.Local == "AlternateContent" {
			for _, chosen := range pickAlternateContent(n) {
				walk(chosen)
			}
			return
		}
		for _, c := range n.Nodes {
			walk(c)
		}
	}
	walk(node)
	return out
}

func buildPlainCodeText(spans []textSpan) string {
	var b strings.Builder
	for _, sp := range spans {
		b.WriteString(sp.text)
	}
	s := strings.ReplaceAll(b.String(), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.ReplaceAll(s, "\t", "    ")
	return s
}

func renderInlineMarkdown(spans []textSpan) string {
	if len(spans) == 0 {
		return ""
	}
	norm := func(s string) string {
		r := strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ", "\t", " ", "\u00a0", " ")
		return r.Replace(s)
	}

	var merged []textSpan
	for _, sp := range spans {
		txt := sp.text
		if !sp.rawMD {
			txt = norm(txt)
		}
		if txt == "" {
			continue
		}
		cur := textSpan{
			text:   txt,
			bold:   sp.bold,
			italic: sp.italic,
			code:   sp.code,
			isMono: sp.isMono,
			rawMD:  sp.rawMD,
		}
		if len(merged) > 0 {
			prev := &merged[len(merged)-1]
			if !prev.rawMD && !cur.rawMD &&
				prev.bold == cur.bold && prev.italic == cur.italic && prev.code == cur.code {
				prev.text += cur.text
				continue
			}
		}
		merged = append(merged, cur)
	}

	var out strings.Builder
	for _, sp := range merged {
		if sp.rawMD {
			out.WriteString(sp.text)
			continue
		}
		if strings.TrimSpace(sp.text) == "" {
			out.WriteString(sp.text)
			continue
		}
		trimmed := strings.TrimSpace(sp.text)
		lead := ""
		trail := ""
		if strings.HasPrefix(sp.text, " ") {
			lead = " "
		}
		if strings.HasSuffix(sp.text, " ") {
			trail = " "
		}
		switch {
		case sp.code:
			out.WriteString(lead)
			out.WriteString(wrapInlineCode(trimmed))
			out.WriteString(trail)
		case sp.bold && sp.italic:
			out.WriteString(lead + "***" + trimmed + "***" + trail)
		case sp.bold:
			out.WriteString(lead + "**" + trimmed + "**" + trail)
		case sp.italic:
			out.WriteString(lead + "*" + trimmed + "*" + trail)
		default:
			out.WriteString(sp.text)
		}
	}
	return out.String()
}

func wrapInlineCode(s string) string {
	if !strings.Contains(s, "`") {
		return "`" + s + "`"
	}
	maxRun := 0
	cur := 0
	for _, r := range s {
		if r == '`' {
			cur++
			if cur > maxRun {
				maxRun = cur
			}
		} else {
			cur = 0
		}
	}
	ticks := strings.Repeat("`", maxRun+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		return ticks + " " + s + " " + ticks
	}
	return ticks + s + ticks
}

func collectImageRelIDs(node xmlNode) []string {
	var ids []string
	var walk func(n xmlNode)
	walk = func(n xmlNode) {
		switch n.XMLName.Local {
		case "blip":
			if rid := attrVal(n.Attrs, "embed"); rid != "" {
				ids = append(ids, rid)
			} else if rid := attrVal(n.Attrs, "link"); rid != "" {
				ids = append(ids, rid)
			}
		case "imagedata":
			if rid := attrVal(n.Attrs, "id"); rid != "" {
				ids = append(ids, rid)
			}
		}
		for _, c := range n.Nodes {
			walk(c)
		}
	}
	walk(node)
	return ids
}

func (w *docWalker) registerImageRel(relID string) string {
	rel, ok := w.rels[relID]
	if !ok || rel.External || rel.Target == "" {
		return ""
	}
	raw, err := w.sz.ReadEntry(rel.Target)
	if err != nil || len(raw) == 0 {
		return ""
	}
	w.imageCounter++
	id := fmt.Sprintf("img-%03d", w.imageCounter)
	ext := strings.ToLower(path.Ext(rel.Target))
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		ct = "application/octet-stream"
	}
	sum := sha256.Sum256(raw)
	asset := &ImageAsset{
		ID:           id,
		OrigFilename: path.Base(rel.Target),
		ContentType:  ct,
		Ext:          ext,
		RawBytes:     raw,
		SHA256:       hex.EncodeToString(sum[:]),
		SectionPath:  w.currentSectionPath(),
	}
	w.ir.Images = append(w.ir.Images, asset)
	return id
}

type tableCellData struct {
	inlineText string
	codeLines  []string
	metas      []*paraMeta
	hasShading bool
}

func (w *docWalker) handleTable(tbl xmlNode) {
	w.blockCounter++
	var rows [][]string
	var cellGrid [][]tableCellData
	// Track active vertical merges by column index
	activeVMerge := make(map[int]string)

	for _, child := range tbl.Nodes {
		if child.XMLName.Local != "tr" {
			continue
		}
		var row []string
		var cellRow []tableCellData
		colIdx := 0
		for _, tc := range child.Nodes {
			if tc.XMLName.Local != "tc" {
				continue
			}
			span := 1
			vMergeState := "" // "" | "restart" | "continue"
			cellShading := false
			var cellParas []string
			var codeLines []string
			var metas []*paraMeta
			for _, tcChild := range tc.Nodes {
				switch tcChild.XMLName.Local {
				case "tcPr":
					for _, tp := range tcChild.Nodes {
						switch tp.XMLName.Local {
						case "gridSpan":
							if v, err := strconv.Atoi(attrVal(tp.Attrs, "val")); err == nil && v > 1 {
								span = v
							}
						case "vMerge":
							val := attrVal(tp.Attrs, "val")
							if val == "restart" {
								vMergeState = "restart"
							} else {
								vMergeState = "continue"
							}
						case "shd":
							if isNonWhiteShading(attrVal(tp.Attrs, "fill")) {
								cellShading = true
							}
						}
					}
				case "p":
					var pStyleID string
					for _, pn := range tcChild.Nodes {
						if pn.XMLName.Local == "pPr" {
							for _, pr := range pn.Nodes {
								if pr.XMLName.Local == "pStyle" {
									pStyleID = attrVal(pr.Attrs, "val")
								}
							}
						}
					}
					pm, _, _, _ := w.extractParagraphMeta(tcChild, pStyleID)
					if t := strings.TrimSpace(pm.inlineMD); t != "" {
						cellParas = append(cellParas, t)
					}
					codeLines = append(codeLines, pm.plainText)
					pmCopy := pm
					metas = append(metas, &pmCopy)
				}
			}
			cellText := strings.Join(cellParas, "<br>")
			cellText = strings.ReplaceAll(cellText, "|", `\|`)

			if vMergeState == "continue" && cellText == "" {
				if prev, ok := activeVMerge[colIdx]; ok {
					cellText = prev
				}
			} else if vMergeState == "restart" {
				activeVMerge[colIdx] = cellText
			} else {
				delete(activeVMerge, colIdx)
			}

			row = append(row, cellText)
			cellRow = append(cellRow, tableCellData{
				inlineText: cellText,
				codeLines:  codeLines,
				metas:      metas,
				hasShading: cellShading,
			})
			for i := 1; i < span; i++ {
				row = append(row, "")
				cellRow = append(cellRow, tableCellData{})
			}
			colIdx += span
		}
		if len(row) > 0 {
			rows = append(rows, row)
			cellGrid = append(cellGrid, cellRow)
		}
	}

	if len(rows) == 0 {
		return
	}

	// Normalize column count across rows
	maxCols := 0
	for _, r := range rows {
		if len(r) > maxCols {
			maxCols = len(r)
		}
	}
	for i := range rows {
		for len(rows[i]) < maxCols {
			rows[i] = append(rows[i], "")
		}
	}

	if w.opts.StripRevisionTable && !w.seenMainHead && isRevisionHistoryTable(rows[0]) {
		w.ir.NoiseStats.StrippedRevisionTables++
		return
	}
	if w.opts.StripCover && !w.seenMainHead {
		w.ir.NoiseStats.StrippedCoverBlocks++
		return
	}

	if blk, ok := w.tryUnwrapCodeTable(cellGrid, maxCols); ok {
		w.appendItem(rawBlockItem{block: blk})
		return
	}

	w.appendItem(rawBlockItem{
		block: Block{
			Kind:      BlockTable,
			TableRows: rows,
		},
	})
}

func (w *docWalker) tryUnwrapCodeTable(grid [][]tableCellData, cols int) (Block, bool) {
	if len(grid) == 0 {
		return Block{}, false
	}

	// Case 1: 1x1 code-box table
	if len(grid) == 1 && cols == 1 {
		cell := grid[0][0]
		rawCode := buildTableCellCodeText(cell)
		cleaned := dedentCode(rawCode)
		if cleaned == "" || isNaturalLanguageCallout(cleaned) {
			return Block{}, false
		}
		hasFormatSignal := cell.hasShading || hasCellCodeFormatting(cell)
		if (hasFormatSignal && (isHighConfidenceCodeBlock(cleaned) || hasCellStrongCodeStyle(cell))) ||
			(!hasFormatSignal && isMultiLineCodeText(cleaned) && isHighConfidenceCodeBlock(cleaned)) {
			return Block{
				Kind:     BlockCode,
				Text:     cleaned,
				Language: DetectCodeLanguage(cleaned, w.lastNonEmptyText()),
			}, true
		}
		return Block{}, false
	}

	// Case 2: 1x2 (line numbers in col 0, code in col 1) or Nx2 (each row is lineNo | codeLine)
	if cols == 2 && len(grid[0]) == 2 {
		var numLines []string
		var codeLines []string
		var metas []*paraMeta
		hasSignal := false
		for _, row := range grid {
			if len(row) < 2 {
				return Block{}, false
			}
			for _, l := range row[0].codeLines {
				for _, sub := range strings.Split(l, "\n") {
					if t := strings.TrimSpace(sub); t != "" {
						numLines = append(numLines, t)
					}
				}
			}
			if row[1].hasShading || hasCellCodeFormatting(row[1]) {
				hasSignal = true
			}
			codeLines = append(codeLines, buildTableCellCodeText(row[1]))
			metas = append(metas, row[1].metas...)
		}
		if isSequentialLineNumbers(numLines) {
			cleaned := dedentCode(strings.Join(codeLines, "\n"))
			if cleaned != "" && !isNaturalLanguageCallout(cleaned) && (hasSignal || isHighConfidenceCodeBlock(cleaned) || len(metas) >= 2) {
				return Block{
					Kind:     BlockCode,
					Text:     cleaned,
					Language: DetectCodeLanguage(cleaned, w.lastNonEmptyText()),
				}, true
			}
		}
	}

	return Block{}, false
}

func buildTableCellCodeText(cell tableCellData) string {
	lines := applyRelativeIndent(cell.metas, cell.codeLines)
	return strings.Join(lines, "\n")
}

func hasCellStrongCodeStyle(cell tableCellData) bool {
	for _, m := range cell.metas {
		if m != nil && (m.isCodeStyle || m.monoRatio >= 0.7) {
			return true
		}
	}
	return false
}

func hasCellCodeFormatting(cell tableCellData) bool {
	if len(cell.metas) == 0 {
		return false
	}
	nonEmpty := 0
	hits := 0
	for _, m := range cell.metas {
		if m == nil || m.isEmpty {
			continue
		}
		nonEmpty++
		if m.isCodeStyle || m.hasShading || m.monoRatio >= 0.7 || m.distinctCols >= 2 {
			hits++
		}
	}
	return nonEmpty > 0 && hits*2 >= nonEmpty
}

func isSequentialLineNumbers(nums []string) bool {
	if len(nums) < 2 {
		return false
	}
	prev := -1
	for _, s := range nums {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return false
		}
		if prev == -1 {
			if n != 0 && n != 1 {
				return false
			}
		} else if n != prev+1 {
			return false
		}
		prev = n
	}
	return true
}

func isRevisionHistoryTable(headerRow []string) bool {
	joined := strings.ToLower(strings.Join(headerRow, " "))
	keywords := []string{"版本", "修订", "修改", "变更", "作者", "日期", "revision", "version", "author", "change", "history"}
	hits := 0
	for _, kw := range keywords {
		if strings.Contains(joined, kw) {
			hits++
		}
	}
	return hits >= 2
}

func extractMathText(node xmlNode) string {
	var buf strings.Builder
	var walk func(n xmlNode)
	walk = func(n xmlNode) {
		if n.XMLName.Local == "t" {
			buf.WriteString(n.Content)
		}
		for _, c := range n.Nodes {
			walk(c)
		}
	}
	walk(node)
	return buf.String()
}

func (w *docWalker) populateImageContexts() {
	imgByID := make(map[string]*ImageAsset, len(w.ir.Images))
	for _, img := range w.ir.Images {
		imgByID[img.ID] = img
	}

	for idx, blk := range w.ir.Blocks {
		if blk.Kind != BlockImage {
			continue
		}
		img := imgByID[blk.ImageID]
		if img == nil {
			continue
		}

		// Look backward for context (up to 3 text blocks)
		var before []string
		for j := idx - 1; j >= 0 && len(before) < 3; j-- {
			txt := blockSummaryText(w.ir.Blocks[j])
			if txt != "" {
				before = append([]string{txt}, before...)
			}
		}
		img.ContextBefore = truncateRunes(strings.Join(before, "\n"), 600)

		// Look forward for caption and context (up to 3 text blocks)
		var after []string
		for j := idx + 1; j < len(w.ir.Blocks) && len(after) < 3; j++ {
			txt := blockSummaryText(w.ir.Blocks[j])
			if txt != "" {
				if j == idx+1 && captionPattern.MatchString(strings.TrimSpace(txt)) {
					img.Caption = strings.TrimSpace(txt)
				}
				after = append(after, txt)
			}
		}
		if img.Caption == "" && idx > 0 {
			prevTxt := strings.TrimSpace(blockSummaryText(w.ir.Blocks[idx-1]))
			if captionPattern.MatchString(prevTxt) {
				img.Caption = prevTxt
			}
		}
		img.ContextAfter = truncateRunes(strings.Join(after, "\n"), 600)
	}
}

func blockSummaryText(b Block) string {
	switch b.Kind {
	case BlockHeading, BlockParagraph, BlockListItem:
		return strings.TrimSpace(b.Text)
	case BlockCode:
		header := "[代码块"
		if b.Language != "" {
			header += ": " + b.Language
		}
		header += "] "
		return header + truncateRunes(strings.TrimSpace(b.Text), 180)
	case BlockTable:
		if len(b.TableRows) > 0 {
			return "[表格: " + strings.Join(b.TableRows[0], " | ") + "]"
		}
	}
	return ""
}

func truncateRunes(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "..."
}

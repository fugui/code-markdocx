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
		}
	}
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
	text, imgIDs, tocFieldDetected := w.extractParagraphContent(p)
	if tocFieldDetected {
		isTOC = true
	}

	trimmed := strings.TrimSpace(text)

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
	if headingLvl == 0 && !hasList && trimmed != "" && len(imgIDs) == 0 {
		// Detect common numbered headings when paragraph lacks formal Heading style
		if m := numberedHeadingRegex.FindStringSubmatch(trimmed); len(m) == 3 {
			dots := strings.Count(m[1], ".") + 1
			if dots >= 1 && dots <= 4 && !strings.HasSuffix(trimmed, "。") && !strings.HasSuffix(trimmed, ".") {
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
		w.ir.Blocks = append(w.ir.Blocks, Block{
			Kind:  BlockHeading,
			Level: headingLvl,
			Text:  trimmed,
		})
		for _, imgID := range imgIDs {
			w.ir.Blocks = append(w.ir.Blocks, Block{Kind: BlockImage, ImageID: imgID})
		}
		return
	}

	if w.opts.StripCover && !w.seenMainHead {
		if trimmed != "" || len(imgIDs) > 0 {
			w.ir.NoiseStats.StrippedCoverBlocks++
		}
		return
	}

	if trimmed != "" {
		if hasList {
			w.ir.Blocks = append(w.ir.Blocks, Block{
				Kind:    BlockListItem,
				Text:    trimmed,
				Ordered: w.numbering.IsOrdered(numID, ilvl),
				Indent:  ilvl,
			})
		} else {
			w.ir.Blocks = append(w.ir.Blocks, Block{
				Kind: BlockParagraph,
				Text: trimmed,
			})
		}
	}
	for _, imgID := range imgIDs {
		w.ir.Blocks = append(w.ir.Blocks, Block{Kind: BlockImage, ImageID: imgID})
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

func (w *docWalker) extractParagraphContent(p xmlNode) (string, []string, bool) {
	var buf strings.Builder
	var imgIDs []string
	sawTOC := false

	var walk func(nodes []xmlNode)
	walk = func(nodes []xmlNode) {
		for _, n := range nodes {
			switch n.XMLName.Local {
			case "pPr":
				continue
			case "r":
				runText, runImgs, runTOC := w.extractRun(n)
				if runTOC {
					sawTOC = true
				}
				buf.WriteString(runText)
				imgIDs = append(imgIDs, runImgs...)
			case "hyperlink":
				linkID := attrVal(n.Attrs, "id")
				var linkBuf strings.Builder
				for _, child := range n.Nodes {
					if child.XMLName.Local == "r" {
						rt, rImgs, _ := w.extractRun(child)
						linkBuf.WriteString(rt)
						imgIDs = append(imgIDs, rImgs...)
					}
				}
				linkText := strings.TrimSpace(linkBuf.String())
				if linkText != "" {
					if rel, ok := w.rels[linkID]; ok && rel.External && rel.Target != "" {
						buf.WriteString(fmt.Sprintf("[%s](%s)", linkText, rel.Target))
					} else {
						buf.WriteString(linkText)
					}
				}
			case "oMath", "oMathPara":
				buf.WriteString(extractMathText(n))
			case "sdt", "sdtContent", "smartTag", "ins":
				walk(n.Nodes)
			}
		}
	}
	walk(p.Nodes)
	return buf.String(), imgIDs, sawTOC
}

func (w *docWalker) extractRun(r xmlNode) (string, []string, bool) {
	var bold, italic, code bool
	var textBuf strings.Builder
	var imgIDs []string
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
					}
				case "rFonts":
					for _, a := range rp.Attrs {
						if isMonospaceFont(a.Value) {
							code = true
							break
						}
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
				textBuf.WriteString(" ")
			}
		case "br", "cr":
			if !w.inInstrText {
				textBuf.WriteString(" ")
			}
		case "drawing", "pict":
			rIDs := collectImageRelIDs(n)
			for _, rid := range rIDs {
				if id := w.registerImageRel(rid); id != "" {
					imgIDs = append(imgIDs, id)
				}
			}
		}
	}

	raw := textBuf.String()
	if strings.TrimSpace(raw) == "" {
		return raw, imgIDs, sawTOC
	}
	trimmed := strings.TrimSpace(raw)
	leadSpace := ""
	trailSpace := ""
	if strings.HasPrefix(raw, " ") {
		leadSpace = " "
	}
	if strings.HasSuffix(raw, " ") {
		trailSpace = " "
	}
	if code && !strings.Contains(trimmed, "`") {
		return leadSpace + "`" + trimmed + "`" + trailSpace, imgIDs, sawTOC
	}
	if bold && italic {
		return leadSpace + "***" + trimmed + "***" + trailSpace, imgIDs, sawTOC
	}
	if bold {
		return leadSpace + "**" + trimmed + "**" + trailSpace, imgIDs, sawTOC
	}
	if italic {
		return leadSpace + "*" + trimmed + "*" + trailSpace, imgIDs, sawTOC
	}
	return raw, imgIDs, sawTOC
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

func (w *docWalker) handleTable(tbl xmlNode) {
	w.blockCounter++
	var rows [][]string
	// Track active vertical merges by column index
	activeVMerge := make(map[int]string)

	for _, child := range tbl.Nodes {
		if child.XMLName.Local != "tr" {
			continue
		}
		var row []string
		colIdx := 0
		for _, tc := range child.Nodes {
			if tc.XMLName.Local != "tc" {
				continue
			}
			span := 1
			vMergeState := "" // "" | "restart" | "continue"
			var cellParas []string
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
						}
					}
				case "p":
					txt, _, _ := w.extractParagraphContent(tcChild)
					if t := strings.TrimSpace(txt); t != "" {
						cellParas = append(cellParas, t)
					}
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
			for i := 1; i < span; i++ {
				row = append(row, "")
			}
			colIdx += span
		}
		if len(row) > 0 {
			rows = append(rows, row)
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

	w.ir.Blocks = append(w.ir.Blocks, Block{
		Kind:      BlockTable,
		TableRows: rows,
	})
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

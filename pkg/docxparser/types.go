package docxparser

type ParseOptions struct {
	StripTOC           bool
	StripCover         bool
	StripRevisionTable bool
}

type NoiseStats struct {
	StrippedTOCBlocks      int `json:"stripped_toc_blocks"`
	StrippedCoverBlocks    int `json:"stripped_cover_blocks"`
	StrippedRevisionTables int `json:"stripped_revision_tables"`
}

type BlockKind string

const (
	BlockHeading   BlockKind = "heading"
	BlockParagraph BlockKind = "paragraph"
	BlockListItem  BlockKind = "list_item"
	BlockTable     BlockKind = "table"
	BlockImage     BlockKind = "image"
	BlockCode      BlockKind = "code"
)

type Block struct {
	Kind      BlockKind  `json:"kind"`
	Level     int        `json:"level,omitempty"`
	Text      string     `json:"text,omitempty"`
	Language  string     `json:"language,omitempty"`
	Ordered   bool       `json:"ordered,omitempty"`
	Indent    int        `json:"indent,omitempty"`
	TableRows [][]string `json:"table_rows,omitempty"`
	ImageID   string     `json:"image_id,omitempty"`
}

type ImageAsset struct {
	ID                string `json:"id"`
	OrigFilename      string `json:"orig_filename"`
	ContentType       string `json:"content_type"`
	Ext               string `json:"ext"`
	RawBytes          []byte `json:"-"`
	SHA256            string `json:"sha256"`
	SectionPath       string `json:"section_path"`
	Caption           string `json:"caption"`
	ContextBefore     string `json:"context_before"`
	ContextAfter      string `json:"context_after"`
	Width             int    `json:"width"`
	Height            int    `json:"height"`
	PreSkipped        bool   `json:"pre_skipped"`
	SkipReason        string `json:"skip_reason,omitempty"`
	UnsupportedVector bool   `json:"unsupported_vector"`
	VectorWarning     string `json:"vector_warning,omitempty"`
}

type DocumentIR struct {
	Title      string        `json:"title"`
	Blocks     []Block       `json:"blocks"`
	Images     []*ImageAsset `json:"images"`
	NoiseStats NoiseStats    `json:"noise_stats"`
}

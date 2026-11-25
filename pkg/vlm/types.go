package vlm

type DiagramCategory string

const (
	CategoryUMLDiagram     DiagramCategory = "uml_diagram"
	CategoryComplexArch    DiagramCategory = "complex_arch"
	CategoryTableShot      DiagramCategory = "table_screenshot"
	CategoryCodeShot       DiagramCategory = "code_screenshot"
	CategoryUIOrDecorative DiagramCategory = "ui_or_decorative"
)

type DiagramType string

const (
	DiagramSequence  DiagramType = "sequence"
	DiagramClass     DiagramType = "class"
	DiagramState     DiagramType = "state"
	DiagramFlowchart DiagramType = "flowchart"
	DiagramER        DiagramType = "er"
	DiagramOther     DiagramType = "other"
)

type ConfidenceLevel string

const (
	ConfidenceHigh   ConfidenceLevel = "high"
	ConfidenceMedium ConfidenceLevel = "medium"
	ConfidenceLow    ConfidenceLevel = "low"
)

type VLMResult struct {
	Category        DiagramCategory `json:"category"`
	DiagramType     DiagramType     `json:"diagram_type"`
	MermaidCode     string          `json:"mermaid_code"`
	MarkdownTable   string          `json:"markdown_table"`
	CodeLanguage    string          `json:"code_language,omitempty"`
	CodeContent     string          `json:"code_content,omitempty"`
	Summary         string          `json:"summary"`
	Confidence      ConfidenceLevel `json:"confidence"`
	RecommendedSkip bool            `json:"recommended_skip"`
	Notes           string          `json:"notes,omitempty"`
}

type ConvertImageInput struct {
	ImageID       string
	ImageBytes    []byte
	ContentType   string
	SectionPath   string
	Caption       string
	ContextBefore string
	ContextAfter  string
	GlossaryHints string
}

type RepairImageInput struct {
	ConvertImageInput
	PreviousResult *VLMResult
	MermaidError   string
	Instruction    string
}

type BatchProgress struct {
	ImageID string
	Done    int
	Total   int
	Result  *VLMResult
	Err     error
}

type CodePolishMode string

const (
	CodePolishFormat CodePolishMode = "format_code"
	CodePolishDetect CodePolishMode = "detect_code"
)

type CodePolishInput struct {
	ID            string
	Mode          CodePolishMode
	SectionPath   string
	ContextBefore string
	Lines         []string
	DetectedLang  string
	GlossaryHints string
}

type CodePolishResult struct {
	IsCode        bool   `json:"is_code"`
	Language      string `json:"language"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
	FormattedCode string `json:"formatted_code"`
	Notes         string `json:"notes,omitempty"`
}

type CodePolishProgress struct {
	ID     string
	Done   int
	Total  int
	Result *CodePolishResult
	Err    error
}

type LayoutOpType string

const (
	LayoutOpSetHeading      LayoutOpType = "set_heading"
	LayoutOpSetParagraph    LayoutOpType = "set_paragraph"
	LayoutOpSetListItem     LayoutOpType = "set_list_item"
	LayoutOpSetQuote        LayoutOpType = "set_quote"
	LayoutOpSetAlert        LayoutOpType = "set_alert"
	LayoutOpMergeParagraphs LayoutOpType = "merge_paragraphs"
)

type LayoutBlockSummary struct {
	Index       int    `json:"index"`
	Kind        string `json:"kind"`
	Level       int    `json:"level,omitempty"`
	Ordered     bool   `json:"ordered,omitempty"`
	AlertType   string `json:"alert_type,omitempty"`
	TextPreview string `json:"text_preview"`
	CharCount   int    `json:"char_count"`
	Immutable   bool   `json:"immutable,omitempty"`
}

type LayoutPolishInput struct {
	ChunkID       string
	DocTitle      string
	GlossaryHints string
	Blocks        []LayoutBlockSummary
}

type LayoutPolishOp struct {
	Op         LayoutOpType `json:"op"`
	Block      int          `json:"block,omitempty"`
	Level      int          `json:"level,omitempty"`
	Ordered    bool         `json:"ordered,omitempty"`
	Indent     int          `json:"indent,omitempty"`
	AlertType  string       `json:"alert_type,omitempty"`
	StartBlock int          `json:"start_block,omitempty"`
	EndBlock   int          `json:"end_block,omitempty"`
}

type LayoutPolishResult struct {
	Ops     []LayoutPolishOp `json:"ops"`
	Summary string           `json:"summary"`
}

type LayoutPolishProgress struct {
	ChunkID string
	Done    int
	Total   int
	Result  *LayoutPolishResult
	Err     error
}

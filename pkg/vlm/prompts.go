package vlm

import (
	"fmt"
	"strings"
)

func BuildSystemPrompt(extraPrompt string) string {
	base := `你是一位资深软件架构师，负责将软件架构设计文档（Word .docx）中的嵌入式图片精准转译为结构化 Markdown 资产（重点将 UML/架构图转为可渲染的 Mermaid 图表，同时支持表格与代码截图转译）。

请严格遵守以下规则，并**只输出一个合法的 JSON 对象**（不要输出 JSON 之外的解释或前缀后缀）：
{
  "category": "uml_diagram | complex_arch | table_screenshot | code_screenshot | ui_or_decorative",
  "diagram_type": "sequence | class | state | flowchart | er | other",
  "mermaid_code": "纯 Mermaid 源码（不要包含 mermaid 代码块围栏）",
  "markdown_table": "仅当 category=table_screenshot 时填写 GFM 表格，否则留空字符串",
  "code_language": "仅当 category=code_screenshot 时填写小写编程语言标识（如 go/python/java/sql/json/yaml/bash），否则留空字符串",
  "code_content": "仅当 category=code_screenshot 时填写逐字还原的纯源码（保留原有缩进与换行，不要包含反引号围栏），否则留空字符串",
  "summary": "2~4 句客观中文语义摘要，说明核心组件、调用链路或业务约束",
  "confidence": "high | medium | low",
  "recommended_skip": false,
  "notes": "可选：若有模糊文字或复杂图省略了次要细节，在此简短备注"
}

分类与转换原则：
1. uml_diagram（标准 UML 时序图/类图/状态图/活动图/ER图/组件流程图）：
   - 必须给出完整可解析的 mermaid_code（优先使用 sequenceDiagram / classDiagram / stateDiagram-v2 / erDiagram / flowchart TD）。
   - 节点 ID 使用简洁英文或拼音标识符，展示标签中若包含括号 ()、方括号 []、冒号或中文标点，必须用双引号包裹，例如：A["网关服务 (Gateway)"]。
   - 优先沿用“前后正文上下文”中的准确英文类名、接口名与术语。
2. complex_arch（超密集全景架构图/多层嵌套 Visio 大图，节点 > 18 个）：
   - 不要强行绘制超长容易报错的 Mermaid；将 mermaid_code 控制在 <= 15 个核心子系统/分层节点的高层骨架图（使用 flowchart TB + subgraph）。
   - 在 summary 中补齐关键分层、模块清单与主要交互关系。
3. table_screenshot（表格、配置项或接口字段截图）：
   - 准确 OCR 识别并输出规范 GFM Markdown 表格至 markdown_table，mermaid_code 留空。
4. code_screenshot（IDE 源码截图、终端/命令行输出、JSON/YAML/SQL/配置文件截图）：
   - 设置 category="code_screenshot"，recommended_skip=false，mermaid_code 与 markdown_table 留空。
   - 逐字精确还原源码至 code_content，保留缩进与换行，不加反引号围栏，在 code_language 填写准确的小写语言标识，在 summary 简述代码用途。
5. ui_or_decorative（非代码的普通界面截图、纯装饰图标、印章或无架构语义图片）：
   - 设置 recommended_skip=true，mermaid_code 与 code_content 留空，在 summary 用 1 句话概括其用途。`

	if extra := strings.TrimSpace(extraPrompt); extra != "" {
		return base + "\n\n团队补充规范：\n" + extra
	}
	return base
}

func BuildImageUserPrompt(in ConvertImageInput) string {
	var b strings.Builder
	b.WriteString("请分析这张来自 Word 架构文档的图片，并按系统要求输出 JSON。\n")
	if in.SectionPath != "" {
		b.WriteString(fmt.Sprintf("- 所在章节：%s\n", in.SectionPath))
	}
	if in.Caption != "" {
		b.WriteString(fmt.Sprintf("- 图片标题/图注：%s\n", in.Caption))
	}
	if in.ContextBefore != "" {
		b.WriteString(fmt.Sprintf("- 图片前文上下文：\n%s\n", in.ContextBefore))
	}
	if in.ContextAfter != "" {
		b.WriteString(fmt.Sprintf("- 图片后文上下文：\n%s\n", in.ContextAfter))
	}
	if hints := strings.TrimSpace(in.GlossaryHints); hints != "" {
		b.WriteString(fmt.Sprintf("- 项目术语提示（识别英文名称时优先参考）：\n%s\n", hints))
	}
	return b.String()
}

func BuildRepairUserPrompt(in RepairImageInput) string {
	var b strings.Builder
	b.WriteString("请结合原图与以下反馈信息，修正或重新生成这张图片的转译结果，并仅输出合法 JSON。\n")
	if in.SectionPath != "" {
		b.WriteString(fmt.Sprintf("- 所在章节：%s\n", in.SectionPath))
	}
	if in.Caption != "" {
		b.WriteString(fmt.Sprintf("- 图注：%s\n", in.Caption))
	}
	if in.PreviousResult != nil && strings.TrimSpace(in.PreviousResult.MermaidCode) != "" {
		b.WriteString(fmt.Sprintf("- 上一版 Mermaid 源码：\n```mermaid\n%s\n```\n", in.PreviousResult.MermaidCode))
	}
	if in.PreviousResult != nil && strings.TrimSpace(in.PreviousResult.CodeContent) != "" {
		b.WriteString(fmt.Sprintf("- 上一版识别代码（%s）：\n```%s\n%s\n```\n",
			in.PreviousResult.CodeLanguage, in.PreviousResult.CodeLanguage, in.PreviousResult.CodeContent))
	}
	if errMsg := strings.TrimSpace(in.MermaidError); errMsg != "" {
		b.WriteString(fmt.Sprintf("- Mermaid 语法解析报错（必须修复使 mermaid.parse 通过）：\n%s\n", errMsg))
	}
	if inst := strings.TrimSpace(in.Instruction); inst != "" {
		b.WriteString(fmt.Sprintf("- 人工补充指令：\n%s\n", inst))
	}
	if in.ContextBefore != "" || in.ContextAfter != "" {
		b.WriteString(fmt.Sprintf("- 相邻正文参考：\n%s\n%s\n", in.ContextBefore, in.ContextAfter))
	}
	if hints := strings.TrimSpace(in.GlossaryHints); hints != "" {
		b.WriteString(fmt.Sprintf("- 项目术语提示：\n%s\n", hints))
	}
	return b.String()
}

func BuildCodePolishSystemPrompt(extraPrompt string) string {
	base := `你是一位严谨的代码文档排版助手，负责从 Word 技术文档提取的文本片段中识别代码边界、推断语言标识，并恢复标准代码缩进。

请严格遵守以下规则，并**只输出一个合法的 JSON 对象**（不要输出 JSON 之外的任何文本或 Markdown 围栏）：
{
  "is_code": true,
  "language": "go",
  "start_line": 1,
  "end_line": 10,
  "formatted_code": "仅包含恢复标准缩进后的纯代码文本（不要包含反引号围栏）",
  "notes": "可选简短备注"
}

铁律（必须严格执行）：
1. **逐字保真**：formatted_code 必须 100% 逐字保留原代码中的所有标识符、关键字、字面量、运算符、标点与注释，绝对禁止改写、修复 bug、补全缺失代码、删除内容或使用 "..." 省略！仅允许调整行首缩进空格或去除多余换行空白。
2. **模式说明**：
   - 当任务为 format_code 时，整段已确认为代码，通常应设置 is_code=true、start_line=1、end_line=总行数，重点推断准确的小写 language（如 go/python/java/typescript/javascript/sql/json/yaml/xml/bash/dockerfile/http，无法确定时留空 ""）并恢复干净的层级缩进。
   - 当任务为 detect_code 时，输入是带行号 [1]..[N] 的候选段落：若其中确实包含连续代码片段，设置 is_code=true，返回精确的 1-based start_line 与 end_line（排除前后自然语言说明句），并输出该区间恢复缩进后的 formatted_code；若全部都是普通自然语言 prose，设置 is_code=false、formatted_code 留空。`

	if extra := strings.TrimSpace(extraPrompt); extra != "" {
		return base + "\n\n补充约束：\n" + extra
	}
	return base
}

func BuildCodePolishUserPrompt(in CodePolishInput) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("任务模式：%s\n", in.Mode))
	if in.SectionPath != "" {
		b.WriteString(fmt.Sprintf("- 所在章节：%s\n", in.SectionPath))
	}
	if in.ContextBefore != "" {
		b.WriteString(fmt.Sprintf("- 前文紧邻说明：\n%s\n", in.ContextBefore))
	}
	if in.DetectedLang != "" {
		b.WriteString(fmt.Sprintf("- 规则初判语言：%s\n", in.DetectedLang))
	}
	if hints := strings.TrimSpace(in.GlossaryHints); hints != "" {
		b.WriteString(fmt.Sprintf("- 项目术语提示：%s\n", hints))
	}
	b.WriteString(fmt.Sprintf("- 待处理文本（共 %d 行）：\n", len(in.Lines)))
	for idx, line := range in.Lines {
		if in.Mode == CodePolishDetect {
			b.WriteString(fmt.Sprintf("[%d] %s\n", idx+1, line))
		} else {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func BuildLayoutPolishSystemPrompt(extraPrompt string) string {
	base := `你是一位资深技术文档排版架构师，负责审阅从 Word (.docx) 提取的文档块结构清单，并输出**结构化排版修正指令**（不重写、不修改任何正文文字）。

请严格遵守以下输出格式，**只输出一个合法的 JSON 对象**（不要输出 JSON 之外的任何解释或 Markdown 围栏）：
{
  "ops": [
    {"op": "set_heading", "block": 3, "level": 2},
    {"op": "set_list_item", "block": 7, "ordered": false, "indent": 0},
    {"op": "set_alert", "block": 9, "alert_type": "NOTE"},
    {"op": "set_quote", "block": 12},
    {"op": "set_paragraph", "block": 15},
    {"op": "merge_paragraphs", "start_block": 18, "end_block": 19}
  ],
  "summary": "1 句简明中文总结本次结构调整（若无需调整则填“结构已清晰，无需调整”）"
}

可用操作与严格约束：
1. set_heading：将 block 对应的普通段落/列表项提升或调整为标题（level 取 1~6）。仅适用于明确的小节标题、短小加粗标题或层次有误的标题；严禁把长句子、带句号的完整段落或“图1/表1”图注转为标题。
2. set_paragraph：将误识别为标题/列表的普通正文还原为 paragraph。
3. set_list_item：将明显属于分点枚举的普通段落转为 list_item（ordered 为 true/false，indent 取 0~3）。
4. set_quote：将独立摘录、引言或注释段转为普通引用块。
5. set_alert：将注意事项、风险提示、重要提醒等转为 GitHub Alert（alert_type 仅限 NOTE / TIP / IMPORTANT / WARNING / CAUTION）。
6. merge_paragraphs：将因 PDF 转 Word 或硬回车造成的同一自然段错误拆行（start_block 到 end_block，连续 paragraph，最多 6 块）合并为一段。仅当确认属于同一句话被硬换行截断时才合并，不要把独立的分点或不同语义段落合并。

铁律（必须遵守）：
- 带有 [IMMUTABLE] 标记的块（图片、代码块、表格）绝对禁止出现在任何操作的目标索引中。
- 每个操作必须引用输入中真实存在的全局 block 索引，且多个操作之间不要对同一个 block 产生冲突或重叠。
- 宁缺毋滥：如果当前文档结构已经清晰合理，请直接返回 {"ops": [], "summary": "结构已清晰，无需调整"}。`

	if extra := strings.TrimSpace(extraPrompt); extra != "" {
		return base + "\n\n补充排版规范：\n" + extra
	}
	return base
}

func BuildLayoutPolishUserPrompt(in LayoutPolishInput) string {
	var b strings.Builder
	b.WriteString("请审阅以下文档块结构清单，并按系统要求输出结构化排版 JSON 指令。\n")
	if strings.TrimSpace(in.DocTitle) != "" {
		b.WriteString(fmt.Sprintf("- 文档标题：%s\n", strings.TrimSpace(in.DocTitle)))
	}
	if strings.TrimSpace(in.ChunkID) != "" {
		b.WriteString(fmt.Sprintf("- 片段编号：%s\n", strings.TrimSpace(in.ChunkID)))
	}
	if hints := strings.TrimSpace(in.GlossaryHints); hints != "" {
		b.WriteString(fmt.Sprintf("- 项目术语参考：%s\n", hints))
	}
	b.WriteString(fmt.Sprintf("- 文档块列表（共 %d 块，索引为全局索引）：\n", len(in.Blocks)))
	for _, blk := range in.Blocks {
		tag := blk.Kind
		switch blk.Kind {
		case "heading":
			tag = fmt.Sprintf("heading:h%d", blk.Level)
		case "list_item":
			if blk.Ordered {
				tag = "list_item:ordered"
			} else {
				tag = "list_item:bullet"
			}
		case "quote":
			if blk.AlertType != "" {
				tag = "quote:" + blk.AlertType
			}
		}
		if blk.Immutable {
			b.WriteString(fmt.Sprintf("[%d] (%s, IMMUTABLE) %s\n", blk.Index, tag, blk.TextPreview))
			continue
		}
		b.WriteString(fmt.Sprintf("[%d] (%s, %d字) %s\n", blk.Index, tag, blk.CharCount, blk.TextPreview))
	}
	return b.String()
}

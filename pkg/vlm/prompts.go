package vlm

import (
	"fmt"
	"strings"
)

func BuildSystemPrompt(extraPrompt string) string {
	base := `你是一位资深软件架构师，负责将软件架构设计文档（Word .docx）中的嵌入式图片精准转译为结构化 Markdown 资产（重点将 UML/架构图转为可渲染的 Mermaid 图表）。

请严格遵守以下规则，并**只输出一个合法的 JSON 对象**（不要输出 JSON 之外的解释或前缀后缀）：
{
  "category": "uml_diagram | complex_arch | table_screenshot | ui_or_decorative",
  "diagram_type": "sequence | class | state | flowchart | er | other",
  "mermaid_code": "纯 Mermaid 源码（不要包含 mermaid 代码块围栏）",
  "markdown_table": "仅当 category=table_screenshot 时填写 GFM 表格，否则留空字符串",
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
4. ui_or_decorative（界面截图、纯装饰图标、印章或无架构语义图片）：
   - 设置 recommended_skip=true，mermaid_code 留空，在 summary 用 1 句话概括其用途。`

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

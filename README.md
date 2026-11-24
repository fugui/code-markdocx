# Code-Markdocx — Word (.docx) 转 Markdown & UML 转 Mermaid 工作台

面向 `code-archdoc` 与企业研发文档的轻量级、**零数据库、零登录、单二进制** `.docx → Markdown` 智能转换子系统（同时支持 CLI 批量模式与 `code-bench` 微前端集成）。

## 核心特性

1. **纯 Go 确定性 `.docx` 解析 (`CGO_ENABLED=0`)**：
   - 基于标准库解压与 XML 流式解析，提取标题层级、有序/无序列表、合并单元格表格与行内格式。
   - 自动去噪：过滤 Word 自动目录 (TOC)、文首修订记录表、重复小图标/页眉 Logo，并保留图片前后正文上下文与图注。
   - EMF/WMF 矢量图：若环境安装 LibreOffice (`soffice`) 则自动转 PNG，未安装则在界面显式标黄提示降级。
2. **多模态 VLM 分类转译（静态 `config.yaml` 配置）**：
   - 携带所在章节路径、图注与前后文上下文调用 OpenAI-compatible 视觉接口：
     - `uml_diagram` → Mermaid (`sequenceDiagram` / `classDiagram` / `stateDiagram-v2` / `erDiagram` / `flowchart`) + 中文摘要
     - `complex_arch` → 控制节点规模的高层骨架 Mermaid + 结构化要点说明
     - `table_screenshot` → OCR 还原为 GFM Markdown 表格
     - `ui_or_decorative` → 建议跳过
3. **图文对照审阅与 `mermaid.parse()` 语法一键修复**：
   - 左侧原图放大对照，右侧实时渲染 Mermaid；前端自动校验语法并在出错时提供「AI 一键修复」和「指令重转」。
   - 内建 `code-archdoc` 合规预检（512 KB 体积预警、Token 估算、零 Base64、疑似凭据提醒，支持按二级标题拆分导出 `.zip`）。

## 快速开始

### 1. 构建与启动 Web 工作台

```bash
cp config.yaml.example config.yaml
# 编辑 config.yaml 填入 vlm.base_url / vlm.api_key / vlm.model（修改需重启进程）

make build
./code-markdocx-server -config config.yaml
```

- 独立访问：`http://127.0.0.1:8105`（无登录门槛）
- 集成到 `code-bench`：在 `code-bench/config.yaml` 的 `gateways` 注册 `markdocx: { url: "http://127.0.0.1:8105", ... }` 后即可在门户侧边栏直接打开。

### 2. CLI 批量转换

```bash
# 带 VLM 转译并输出离线图文对照 HTML
./code-markdocx-server convert --config config.yaml --review-html review.html -o arch.md arch.docx

# 仅提取纯文本与占位（不调 VLM）
./code-markdocx-server convert --no-vlm -o arch.md arch.docx
```

## 质量门禁

```bash
make lint-arch   # 校验 pkg/* 零 Web/DB 框架依赖
make test        # 运行 Go 全量单测 (-race) 与前端纯函数单测
make lint        # TypeScript 严格检查与 ESLint
make build       # 构建前端联邦模块与 CGO_ENABLED=0 单二进制
```

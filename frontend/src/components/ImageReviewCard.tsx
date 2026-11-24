import React, { useEffect, useState } from 'react';
import mermaid from 'mermaid';
import {
  AlertTriangle,
  CheckCircle2,
  EyeOff,
  Image as ImageIcon,
  Loader2,
  Maximize2,
  RefreshCw,
  Save,
  Sparkles,
  Wrench,
  X,
} from 'lucide-react';
import { MermaidDiagram } from '@code/common/mermaid';
import { getImageUrl } from '../api/client';
import type { DiagramCategory, DiagramType, ImageState } from '../types/api';

export interface ImageReviewCardProps {
  jobId: string;
  image: ImageState;
  syntaxError?: string;
  vlmConfigured: boolean;
  highlighted?: boolean;
  onSave: (
    imageId: string,
    patch: {
      category?: DiagramCategory;
      diagram_type?: DiagramType;
      mermaid_code?: string;
      markdown_table?: string;
      summary?: string;
      skipped?: boolean;
      keep_original?: boolean;
    },
  ) => Promise<void>;
  onRetry: (
    imageId: string,
    payload: { mermaid_error?: string; instruction?: string },
  ) => Promise<void>;
}

const CATEGORY_LABELS: Record<DiagramCategory, string> = {
  uml_diagram: 'UML 图表',
  complex_arch: '复杂架构图 (骨架)',
  table_screenshot: '表格截图',
  ui_or_decorative: '界面 / 装饰图',
};

export const ImageReviewCard: React.FC<ImageReviewCardProps> = ({
  jobId,
  image,
  syntaxError,
  vlmConfigured,
  highlighted = false,
  onSave,
  onRetry,
}) => {
  const [category, setCategory] = useState<DiagramCategory>(
    image.category || 'uml_diagram',
  );
  const [diagramType, setDiagramType] = useState<DiagramType>(
    image.diagram_type || 'flowchart',
  );
  const [mermaidCode, setMermaidCode] = useState(image.mermaid_code || '');
  const [markdownTable, setMarkdownTable] = useState(image.markdown_table || '');
  const [summary, setSummary] = useState(image.summary || '');
  const [instruction, setInstruction] = useState('');
  const [saving, setSaving] = useState(false);
  const [retrying, setRetrying] = useState(false);
  const [lightboxOpen, setLightboxOpen] = useState(false);

  useEffect(() => {
    setCategory(image.category || 'uml_diagram');
    setDiagramType(image.diagram_type || 'flowchart');
    setMermaidCode(image.mermaid_code || '');
    setMarkdownTable(image.markdown_table || '');
    setSummary(image.summary || '');
  }, [
    image.category,
    image.diagram_type,
    image.mermaid_code,
    image.markdown_table,
    image.summary,
  ]);

  const imgSrc = getImageUrl(jobId, image.image_id);
  const isSkipped = image.skipped || image.status === 'skipped';
  const isKeepOriginal = !isSkipped && Boolean(image.keep_original && image.asset_filename);
  const canKeepOriginal = Boolean(image.has_image_file && image.asset_filename && !image.unsupported_vector);
  const isTable = category === 'table_screenshot';
  const hasEdits = isKeepOriginal
    ? summary !== (image.summary || '')
    : mermaidCode !== (image.mermaid_code || '') ||
      markdownTable !== (image.markdown_table || '') ||
      summary !== (image.summary || '') ||
      category !== (image.category || 'uml_diagram') ||
      diagramType !== (image.diagram_type || 'flowchart');

  const handleSave = async () => {
    setSaving(true);
    try {
      await onSave(
        image.image_id,
        isKeepOriginal
          ? { summary, keep_original: true }
          : {
              category,
              diagram_type: diagramType,
              mermaid_code: mermaidCode,
              markdown_table: markdownTable,
              summary,
              skipped: false,
            },
      );
    } finally {
      setSaving(false);
    }
  };

  const handleToggleSkip = async () => {
    setSaving(true);
    try {
      await onSave(image.image_id, { skipped: !isSkipped });
    } finally {
      setSaving(false);
    }
  };

  const handleToggleKeepOriginal = async () => {
    setSaving(true);
    try {
      await onSave(image.image_id, { keep_original: !isKeepOriginal });
    } finally {
      setSaving(false);
    }
  };

  const handleRetry = async (payload: { mermaid_error?: string; instruction?: string }) => {
    setRetrying(true);
    try {
      await onRetry(image.image_id, payload);
    } finally {
      setRetrying(false);
    }
  };

  return (
    <div
      id={`image-card-${image.image_id}`}
      className={`code-markdocx-review-card ${isSkipped ? 'is-skipped' : ''} ${
        !isKeepOriginal && (syntaxError || image.status === 'failed' || image.unsupported_vector)
          ? 'has-warning'
          : ''
      } ${highlighted ? 'is-highlighted' : ''}`}
    >
      <div className="code-markdocx-review-header">
        <div className="code-markdocx-review-meta">
          <span className="code-markdocx-badge is-id">{image.image_id}</span>
          <span className="code-markdocx-section-crumb">
            {image.section_path || '未归属章节'}
          </span>
          {image.caption && (
            <span className="code-markdocx-caption-tag">{image.caption}</span>
          )}
        </div>

        <div className="code-markdocx-review-badges">
          {isKeepOriginal && (
            <span className="code-markdocx-pill is-info">引用原图</span>
          )}
          {image.category && !isKeepOriginal && (
            <span className="code-markdocx-pill">{CATEGORY_LABELS[image.category]}</span>
          )}
          {image.confidence && !isKeepOriginal && (
            <span
              className={`code-markdocx-pill ${
                image.confidence === 'low' ? 'is-warning' : 'is-neutral'
              }`}
            >
              置信度: {image.confidence}
            </span>
          )}
          <span
            className={`code-markdocx-pill ${
              image.status === 'done'
                ? 'is-success'
                : image.status === 'failed' || image.unsupported_vector
                  ? 'is-danger'
                  : 'is-neutral'
            }`}
          >
            {image.status}
          </span>
          {canKeepOriginal && (
            <button
              type="button"
              className={`code-markdocx-btn is-sm ${isKeepOriginal ? 'is-primary' : 'is-secondary'}`}
              disabled={saving}
              onClick={handleToggleKeepOriginal}
              title={
                isKeepOriginal
                  ? '取消原图引用，恢复为 Mermaid/文字模式'
                  : '在 Markdown 中直接引用原图 assets/，并支持打包下载'
              }
            >
              <ImageIcon size={14} />
              <span>{isKeepOriginal ? '取消原图' : '引用原图'}</span>
            </button>
          )}
          <button
            type="button"
            className="code-markdocx-btn is-ghost is-sm"
            disabled={saving}
            onClick={handleToggleSkip}
            title={isSkipped ? '恢复包含到 Markdown' : '在导出 Markdown 中跳过此图'}
          >
            <EyeOff size={14} />
            <span>{isSkipped ? '恢复此图' : '跳过此图'}</span>
          </button>
        </div>
      </div>

      <div className="code-markdocx-review-grid">
        {/* 左侧：原图与上下文 */}
        <div className="code-markdocx-review-left">
          {image.unsupported_vector ? (
            <div className="code-markdocx-vector-box">
              <AlertTriangle size={20} />
              <div>
                <strong>检测到矢量图 ({image.orig_filename})</strong>
                <p>{image.vector_warning}</p>
              </div>
            </div>
          ) : (
            <div className="code-markdocx-orig-img-wrap">
              <img
                src={imgSrc}
                alt={image.caption || image.orig_filename}
                className="code-markdocx-orig-img"
                onClick={() => setLightboxOpen(true)}
              />
              <button
                type="button"
                className="code-markdocx-zoom-btn"
                onClick={() => setLightboxOpen(true)}
                title="放大查看原图"
              >
                <Maximize2 size={14} />
                <span>放大 ({image.width}×{image.height})</span>
              </button>
            </div>
          )}

          {(image.context_before || image.context_after) && (
            <details className="code-markdocx-context-details">
              <summary>查看提取的图文上下文</summary>
              {image.context_before && (
                <div className="code-markdocx-context-block">
                  <strong>前文：</strong>
                  <p>{image.context_before}</p>
                </div>
              )}
              {image.context_after && (
                <div className="code-markdocx-context-block">
                  <strong>后文：</strong>
                  <p>{image.context_after}</p>
                </div>
              )}
            </details>
          )}
        </div>

        {/* 右侧：Mermaid 实时渲染 / 源码编辑 / 原图模式 / 修复 */}
        <div className="code-markdocx-review-right">
          {isSkipped ? (
            <div className="code-markdocx-skipped-box">
              已跳过此图（{image.skip_reason || '人工跳过'}），不会输出到最终 Markdown。
            </div>
          ) : isKeepOriginal ? (
            <>
              <div className="code-markdocx-original-box">
                <div className="code-markdocx-original-box-head">
                  <ImageIcon size={16} />
                  <strong>已启用原图引用模式</strong>
                </div>
                <p className="code-markdocx-muted">
                  导出 Markdown 时将保留相对路径引用，顶部可点击「打包下载 (.zip 含原图)」一并导出：
                </p>
                <code className="code-markdocx-original-syntax">
                  {`![${image.caption || image.orig_filename || image.image_id}](assets/${image.asset_filename})`}
                </code>
              </div>

              <label className="code-markdocx-field">
                <span>语义摘要（可选，将紧随图片写入说明）</span>
                <textarea
                  rows={2}
                  className="code-markdocx-textarea"
                  value={summary}
                  onChange={(e) => setSummary(e.target.value)}
                />
              </label>

              <div className="code-markdocx-card-actions">
                <button
                  type="button"
                  className="code-markdocx-btn is-secondary is-sm"
                  disabled={saving}
                  onClick={handleToggleKeepOriginal}
                >
                  <span>切换回 Mermaid / 文本模式</span>
                </button>
                <button
                  type="button"
                  className={`code-markdocx-btn is-sm ${
                    hasEdits ? 'is-primary' : 'is-secondary'
                  }`}
                  disabled={saving || !hasEdits}
                  onClick={handleSave}
                >
                  {saving ? (
                    <Loader2 size={14} className="code-markdocx-spin" />
                  ) : (
                    <Save size={14} />
                  )}
                  <span>保存修改</span>
                </button>
              </div>
            </>
          ) : image.status === 'processing' ? (
            <div className="code-markdocx-processing-box">
              <Loader2 size={18} className="code-markdocx-spin" />
              <span>VLM 正在识图转译中...</span>
            </div>
          ) : (
            <>
              {image.status === 'failed' && image.error && (
                <div className="code-markdocx-alert is-danger">
                  <AlertTriangle size={16} />
                  <div className="code-markdocx-syntax-err-body">
                    <span>{image.error}</span>
                    {canKeepOriginal && (
                      <button
                        type="button"
                        className="code-markdocx-btn is-secondary is-sm"
                        disabled={saving}
                        onClick={handleToggleKeepOriginal}
                      >
                        <ImageIcon size={14} />
                        <span>改为引用原图</span>
                      </button>
                    )}
                  </div>
                </div>
              )}

              {syntaxError && !isTable && (
                <div className="code-markdocx-alert is-danger">
                  <AlertTriangle size={16} />
                  <div className="code-markdocx-syntax-err-body">
                    <div>
                      <strong>Mermaid 语法校验未通过：</strong>
                      <code>{syntaxError}</code>
                    </div>
                    {vlmConfigured && !image.unsupported_vector && (
                      <button
                        type="button"
                        className="code-markdocx-btn is-warning is-sm"
                        disabled={retrying}
                        onClick={() => handleRetry({ mermaid_error: syntaxError })}
                      >
                        {retrying ? (
                          <Loader2 size={14} className="code-markdocx-spin" />
                        ) : (
                          <Wrench size={14} />
                        )}
                        <span>AI 一键修复</span>
                      </button>
                    )}
                  </div>
                </div>
              )}

              {!isTable && mermaidCode.trim() && !syntaxError && (
                <div className="code-markdocx-mermaid-preview">
                  <div className="code-markdocx-syntax-ok">
                    <CheckCircle2 size={14} />
                    <span>Mermaid 语法校验通过</span>
                  </div>
                  <MermaidDiagram
                    chart={mermaidCode}
                    mermaid={mermaid}
                    inlineMaxHeight={360}
                    exportFilenamePrefix={image.image_id}
                  />
                </div>
              )}

              <div className="code-markdocx-editor-row">
                <label className="code-markdocx-field">
                  <span>分类</span>
                  <select
                    value={category}
                    onChange={(e) => setCategory(e.target.value as DiagramCategory)}
                  >
                    <option value="uml_diagram">UML 图表</option>
                    <option value="complex_arch">复杂架构图</option>
                    <option value="table_screenshot">表格截图</option>
                    <option value="ui_or_decorative">界面/装饰图</option>
                  </select>
                </label>
                {!isTable && (
                  <label className="code-markdocx-field">
                    <span>图类型</span>
                    <select
                      value={diagramType}
                      onChange={(e) => setDiagramType(e.target.value as DiagramType)}
                    >
                      <option value="sequence">时序图 (sequence)</option>
                      <option value="class">类图 (class)</option>
                      <option value="state">状态图 (state)</option>
                      <option value="flowchart">流程图 (flowchart)</option>
                      <option value="er">ER 图 (er)</option>
                      <option value="other">其他</option>
                    </select>
                  </label>
                )}
              </div>

              {isTable ? (
                <label className="code-markdocx-field">
                  <span>Markdown 表格源码</span>
                  <textarea
                    rows={6}
                    className="code-markdocx-code-textarea"
                    value={markdownTable}
                    onChange={(e) => setMarkdownTable(e.target.value)}
                  />
                </label>
              ) : (
                <label className="code-markdocx-field">
                  <span>Mermaid 源码（支持直接微调）</span>
                  <textarea
                    rows={7}
                    className="code-markdocx-code-textarea"
                    value={mermaidCode}
                    onChange={(e) => setMermaidCode(e.target.value)}
                    placeholder="sequenceDiagram&#10;  Client->>Gateway: Request"
                  />
                </label>
              )}

              <label className="code-markdocx-field">
                <span>语义摘要（将作为图表说明写入 Markdown）</span>
                <textarea
                  rows={2}
                  className="code-markdocx-textarea"
                  value={summary}
                  onChange={(e) => setSummary(e.target.value)}
                />
              </label>

              <div className="code-markdocx-card-actions">
                <div className="code-markdocx-retry-bar">
                  <input
                    type="text"
                    className="code-markdocx-input"
                    placeholder="补充重转指令，如：请改用时序图 / 保留中文节点名"
                    value={instruction}
                    onChange={(e) => setInstruction(e.target.value)}
                  />
                  <button
                    type="button"
                    className="code-markdocx-btn is-secondary is-sm"
                    disabled={!vlmConfigured || retrying || image.unsupported_vector}
                    onClick={() =>
                      handleRetry({ instruction: instruction || '请结合上下文重新识图生成' })
                    }
                  >
                    {retrying ? (
                      <Loader2 size={14} className="code-markdocx-spin" />
                    ) : instruction.trim() ? (
                      <Sparkles size={14} />
                    ) : (
                      <RefreshCw size={14} />
                    )}
                    <span>{instruction.trim() ? '指令重转' : '重新识别'}</span>
                  </button>
                </div>

                <button
                  type="button"
                  className={`code-markdocx-btn is-sm ${
                    hasEdits ? 'is-primary' : 'is-secondary'
                  }`}
                  disabled={saving || !hasEdits}
                  onClick={handleSave}
                >
                  {saving ? <Loader2 size={14} className="code-markdocx-spin" /> : <Save size={14} />}
                  <span>保存修改</span>
                </button>
              </div>
            </>
          )}
        </div>
      </div>

      {lightboxOpen && (
        <div
          className="code-markdocx-lightbox"
          onClick={() => setLightboxOpen(false)}
        >
          <button
            type="button"
            className="code-markdocx-lightbox-close"
            onClick={() => setLightboxOpen(false)}
          >
            <X size={20} />
          </button>
          <img
            src={imgSrc}
            alt={image.caption || image.orig_filename}
            className="code-markdocx-lightbox-img"
            onClick={(e) => e.stopPropagation()}
          />
        </div>
      )}
    </div>
  );
};

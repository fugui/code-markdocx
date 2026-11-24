import React, { useMemo, useState } from 'react';
import { Check, Code2, Copy, Eye, ListTree } from 'lucide-react';
import { extractHeadings } from '../utils/markdocxUtils';
import { MarkdownRenderer } from './MarkdownRenderer';

export interface MarkdownPreviewProps {
  markdown: string;
  assetUrlMap?: Record<string, string>;
  keptOriginalCount?: number;
}

export const MarkdownPreview: React.FC<MarkdownPreviewProps> = ({
  markdown,
  assetUrlMap,
  keptOriginalCount = 0,
}) => {
  const [copied, setCopied] = useState(false);
  const [viewMode, setViewMode] = useState<'rendered' | 'source'>('rendered');
  const [showToc, setShowToc] = useState(true);

  const headings = useMemo(() => extractHeadings(markdown), [markdown]);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(markdown);
      setCopied(true);
      setTimeout(() => setCopied(false), 1800);
    } catch {
      // ignore
    }
  };

  const handleJumpHeading = (id: string) => {
    const el = document.getElementById(id);
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }
  };

  return (
    <div className="code-markdocx-card">
      <div className="code-markdocx-card-header">
        <div>
          <h3 className="code-markdocx-section-title">完整 Markdown 预览</h3>
          <p className="code-markdocx-muted">
            {keptOriginalCount > 0
              ? `已实时渲染 Mermaid 与 ${keptOriginalCount} 张引用原图，支持与源码对照及合并打包下载（.zip）。`
              : '已剥离页眉页脚与 Base64 内嵌图像，支持富文本/Mermaid 实时渲染与源码一键复制。'}
          </p>
        </div>

        <div className="code-markdocx-preview-toolbar">
          <div className="code-markdocx-segmented" role="tablist" aria-label="Markdown 视图模式">
            <button
              type="button"
              role="tab"
              aria-selected={viewMode === 'rendered'}
              className={`code-markdocx-segmented-btn ${viewMode === 'rendered' ? 'is-active' : ''}`}
              onClick={() => setViewMode('rendered')}
            >
              <Eye size={14} />
              <span>渲染预览</span>
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={viewMode === 'source'}
              className={`code-markdocx-segmented-btn ${viewMode === 'source' ? 'is-active' : ''}`}
              onClick={() => setViewMode('source')}
            >
              <Code2 size={14} />
              <span>Markdown 源码</span>
            </button>
          </div>

          {viewMode === 'rendered' && headings.length > 0 && (
            <button
              type="button"
              className={`code-markdocx-btn is-sm ${showToc ? 'is-primary' : 'is-secondary'}`}
              onClick={() => setShowToc((v) => !v)}
            >
              <ListTree size={14} />
              <span>目录大纲 ({headings.length})</span>
            </button>
          )}

          <button
            type="button"
            className="code-markdocx-btn is-secondary is-sm"
            onClick={handleCopy}
          >
            {copied ? <Check size={14} /> : <Copy size={14} />}
            <span>{copied ? '已复制' : '复制全文'}</span>
          </button>
        </div>
      </div>

      {viewMode === 'source' ? (
        <pre className="code-markdocx-md-pre">
          <code>{markdown}</code>
        </pre>
      ) : (
        <div
          className={`code-markdocx-preview-layout ${
            showToc && headings.length > 0 ? 'has-toc' : ''
          }`}
        >
          <div className="code-markdocx-preview-body">
            <MarkdownRenderer markdown={markdown} assetUrlMap={assetUrlMap} />
          </div>

          {showToc && headings.length > 0 && (
            <aside className="code-markdocx-toc" aria-label="文档目录大纲">
              <div className="code-markdocx-toc-title">文档目录</div>
              <nav className="code-markdocx-toc-list">
                {headings.map((h) => (
                  <button
                    key={h.id}
                    type="button"
                    className={`code-markdocx-toc-item is-lvl-${Math.min(4, h.level)}`}
                    onClick={() => handleJumpHeading(h.id)}
                    title={h.text}
                  >
                    {h.text}
                  </button>
                ))}
              </nav>
            </aside>
          )}
        </div>
      )}
    </div>
  );
};

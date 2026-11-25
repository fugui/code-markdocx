import React, { useEffect, useMemo, useRef, useState } from 'react';
import { ArrowUp, Code2, Eye, ListTree, Loader2, RotateCcw, Wand2 } from 'lucide-react';
import { CopyButton } from '@code/common';
import type { FormatStats } from '../types/api';
import {
  computeScrollTargetTop,
  extractHeadings,
  findActiveHeadingId,
  formatAutoPolishSummary,
  type HeadingOffset,
} from '../utils/markdocxUtils';
import { MarkdownRenderer } from './MarkdownRenderer';

export interface MarkdownPreviewProps {
  markdown: string;
  assetUrlMap?: Record<string, string>;
  keptOriginalCount?: number;
  formatStats?: FormatStats;
  vlmConfigured?: boolean;
  jobRunning?: boolean;
  layoutPolished?: boolean;
  layoutPolishAppliedOps?: number;
  layoutPolishSummary?: string;
  layoutPolishing?: boolean;
  onPolishLayout?: () => void;
  onRevertLayoutPolish?: () => void;
}

export const MarkdownPreview: React.FC<MarkdownPreviewProps> = ({
  markdown,
  assetUrlMap,
  keptOriginalCount = 0,
  formatStats,
  vlmConfigured = false,
  jobRunning = false,
  layoutPolished = false,
  layoutPolishAppliedOps = 0,
  layoutPolishSummary,
  layoutPolishing = false,
  onPolishLayout,
  onRevertLayoutPolish,
}) => {
  const [viewMode, setViewMode] = useState<'rendered' | 'source'>('rendered');
  const [showToc, setShowToc] = useState(true);
  const [activeHeadingId, setActiveHeadingId] = useState<string | null>(null);
  const [showBackToTop, setShowBackToTop] = useState(false);

  const previewBodyRef = useRef<HTMLDivElement | null>(null);
  const tocListRef = useRef<HTMLElement | null>(null);

  const headings = useMemo(() => extractHeadings(markdown), [markdown]);
  const autoPolishSummary = useMemo(
    () => formatAutoPolishSummary(formatStats),
    [formatStats],
  );

  const handleJumpHeading = (id: string) => {
    setActiveHeadingId(id);
    const el = document.getElementById(id);
    if (!el) {
      return;
    }
    const container = previewBodyRef.current;
    if (
      container &&
      container.contains(el) &&
      container.scrollHeight > container.clientHeight + 2
    ) {
      const targetTop = computeScrollTargetTop(
        container.scrollTop,
        el.getBoundingClientRect().top,
        container.getBoundingClientRect().top,
        16,
      );
      container.scrollTo({ top: targetTop, behavior: 'smooth' });
      return;
    }
    el.scrollIntoView({ behavior: 'smooth', block: 'start' });
  };

  const handleScrollTop = () => {
    previewBodyRef.current?.scrollTo({ top: 0, behavior: 'smooth' });
  };

  useEffect(() => {
    if (viewMode !== 'rendered' || headings.length === 0) {
      setShowBackToTop(false);
      if (headings.length === 0) {
        setActiveHeadingId(null);
      }
      return;
    }

    const container = previewBodyRef.current;
    if (!container) {
      setActiveHeadingId(headings[0].id);
      return;
    }

    let rafId = 0;

    const syncScrollState = () => {
      rafId = 0;
      setShowBackToTop(container.scrollTop > 160);

      const hasScroll = container.scrollHeight > container.clientHeight + 2;
      if (
        hasScroll &&
        container.scrollTop + container.clientHeight >= container.scrollHeight - 4
      ) {
        setActiveHeadingId(headings[headings.length - 1].id);
        return;
      }

      const containerTop = container.getBoundingClientRect().top;
      const offsets: HeadingOffset[] = [];
      for (const h of headings) {
        const el = document.getElementById(h.id);
        if (el && container.contains(el)) {
          offsets.push({
            id: h.id,
            top: el.getBoundingClientRect().top - containerTop,
          });
        }
      }
      setActiveHeadingId(findActiveHeadingId(offsets, 36) ?? headings[0].id);
    };

    const handleScroll = () => {
      if (rafId !== 0) {
        return;
      }
      rafId = window.requestAnimationFrame(syncScrollState);
    };

    syncScrollState();
    container.addEventListener('scroll', handleScroll, { passive: true });
    window.addEventListener('resize', handleScroll);

    return () => {
      if (rafId !== 0) {
        window.cancelAnimationFrame(rafId);
      }
      container.removeEventListener('scroll', handleScroll);
      window.removeEventListener('resize', handleScroll);
    };
  }, [viewMode, headings]);

  useEffect(() => {
    if (viewMode !== 'rendered' || !showToc || !activeHeadingId) {
      return;
    }
    const tocList = tocListRef.current;
    if (!tocList) {
      return;
    }
    const activeBtn = tocList.querySelector<HTMLElement>('[data-toc-active="true"]');
    if (!activeBtn) {
      return;
    }
    const listRect = tocList.getBoundingClientRect();
    const btnRect = activeBtn.getBoundingClientRect();
    if (btnRect.top < listRect.top) {
      tocList.scrollTo({
        top: Math.max(0, tocList.scrollTop + (btnRect.top - listRect.top) - 8),
        behavior: 'smooth',
      });
    } else if (btnRect.bottom > listRect.bottom) {
      tocList.scrollTo({
        top: tocList.scrollTop + (btnRect.bottom - listRect.bottom) + 8,
        behavior: 'smooth',
      });
    }
  }, [activeHeadingId, showToc, viewMode]);

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
          {autoPolishSummary && (
            <p className="code-markdocx-format-summary">{autoPolishSummary}</p>
          )}
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

          {layoutPolished && (
            <span
              className="code-markdocx-pill is-success"
              title={layoutPolishSummary || '已应用 AI 结构化排版优化'}
            >
              AI 排版已优化 ({layoutPolishAppliedOps} 处)
            </span>
          )}

          {onPolishLayout && (
            <button
              type="button"
              className="code-markdocx-btn is-secondary is-sm"
              disabled={layoutPolishing || jobRunning || !vlmConfigured}
              onClick={onPolishLayout}
              title={
                !vlmConfigured
                  ? '服务端 config.yaml 未配置有效 VLM 模型'
                  : jobRunning
                    ? '请等待后台转译完成'
                    : '仅调整标题、列表、引用与错误断行结构，绝不改写正文文字'
              }
            >
              {layoutPolishing ? (
                <Loader2 size={14} className="code-markdocx-spin" />
              ) : (
                <Wand2 size={14} />
              )}
              <span>
                {layoutPolishing
                  ? 'AI 排版优化中...'
                  : layoutPolished
                    ? '再次 AI 排版'
                    : 'AI 排版优化'}
              </span>
            </button>
          )}

          {layoutPolished && onRevertLayoutPolish && (
            <button
              type="button"
              className="code-markdocx-btn is-secondary is-sm"
              disabled={layoutPolishing || jobRunning}
              onClick={onRevertLayoutPolish}
              title="撤销 AI 结构化排版优化，恢复至规则规整后的版本"
            >
              <RotateCcw size={14} />
              <span>撤销 AI 排版</span>
            </button>
          )}

          <CopyButton
            text={markdown}
            label="复制全文"
            copiedLabel="已复制"
            variant="secondary"
            size="sm"
          />
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
          <div className="code-markdocx-preview-body" ref={previewBodyRef}>
            <MarkdownRenderer markdown={markdown} assetUrlMap={assetUrlMap} />
          </div>

          {showToc && headings.length > 0 && (
            <aside className="code-markdocx-toc" aria-label="文档目录大纲">
              <div className="code-markdocx-toc-header">
                <div className="code-markdocx-toc-title">文档目录</div>
                {showBackToTop && (
                  <button
                    type="button"
                    className="code-markdocx-toc-top-btn"
                    onClick={handleScrollTop}
                    title="回到文档顶部"
                  >
                    <ArrowUp size={12} />
                    <span>顶部</span>
                  </button>
                )}
              </div>
              <nav className="code-markdocx-toc-list" ref={tocListRef}>
                {headings.map((h) => {
                  const isActive = activeHeadingId === h.id;
                  return (
                    <button
                      key={h.id}
                      type="button"
                      className={`code-markdocx-toc-item is-lvl-${Math.min(4, h.level)} ${
                        isActive ? 'is-active' : ''
                      }`}
                      aria-current={isActive ? 'location' : undefined}
                      data-toc-active={isActive ? 'true' : undefined}
                      onClick={() => handleJumpHeading(h.id)}
                      title={h.text}
                    >
                      {h.text}
                    </button>
                  );
                })}
              </nav>
            </aside>
          )}
        </div>
      )}
    </div>
  );
};

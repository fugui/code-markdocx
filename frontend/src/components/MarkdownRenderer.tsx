import React, { useMemo, useState } from 'react';
import mermaid from 'mermaid';
import { Check, Copy, X } from 'lucide-react';
import { MermaidDiagram } from '@code/common/mermaid';
import {
  resolveAssetImageUrl,
  slugifyHeading,
  stripInlineMarkdownForHeading,
  stripMarkdownTitle,
  tokenizeInlineMarkdown,
  type InlineToken,
} from '../utils/markdocxUtils';

export interface MarkdownRendererProps {
  markdown: string;
  assetUrlMap?: Record<string, string>;
}

const ALERT_META: Record<string, { label: string; tone: string }> = {
  NOTE: { label: '注意', tone: 'note' },
  TIP: { label: '提示', tone: 'tip' },
  WARNING: { label: '警告', tone: 'warning' },
  IMPORTANT: { label: '重要', tone: 'important' },
  CAUTION: { label: '危险', tone: 'caution' },
};

interface CodeBlockProps {
  lang: string;
  code: string;
}

const CodeBlock: React.FC<CodeBlockProps> = ({ lang, code }) => {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      // ignore clipboard errors
    }
  };

  return (
    <div className="code-markdocx-prose-codeblock">
      <div className="code-markdocx-prose-codebar">
        <span>{lang || 'text'}</span>
        <button
          type="button"
          className="code-markdocx-prose-copybtn"
          onClick={() => void handleCopy()}
        >
          {copied ? <Check size={13} /> : <Copy size={13} />}
          <span>{copied ? '已复制' : '复制'}</span>
        </button>
      </div>
      <pre className="code-markdocx-prose-pre">
        <code>{code}</code>
      </pre>
    </div>
  );
};

function renderTokens(
  tokens: InlineToken[],
  assetUrlMap: Record<string, string>,
  onZoomImage: (src: string, alt: string) => void,
  keyPrefix: string,
): React.ReactNode[] {
  return tokens.map((token, idx) => {
    const key = `${keyPrefix}-${idx}`;
    switch (token.type) {
      case 'text':
        return <React.Fragment key={key}>{token.value}</React.Fragment>;
      case 'code':
        return (
          <code key={key} className="code-markdocx-inline-code">
            {token.value}
          </code>
        );
      case 'image': {
        const resolved = resolveAssetImageUrl(token.url, assetUrlMap);
        return (
          <img
            key={key}
            src={resolved}
            alt={token.alt}
            title={token.alt || '点击放大查看'}
            className="code-markdocx-prose-inline-img"
            onClick={() => onZoomImage(resolved, token.alt)}
          />
        );
      }
      case 'link': {
        const isExternal = /^https?:\/\//i.test(token.url);
        return (
          <a
            key={key}
            href={token.url}
            target={isExternal ? '_blank' : undefined}
            rel={isExternal ? 'noopener noreferrer' : undefined}
            className="code-markdocx-prose-link"
          >
            {renderTokens(token.children, assetUrlMap, onZoomImage, `${key}-lnk`)}
          </a>
        );
      }
      case 'autolink': {
        const isExternal = /^https?:\/\//i.test(token.url);
        return (
          <a
            key={key}
            href={token.url}
            target={isExternal ? '_blank' : undefined}
            rel={isExternal ? 'noopener noreferrer' : undefined}
            className="code-markdocx-prose-link"
          >
            {token.url}
          </a>
        );
      }
      case 'bold':
        return (
          <strong key={key}>
            {renderTokens(token.children, assetUrlMap, onZoomImage, `${key}-b`)}
          </strong>
        );
      case 'strike':
        return (
          <del key={key}>
            {renderTokens(token.children, assetUrlMap, onZoomImage, `${key}-del`)}
          </del>
        );
      case 'italic':
        return (
          <em key={key}>
            {renderTokens(token.children, assetUrlMap, onZoomImage, `${key}-em`)}
          </em>
        );
    }
  });
}

function renderInlineNodes(
  text: string,
  assetUrlMap: Record<string, string>,
  onZoomImage: (src: string, alt: string) => void,
  keyPrefix: string,
): React.ReactNode[] {
  if (!text) {
    return [];
  }
  return renderTokens(tokenizeInlineMarkdown(text), assetUrlMap, onZoomImage, keyPrefix);
}

function splitTableRow(rowLine: string): string[] {
  const trimmed = rowLine.trim();
  const withoutEdges = trimmed.replace(/^\|/, '').replace(/\|$/, '');
  return withoutEdges.split('|').map((cell) => cell.trim());
}

function parseAlignments(sepLine: string, colCount: number): Array<'left' | 'center' | 'right'> {
  const cells = splitTableRow(sepLine);
  const out: Array<'left' | 'center' | 'right'> = [];
  for (let i = 0; i < colCount; i++) {
    const raw = (cells[i] || '').trim();
    if (raw.startsWith(':') && raw.endsWith(':')) {
      out.push('center');
    } else if (raw.endsWith(':')) {
      out.push('right');
    } else {
      out.push('left');
    }
  }
  return out;
}

interface ParsedListItem {
  indent: number;
  ordered: boolean;
  text: string;
}

function parseListLine(line: string): ParsedListItem | null {
  const match = line.match(/^(\s*)([-*+]|\d+\.)\s+(.*)$/);
  if (!match) {
    return null;
  }
  return {
    indent: match[1].replace(/\t/g, '  ').length,
    ordered: /^\d+\./.test(match[2]),
    text: match[3],
  };
}

export const MarkdownRenderer: React.FC<MarkdownRendererProps> = ({
  markdown,
  assetUrlMap = {},
}) => {
  const [lightbox, setLightbox] = useState<{ src: string; alt: string } | null>(null);

  const renderedBlocks = useMemo(() => {
    if (!markdown.trim()) {
      return (
        <p className="code-markdocx-muted">暂无 Markdown 内容可预览。</p>
      );
    }

    const lines = markdown.split(/\r\n?|\n/);
    const elements: React.ReactNode[] = [];
    const seenHeadingIds = new Map<string, number>();
    let i = 0;
    let codeBlockCounter = 0;

    const openZoom = (src: string, alt: string) => setLightbox({ src, alt });

    while (i < lines.length) {
      const line = lines[i];
      const trimmed = line.trim();

      // Fenced code blocks (``` or ~~~, supporting variable-length fences)
      const fenceMatch = trimmed.match(/^(`{3,}|~{3,})(.*)$/);
      if (fenceMatch && !(fenceMatch[1][0] === '`' && fenceMatch[2].includes('`'))) {
        const fenceChar = fenceMatch[1][0];
        const fenceLen = fenceMatch[1].length;
        const lang = fenceMatch[2].trim();
        const codeLines: string[] = [];
        i++;
        while (i < lines.length) {
          const closeTrim = lines[i].trim();
          if (
            closeTrim.length >= fenceLen &&
            [...closeTrim].every((ch) => ch === fenceChar)
          ) {
            break;
          }
          codeLines.push(lines[i]);
          i++;
        }
        if (i < lines.length) {
          i++;
        }
        const codeText = codeLines.join('\n');
        const blockIdx = codeBlockCounter++;

        if (lang.toLowerCase() === 'mermaid') {
          elements.push(
            <div key={`mermaid-${i}-${blockIdx}`} className="code-markdocx-prose-mermaid">
              <MermaidDiagram
                chart={codeText}
                mermaid={mermaid}
                inlineMaxHeight={480}
                exportFilenamePrefix={`diagram-${blockIdx + 1}`}
              />
            </div>,
          );
          continue;
        }

        elements.push(
          <CodeBlock key={`code-${i}-${blockIdx}`} lang={lang} code={codeText} />,
        );
        continue;
      }

      // Headings (#..######)
      const headingMatch = line.match(/^(#{1,6})\s+(.+?)\s*#*$/);
      if (headingMatch) {
        const level = headingMatch[1].length;
        const rawText = headingMatch[2];
        const plainText = stripInlineMarkdownForHeading(rawText);
        const baseId = slugifyHeading(plainText || rawText);
        const count = (seenHeadingIds.get(baseId) ?? 0) + 1;
        seenHeadingIds.set(baseId, count);
        const headingId = count === 1 ? baseId : `${baseId}-${count}`;

        const Tag = `h${level}` as 'h1' | 'h2' | 'h3' | 'h4' | 'h5' | 'h6';
        elements.push(
          <Tag
            key={`h-${i}`}
            id={headingId}
            className={`code-markdocx-prose-h is-h${level}`}
          >
            {renderInlineNodes(rawText, assetUrlMap, openZoom, `h-${i}`)}
          </Tag>,
        );
        i++;
        continue;
      }

      // Horizontal rule
      if (/^(---|[*]{3,}|_{3,})$/.test(trimmed)) {
        elements.push(<hr key={`hr-${i}`} className="code-markdocx-prose-hr" />);
        i++;
        continue;
      }

      // Standalone image figure: ![alt](url)
      const standaloneImg = trimmed.match(/^!\[([^\]]*)\]\(([^)]+)\)$/);
      if (standaloneImg) {
        const alt = standaloneImg[1].replace(/\\\[/g, '[').replace(/\\\]/g, ']');
        const resolved = resolveAssetImageUrl(stripMarkdownTitle(standaloneImg[2]), assetUrlMap);
        elements.push(
          <figure key={`fig-${i}`} className="code-markdocx-prose-figure">
            <img
              src={resolved}
              alt={alt}
              className="code-markdocx-prose-img"
              onClick={() => openZoom(resolved, alt)}
            />
            {alt && <figcaption className="code-markdocx-prose-figcaption">{alt}</figcaption>}
          </figure>,
        );
        i++;
        continue;
      }

      // Blockquote / GitHub Alert
      if (trimmed.startsWith('>')) {
        const quoteLines: string[] = [];
        while (i < lines.length && lines[i].trim().startsWith('>')) {
          quoteLines.push(lines[i].replace(/^\s*>\s?/, ''));
          i++;
        }
        const joined = quoteLines.join('\n');
        const alertMatch = joined.match(/^\[!(NOTE|TIP|WARNING|IMPORTANT|CAUTION)\]\s*([\s\S]*)$/i);
        if (alertMatch) {
          const typeKey = alertMatch[1].toUpperCase();
          const meta = ALERT_META[typeKey] || ALERT_META.NOTE;
          const bodyLines = alertMatch[2].split('\n').filter((l, idx) => idx > 0 || l.trim().length > 0);
          elements.push(
            <div key={`alert-${i}`} className={`code-markdocx-md-alert is-${meta.tone}`}>
              <div className="code-markdocx-md-alert-title">{meta.label}</div>
              <div className="code-markdocx-md-alert-body">
                {bodyLines.map((bLine, bIdx) =>
                  bLine.trim() ? (
                    <p key={bIdx}>
                      {renderInlineNodes(bLine, assetUrlMap, openZoom, `al-${i}-${bIdx}`)}
                    </p>
                  ) : (
                    <div key={bIdx} className="code-markdocx-prose-spacer" />
                  ),
                )}
              </div>
            </div>,
          );
        } else {
          elements.push(
            <blockquote key={`bq-${i}`} className="code-markdocx-prose-quote">
              {quoteLines.map((qLine, qIdx) =>
                qLine.trim() ? (
                  <p key={qIdx}>
                    {renderInlineNodes(qLine, assetUrlMap, openZoom, `bq-${i}-${qIdx}`)}
                  </p>
                ) : (
                  <div key={qIdx} className="code-markdocx-prose-spacer" />
                ),
              )}
            </blockquote>,
          );
        }
        continue;
      }

      // GFM Table
      if (trimmed.startsWith('|') && trimmed.endsWith('|') && i + 1 < lines.length) {
        const nextTrimmed = lines[i + 1].trim();
        const isSep =
          nextTrimmed.startsWith('|') &&
          nextTrimmed.endsWith('|') &&
          splitTableRow(nextTrimmed).every((cell) => /^:?-+:?$/.test(cell));

        if (isSep) {
          const tableLines: string[] = [];
          while (
            i < lines.length &&
            lines[i].trim().startsWith('|') &&
            lines[i].trim().endsWith('|')
          ) {
            tableLines.push(lines[i].trim());
            i++;
          }
          const headers = splitTableRow(tableLines[0]);
          const aligns = parseAlignments(tableLines[1], headers.length);
          const bodyRows = tableLines.slice(2).map(splitTableRow);

          elements.push(
            <div key={`tbl-${i}`} className="code-markdocx-prose-table-wrap">
              <table className="code-markdocx-prose-table">
                <thead>
                  <tr>
                    {headers.map((col, idx) => (
                      <th key={idx} style={{ textAlign: aligns[idx] || 'left' }}>
                        {renderInlineNodes(col, assetUrlMap, openZoom, `th-${i}-${idx}`)}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {bodyRows.map((row, rIdx) => (
                    <tr key={rIdx}>
                      {headers.map((_, cIdx) => (
                        <td key={cIdx} style={{ textAlign: aligns[cIdx] || 'left' }}>
                          {renderInlineNodes(
                            row[cIdx] ?? '',
                            assetUrlMap,
                            openZoom,
                            `td-${i}-${rIdx}-${cIdx}`,
                          )}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>,
          );
          continue;
        }
      }

      // Lists (unordered & ordered)
      const firstList = parseListLine(line);
      if (firstList) {
        const items: ParsedListItem[] = [];
        while (i < lines.length) {
          const parsed = parseListLine(lines[i]);
          if (!parsed) {
            break;
          }
          items.push(parsed);
          i++;
        }
        const ListTag = firstList.ordered ? 'ol' : 'ul';
        const minIndent = Math.min(...items.map((it) => it.indent));
        elements.push(
          <ListTag key={`list-${i}`} className="code-markdocx-prose-list">
            {items.map((item, idx) => {
              const nestLevel = Math.min(4, Math.floor((item.indent - minIndent) / 2));
              return (
                <li
                  key={idx}
                  style={nestLevel > 0 ? { marginLeft: `${nestLevel * 1.25}rem` } : undefined}
                >
                  {renderInlineNodes(item.text, assetUrlMap, openZoom, `li-${i}-${idx}`)}
                </li>
              );
            })}
          </ListTag>,
        );
        continue;
      }

      // Regular paragraph (merge consecutive non-blank plain lines)
      if (trimmed !== '') {
        const paraLines: string[] = [line];
        i++;
        while (i < lines.length) {
          const next = lines[i];
          const nextTrim = next.trim();
          if (
            nextTrim === '' ||
            nextTrim.startsWith('```') ||
            nextTrim.startsWith('~~~') ||
            nextTrim.startsWith('>') ||
            /^(#{1,6})\s+/.test(next) ||
            /^(---|[*]{3,}|_{3,})$/.test(nextTrim) ||
            /^!\[([^\]]*)\]\(([^)]+)\)$/.test(nextTrim) ||
            parseListLine(next) !== null ||
            (nextTrim.startsWith('|') && nextTrim.endsWith('|'))
          ) {
            break;
          }
          paraLines.push(next);
          i++;
        }
        elements.push(
          <p key={`p-${i}`} className="code-markdocx-prose-p">
            {paraLines.map((pLine, pIdx) => (
              <React.Fragment key={pIdx}>
                {pIdx > 0 && <br />}
                {renderInlineNodes(pLine, assetUrlMap, openZoom, `p-${i}-${pIdx}`)}
              </React.Fragment>
            ))}
          </p>,
        );
        continue;
      }

      i++;
    }

    return elements;
  }, [markdown, assetUrlMap]);

  return (
    <div className="code-markdocx-prose">
      {renderedBlocks}

      {lightbox && (
        <div className="code-markdocx-lightbox" onClick={() => setLightbox(null)}>
          <button
            type="button"
            className="code-markdocx-lightbox-close"
            onClick={() => setLightbox(null)}
            aria-label="关闭大图预览"
          >
            <X size={20} />
          </button>
          <img
            src={lightbox.src}
            alt={lightbox.alt}
            className="code-markdocx-lightbox-img"
            onClick={(e) => e.stopPropagation()}
          />
        </div>
      )}
    </div>
  );
};

import type { FormatStats, ImageState, PreflightReport } from '../types/api';

export type ReviewFilter = 'all' | 'attention' | 'mermaid' | 'original' | 'skipped';

export function formatAutoPolishSummary(stats?: FormatStats): string {
  if (!stats) {
    return '';
  }
  const parts: string[] = [];
  if (stats.promoted_headings > 0) {
    parts.push(`标题 +${stats.promoted_headings}`);
  }
  if (stats.normalized_lists > 0) {
    parts.push(`列表 +${stats.normalized_lists}`);
  }
  if (stats.converted_alerts > 0) {
    parts.push(`提示框 +${stats.converted_alerts}`);
  }
  if (stats.cleaned_inline > 0) {
    parts.push(`行内格式 +${stats.cleaned_inline}`);
  }
  if (stats.trimmed_tables > 0) {
    parts.push(`表格规整 +${stats.trimmed_tables}`);
  }
  if (parts.length === 0) {
    return '';
  }
  return `规则自动规整：${parts.join(' · ')}`;
}

export interface PreflightIssue {
  id: string;
  severity: 'error' | 'warn';
  kind: 'failed' | 'syntax' | 'base64' | 'vector' | 'low_confidence' | 'size';
  imageId?: string;
  title: string;
  detail: string;
}

export interface TocHeading {
  id: string;
  text: string;
  level: number;
}

export interface HeadingOffset {
  id: string;
  top: number;
}

export function findActiveHeadingId(
  offsets: readonly HeadingOffset[],
  threshold = 36,
): string | null {
  if (offsets.length === 0) {
    return null;
  }
  let activeId = offsets[0].id;
  for (const offset of offsets) {
    if (offset.top <= threshold) {
      activeId = offset.id;
    }
  }
  return activeId;
}

export function computeScrollTargetTop(
  currentScrollTop: number,
  elementTop: number,
  containerTop: number,
  topPadding = 16,
): number {
  return Math.max(0, Math.round(currentScrollTop + (elementTop - containerTop) - topPadding));
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
}

export function needsAttention(img: ImageState): boolean {
  if (img.skipped || img.status === 'skipped' || img.keep_original) {
    return false;
  }
  return (
    img.status === 'failed' ||
    img.status === 'unsupported_vector' ||
    img.confidence === 'low'
  );
}

export function isUnconvertedWithImage(img: ImageState): boolean {
  if (!img.has_image_file || !img.asset_filename || img.unsupported_vector || img.keep_original) {
    return false;
  }
  if (img.status === 'pending' || img.status === 'processing') {
    return false;
  }
  if (img.status === 'failed' || img.skipped || img.status === 'skipped') {
    return true;
  }
  const hasMermaid = Boolean(img.mermaid_code && img.mermaid_code.trim());
  const hasTable =
    img.category === 'table_screenshot' && Boolean(img.markdown_table && img.markdown_table.trim());
  const hasCode =
    img.category === 'code_screenshot' && Boolean(img.code_content && img.code_content.trim());
  return !hasMermaid && !hasTable && !hasCode;
}

export function filterImages(
  images: ImageState[],
  filter: ReviewFilter,
  syntaxErrors: Record<string, string | undefined>,
): ImageState[] {
  return images.filter((img) => {
    const isSkipped = img.skipped || img.status === 'skipped';
    const hasSyntaxErr =
      !isSkipped && !img.keep_original && Boolean(syntaxErrors[img.image_id]);
    switch (filter) {
      case 'attention':
        return needsAttention(img) || hasSyntaxErr;
      case 'mermaid':
        return (
          !isSkipped &&
          !img.keep_original &&
          Boolean(img.mermaid_code && img.mermaid_code.trim())
        );
      case 'original':
        return !isSkipped && Boolean(img.keep_original);
      case 'skipped':
        return isSkipped;
      default:
        return true;
    }
  });
}

export function countSyntaxErrors(
  images: ImageState[],
  syntaxErrors: Record<string, string | undefined>,
): number {
  let count = 0;
  for (const img of images) {
    if (
      !img.skipped &&
      img.status !== 'skipped' &&
      !img.keep_original &&
      syntaxErrors[img.image_id]
    ) {
      count++;
    }
  }
  return count;
}

export function getPreflightStatusLevel(
  preflight: PreflightReport,
  syntaxErrorCount: number,
): 'ok' | 'warn' | 'error' {
  if (preflight.has_inline_base64 || preflight.image_stats.failed > 0 || syntaxErrorCount > 0) {
    return 'error';
  }
  if (
    preflight.exceeds_size_limit ||
    preflight.secret_warnings.length > 0 ||
    preflight.image_stats.low_confidence > 0 ||
    preflight.image_stats.unsupported_vector > 0
  ) {
    return 'warn';
  }
  return 'ok';
}

export function collectPreflightIssues(
  images: ImageState[],
  preflight: PreflightReport,
  syntaxErrors: Record<string, string | undefined>,
): PreflightIssue[] {
  const issues: PreflightIssue[] = [];

  for (const img of images) {
    const isSkipped = img.skipped || img.status === 'skipped';
    if (isSkipped) {
      continue;
    }
    const label = img.caption || img.orig_filename || img.image_id;
    const section = img.section_path ? `（${img.section_path}）` : '';

    if (img.status === 'failed') {
      issues.push({
        id: `failed-${img.image_id}`,
        severity: 'error',
        kind: 'failed',
        imageId: img.image_id,
        title: `${img.image_id} 转译失败${section}`,
        detail: `${label}：${img.error || 'VLM 转译失败，可重试、跳过或改为引用原图'}`,
      });
      continue;
    }

    const syntaxErr =
      !img.keep_original &&
      img.category !== 'table_screenshot' &&
      img.category !== 'code_screenshot'
        ? syntaxErrors[img.image_id]
        : undefined;
    if (syntaxErr) {
      issues.push({
        id: `syntax-${img.image_id}`,
        severity: 'error',
        kind: 'syntax',
        imageId: img.image_id,
        title: `${img.image_id} Mermaid 语法错误${section}`,
        detail: `${label}：${syntaxErr}`,
      });
    }
  }

  if (preflight.has_inline_base64) {
    issues.push({
      id: 'inline-base64',
      severity: 'error',
      kind: 'base64',
      title: '正文中检测到 Base64 内嵌图像',
      detail: 'code-archdoc 不允许内嵌 data:image/...;base64 数据，请检查并清理。',
    });
  }

  for (const img of images) {
    const isSkipped = img.skipped || img.status === 'skipped';
    if (isSkipped) {
      continue;
    }
    const label = img.caption || img.orig_filename || img.image_id;
    const section = img.section_path ? `（${img.section_path}）` : '';

    if (img.unsupported_vector || img.status === 'unsupported_vector') {
      issues.push({
        id: `vector-${img.image_id}`,
        severity: 'warn',
        kind: 'vector',
        imageId: img.image_id,
        title: `${img.image_id} 矢量图未自动转换${section}`,
        detail: `${label}：${img.vector_warning || '需要人工替换为 PNG 或补充 Mermaid'}`,
      });
    } else if (!img.keep_original && img.status === 'done' && img.confidence === 'low') {
      issues.push({
        id: `lowconf-${img.image_id}`,
        severity: 'warn',
        kind: 'low_confidence',
        imageId: img.image_id,
        title: `${img.image_id} 识别置信度较低${section}`,
        detail: `${label}：建议对照原图核对节点/连线，或切换为引用原图。`,
      });
    }
  }

  if (preflight.exceeds_size_limit) {
    issues.push({
      id: 'size-limit',
      severity: 'warn',
      kind: 'size',
      title: '文档体积超出单篇上限',
      detail: `当前 ${formatBytes(preflight.byte_size)} 超过 ${formatBytes(preflight.max_bytes)}，可使用右侧「按 H2 拆分导出 (.zip)」。`,
    });
  }

  return issues;
}

export function stripInlineMarkdownForHeading(text: string): string {
  return text
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/`([^`]+)`/g, '$1')
    .replace(/(\*\*|__|\*|_|~~)/g, '')
    .trim();
}

export function slugifyHeading(text: string): string {
  const clean = stripInlineMarkdownForHeading(text)
    .toLowerCase()
    .replace(/[^\w\u4e00-\u9fa5]+/g, '-')
    .replace(/^-+|-+$/g, '');
  return clean || 'section';
}

export function extractHeadings(markdown: string): TocHeading[] {
  if (!markdown) {
    return [];
  }
  const lines = markdown.split(/\r\n?|\n/);
  const headings: TocHeading[] = [];
  const seen = new Map<string, number>();
  let fenceChar: string | null = null;
  let fenceLen = 0;

  for (const line of lines) {
    const trimmed = line.trim();
    const fenceOpen = trimmed.match(/^(`{3,}|~{3,})(.*)$/);
    if (fenceChar === null) {
      if (fenceOpen) {
        const marker = fenceOpen[1];
        const info = fenceOpen[2] || '';
        if (!(marker[0] === '`' && info.includes('`'))) {
          fenceChar = marker[0];
          fenceLen = marker.length;
          continue;
        }
      }
    } else {
      if (
        trimmed.length >= fenceLen &&
        [...trimmed].every((ch) => ch === fenceChar)
      ) {
        fenceChar = null;
        fenceLen = 0;
      }
      continue;
    }
    const match = line.match(/^(#{1,6})\s+(.+?)\s*#*$/);
    if (!match) {
      continue;
    }
    const level = match[1].length;
    const text = stripInlineMarkdownForHeading(match[2]);
    if (!text) {
      continue;
    }
    const baseId = slugifyHeading(text);
    const count = (seen.get(baseId) ?? 0) + 1;
    seen.set(baseId, count);
    const id = count === 1 ? baseId : `${baseId}-${count}`;
    headings.push({ id, text, level });
  }
  return headings;
}

export function resolveAssetImageUrl(
  rawUrl: string,
  assetUrlMap: Record<string, string> = {},
): string {
  const trimmed = rawUrl.trim();
  if (!trimmed) {
    return trimmed;
  }
  const normalized = trimmed.replace(/^\.\//, '');
  return assetUrlMap[trimmed] ?? assetUrlMap[normalized] ?? trimmed;
}

export function stripMarkdownTitle(rawUrl: string): string {
  return rawUrl
    .trim()
    .replace(/\s+"[^"]*"$/, '')
    .replace(/\s+'[^']*'$/, '');
}

export function isSafeHref(rawHref: string): boolean {
  const trimmed = rawHref.trim();
  if (!trimmed) {
    return false;
  }
  if (trimmed.startsWith('#')) {
    return true;
  }
  return /^(https?:\/\/|mailto:)/i.test(trimmed);
}

export type InlineToken =
  | { type: 'text'; value: string }
  | { type: 'code'; value: string }
  | { type: 'image'; alt: string; url: string }
  | { type: 'link'; children: InlineToken[]; url: string }
  | { type: 'autolink'; url: string }
  | { type: 'bold'; children: InlineToken[] }
  | { type: 'italic'; children: InlineToken[] }
  | { type: 'strike'; children: InlineToken[] };

const INLINE_TOKEN_PATTERN =
  '(`[^`]+`|!\\[[^\\]]*\\]\\([^)]+\\)|\\[[^\\]]+\\]\\([^)]+\\)|<(?:https?:\\/\\/|mailto:)[^\\s<>]+>|\\*\\*(?:[^*\\n]|\\*(?!\\*))+?\\*\\*|~~(?:[^~\\n]|~(?!~))+?~~|\\*[^*\\n]+?\\*)';
const MAX_INLINE_DEPTH = 8;

export function tokenizeInlineMarkdown(text: string, depth = 0): InlineToken[] {
  if (!text) {
    return [];
  }
  if (depth >= MAX_INLINE_DEPTH) {
    return [{ type: 'text', value: text }];
  }

  // Fresh local RegExp instance per invocation avoids shared /g lastIndex corruption on recursion.
  const tokenRe = new RegExp(INLINE_TOKEN_PATTERN, 'g');
  const tokens: InlineToken[] = [];
  let lastIndex = 0;

  let match: RegExpExecArray | null;
  while ((match = tokenRe.exec(text)) !== null) {
    if (match.index > lastIndex) {
      tokens.push({ type: 'text', value: text.slice(lastIndex, match.index) });
    }

    const token = match[0];
    const nextLastIndex = tokenRe.lastIndex > match.index ? tokenRe.lastIndex : match.index + 1;

    if (token.startsWith('`') && token.endsWith('`') && token.length >= 2) {
      tokens.push({ type: 'code', value: token.slice(1, -1) });
    } else if (token.startsWith('![')) {
      const m = token.match(/^!\[([^\]]*)\]\(([^)]+)\)$/);
      if (m) {
        const alt = m[1].replace(/\\\[/g, '[').replace(/\\\]/g, ']');
        tokens.push({ type: 'image', alt, url: stripMarkdownTitle(m[2]) });
      } else {
        tokens.push({ type: 'text', value: token });
      }
    } else if (token.startsWith('[')) {
      const m = token.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
      if (m) {
        const href = stripMarkdownTitle(m[2]);
        if (isSafeHref(href)) {
          tokens.push({
            type: 'link',
            url: href,
            children: tokenizeInlineMarkdown(m[1], depth + 1),
          });
        } else {
          tokens.push({ type: 'text', value: token });
        }
      } else {
        tokens.push({ type: 'text', value: token });
      }
    } else if (token.startsWith('<') && token.endsWith('>')) {
      const href = token.slice(1, -1);
      if (isSafeHref(href)) {
        tokens.push({ type: 'autolink', url: href });
      } else {
        tokens.push({ type: 'text', value: token });
      }
    } else if (token.startsWith('**') && token.endsWith('**') && token.length > 4) {
      tokens.push({
        type: 'bold',
        children: tokenizeInlineMarkdown(token.slice(2, -2), depth + 1),
      });
    } else if (token.startsWith('~~') && token.endsWith('~~') && token.length > 4) {
      tokens.push({
        type: 'strike',
        children: tokenizeInlineMarkdown(token.slice(2, -2), depth + 1),
      });
    } else if (token.startsWith('*') && token.endsWith('*') && token.length > 2) {
      tokens.push({
        type: 'italic',
        children: tokenizeInlineMarkdown(token.slice(1, -1), depth + 1),
      });
    } else {
      tokens.push({ type: 'text', value: token });
    }

    lastIndex = nextLastIndex;
    tokenRe.lastIndex = nextLastIndex;
  }

  if (lastIndex < text.length) {
    tokens.push({ type: 'text', value: text.slice(lastIndex) });
  }
  return tokens;
}

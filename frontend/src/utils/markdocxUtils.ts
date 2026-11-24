import type { ImageState, PreflightReport } from '../types/api';

export type ReviewFilter = 'all' | 'attention' | 'mermaid' | 'original' | 'skipped';

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
  return !hasMermaid && !hasTable;
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

    const syntaxErr = !img.keep_original && img.category !== 'table_screenshot'
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

  for (const line of lines) {
    const trimmed = line.trim();
    if (trimmed.startsWith('```') || trimmed.startsWith('~~~')) {
      const marker = trimmed.slice(0, 3);
      if (fenceChar === null) {
        fenceChar = marker;
      } else if (fenceChar === marker) {
        fenceChar = null;
      }
      continue;
    }
    if (fenceChar !== null) {
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

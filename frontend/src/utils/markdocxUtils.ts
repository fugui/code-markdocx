import type { ImageState, PreflightReport } from '../types/api';

export type ReviewFilter = 'all' | 'attention' | 'mermaid' | 'skipped';

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
  if (img.skipped || img.status === 'skipped') {
    return false;
  }
  return (
    img.status === 'failed' ||
    img.status === 'unsupported_vector' ||
    img.confidence === 'low'
  );
}

export function filterImages(
  images: ImageState[],
  filter: ReviewFilter,
  syntaxErrors: Record<string, string | undefined>,
): ImageState[] {
  return images.filter((img) => {
    const hasSyntaxErr = Boolean(syntaxErrors[img.image_id]);
    switch (filter) {
      case 'attention':
        return needsAttention(img) || hasSyntaxErr;
      case 'mermaid':
        return !img.skipped && Boolean(img.mermaid_code && img.mermaid_code.trim());
      case 'skipped':
        return img.skipped || img.status === 'skipped';
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
    if (!img.skipped && img.status !== 'skipped' && syntaxErrors[img.image_id]) {
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

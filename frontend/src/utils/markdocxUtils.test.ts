import assert from 'node:assert/strict';
import test from 'node:test';
import type { ImageState, PreflightReport } from '../types/api.ts';
import {
  collectPreflightIssues,
  computeScrollTargetTop,
  countSyntaxErrors,
  extractHeadings,
  filterImages,
  findActiveHeadingId,
  formatAutoPolishSummary,
  formatBytes,
  getPreflightStatusLevel,
  isUnconvertedWithImage,
  resolveAssetImageUrl,
  tokenizeInlineMarkdown,
} from './markdocxUtils.ts';

function makeImage(overrides: Partial<ImageState> = {}): ImageState {
  return {
    image_id: 'img-001',
    orig_filename: 'a.png',
    section_path: '1. 架构',
    caption: '图1',
    context_before: '',
    context_after: '',
    width: 400,
    height: 300,
    status: 'done',
    skipped: false,
    keep_original: false,
    has_image_file: true,
    asset_filename: 'img-001.png',
    unsupported_vector: false,
    category: 'uml_diagram',
    mermaid_code: 'flowchart TD\n A-->B',
    confidence: 'high',
    ...overrides,
  };
}

function makePreflight(overrides: Partial<PreflightReport> = {}): PreflightReport {
  return {
    byte_size: 1024,
    max_bytes: 524288,
    exceeds_size_limit: false,
    estimated_tokens: 300,
    line_count: 40,
    heading_count: 4,
    mermaid_block_count: 1,
    has_inline_base64: false,
    image_stats: {
      total: 1,
      done: 1,
      pending: 0,
      failed: 0,
      skipped: 0,
      low_confidence: 0,
      unsupported_vector: 0,
      kept_original: 0,
    },
    secret_warnings: [],
    ...overrides,
  };
}

test('formatBytes formats bytes and KB cleanly', () => {
  assert.equal(formatBytes(512), '512 B');
  assert.equal(formatBytes(2048), '2.0 KB');
  assert.equal(formatBytes(524288), '512.0 KB');
});

test('filterImages filters attention, mermaid, original, and skipped', () => {
  const images: ImageState[] = [
    makeImage({ image_id: 'img-001' }),
    makeImage({
      image_id: 'img-002',
      orig_filename: 'b.png',
      section_path: '2. 跳过',
      caption: '',
      width: 40,
      height: 40,
      status: 'skipped',
      skipped: true,
      has_image_file: false,
      asset_filename: undefined,
      mermaid_code: undefined,
    }),
    makeImage({
      image_id: 'img-003',
      orig_filename: 'c.png',
      keep_original: true,
      mermaid_code: 'bad mermaid',
    }),
  ];
  const errs = {
    'img-001': 'Parse error on line 2',
    'img-003': 'Should be ignored when keep_original is true',
  };
  assert.equal(filterImages(images, 'attention', errs).length, 1);
  assert.equal(filterImages(images, 'mermaid', errs).length, 1);
  assert.equal(filterImages(images, 'original', errs).length, 1);
  assert.equal(filterImages(images, 'skipped', errs).length, 1);
  assert.equal(countSyntaxErrors(images, errs), 1);
});

test('getPreflightStatusLevel reflects errors and warnings', () => {
  const base = makePreflight();
  assert.equal(getPreflightStatusLevel(base, 0), 'ok');
  assert.equal(getPreflightStatusLevel({ ...base, exceeds_size_limit: true }, 0), 'warn');
  assert.equal(getPreflightStatusLevel(base, 1), 'error');
});

test('collectPreflightIssues surfaces failed images, syntax errors, base64, and warnings', () => {
  const images: ImageState[] = [
    makeImage({
      image_id: 'img-001',
      status: 'failed',
      error: 'VLM timeout',
      mermaid_code: '',
    }),
    makeImage({
      image_id: 'img-002',
      status: 'done',
      mermaid_code: 'sequenceDiagram\n  bad',
    }),
    makeImage({
      image_id: 'img-003',
      status: 'unsupported_vector',
      unsupported_vector: true,
      vector_warning: 'missing soffice',
      has_image_file: false,
      asset_filename: undefined,
      mermaid_code: '',
    }),
    makeImage({
      image_id: 'img-004',
      status: 'done',
      confidence: 'low',
    }),
  ];
  const preflight = makePreflight({
    has_inline_base64: true,
    exceeds_size_limit: true,
  });
  const issues = collectPreflightIssues(images, preflight, {
    'img-002': 'Syntax error at line 2',
  });
  const errors = issues.filter((i) => i.severity === 'error');
  const warns = issues.filter((i) => i.severity === 'warn');

  assert.equal(errors.length, 3);
  assert.equal(errors[0].imageId, 'img-001');
  assert.match(errors[0].detail, /VLM timeout/);
  assert.equal(errors[1].imageId, 'img-002');
  assert.equal(errors[2].kind, 'base64');

  assert.equal(warns.length, 3);
  assert.equal(isUnconvertedWithImage(images[0]), true);
  assert.equal(isUnconvertedWithImage(images[2]), false);
});

test('extractHeadings ignores fenced code blocks and deduplicates IDs', () => {
  const md = [
    '# 系统设计',
    '```markdown',
    '# 代码块里的标题',
    '```',
    '## 接口规范',
    '~~~txt',
    '## 假标题',
    '~~~',
    '## 接口规范',
    '### **加粗**子标题',
  ].join('\n');

  const headings = extractHeadings(md);
  assert.equal(headings.length, 4);
  assert.deepEqual(
    headings.map((h) => ({ id: h.id, text: h.text, level: h.level })),
    [
      { id: '系统设计', text: '系统设计', level: 1 },
      { id: '接口规范', text: '接口规范', level: 2 },
      { id: '接口规范-2', text: '接口规范', level: 2 },
      { id: '加粗子标题', text: '加粗子标题', level: 3 },
    ],
  );
});

test('extractHeadings supports 4-backtick fences and code_screenshot image status', () => {
  const md = [
    '# 文档',
    '````markdown',
    '```go',
    '# 内嵌假标题',
    '```',
    '````',
    '## 真实二级标题',
  ].join('\n');
  const headings = extractHeadings(md);
  assert.deepEqual(
    headings.map((h) => h.text),
    ['文档', '真实二级标题'],
  );

  const codeShot = makeImage({
    category: 'code_screenshot',
    mermaid_code: '',
    code_language: 'go',
    code_content: 'package main',
  });
  assert.equal(isUnconvertedWithImage(codeShot), false);
  const issues = collectPreflightIssues([codeShot], makePreflight(), {
    [codeShot.image_id]: 'should be ignored for code_screenshot',
  });
  assert.equal(issues.length, 0);
});

test('resolveAssetImageUrl resolves relative assets paths', () => {
  const map = {
    'assets/img-001.png': '/api/jobs/123/images/img-001',
    './assets/img-001.png': '/api/jobs/123/images/img-001',
  };
  assert.equal(resolveAssetImageUrl('assets/img-001.png', map), '/api/jobs/123/images/img-001');
  assert.equal(resolveAssetImageUrl('./assets/img-001.png', map), '/api/jobs/123/images/img-001');
  assert.equal(resolveAssetImageUrl('https://example.com/a.png', map), 'https://example.com/a.png');
});

test('tokenizeInlineMarkdown handles bold, nested formatting, and terminates safely', () => {
  // Directly reproduces the caption pattern (**...**) that triggered the shared RegExp lastIndex loop
  assert.deepEqual(tokenizeInlineMarkdown('**图 1-1 登录时序**'), [
    { type: 'bold', children: [{ type: 'text', value: '图 1-1 登录时序' }] },
  ]);

  assert.deepEqual(
    tokenizeInlineMarkdown('**A** 与 *B* 和 ~~C~~ 以及 `code` <https://example.com>'),
    [
      { type: 'bold', children: [{ type: 'text', value: 'A' }] },
      { type: 'text', value: ' 与 ' },
      { type: 'italic', children: [{ type: 'text', value: 'B' }] },
      { type: 'text', value: ' 和 ' },
      { type: 'strike', children: [{ type: 'text', value: 'C' }] },
      { type: 'text', value: ' 以及 ' },
      { type: 'code', value: 'code' },
      { type: 'text', value: ' ' },
      { type: 'autolink', url: 'https://example.com' },
    ],
  );

  assert.deepEqual(
    tokenizeInlineMarkdown('[**加粗**与*斜体*](https://example.com) ![图注](assets/img-001.png)'),
    [
      {
        type: 'link',
        url: 'https://example.com',
        children: [
          { type: 'bold', children: [{ type: 'text', value: '加粗' }] },
          { type: 'text', value: '与' },
          { type: 'italic', children: [{ type: 'text', value: '斜体' }] },
        ],
      },
      { type: 'text', value: ' ' },
      { type: 'image', alt: '图注', url: 'assets/img-001.png' },
    ],
  );

  assert.deepEqual(tokenizeInlineMarkdown('**外层 *内层* 文本**'), [
    {
      type: 'bold',
      children: [
        { type: 'text', value: '外层 ' },
        { type: 'italic', children: [{ type: 'text', value: '内层' }] },
        { type: 'text', value: ' 文本' },
      ],
    },
  ]);

  // Pathological deeply nested/unbalanced markers must terminate fast within MAX_INLINE_DEPTH
  const pathological = `${'**'.repeat(20)}text${'**'.repeat(20)}`;
  const tokens = tokenizeInlineMarkdown(pathological);
  assert.ok(tokens.length > 0);
});

test('findActiveHeadingId tracks active heading across scroll positions', () => {
  assert.equal(findActiveHeadingId([]), null);

  const offsets = [
    { id: 'overview', top: 20 },
    { id: 'architecture', top: 180 },
    { id: 'api', top: 420 },
  ];
  // Near top: all headings below threshold -> default to first
  assert.equal(
    findActiveHeadingId(
      offsets.map((o) => ({ ...o, top: o.top + 30 })),
      36,
    ),
    'overview',
  );

  // Middle scroll: exact threshold boundary matches
  assert.equal(
    findActiveHeadingId(
      [
        { id: 'overview', top: -140 },
        { id: 'architecture', top: 36 },
        { id: 'api', top: 260 },
      ],
      36,
    ),
    'architecture',
  );

  // Scrolled past all headings -> last heading remains active
  assert.equal(
    findActiveHeadingId(
      [
        { id: 'overview', top: -400 },
        { id: 'architecture', top: -220 },
        { id: 'api', top: -10 },
      ],
      36,
    ),
    'api',
  );
});

test('computeScrollTargetTop computes non-negative scroll offset within container', () => {
  assert.equal(computeScrollTargetTop(120, 480, 100, 16), 484);
  assert.equal(computeScrollTargetTop(10, 110, 100, 16), 4);
  assert.equal(computeScrollTargetTop(0, 105, 100, 16), 0);
});

test('formatAutoPolishSummary formats non-zero rule formatting stats cleanly', () => {
  assert.equal(formatAutoPolishSummary(undefined), '');
  assert.equal(
    formatAutoPolishSummary({
      promoted_headings: 0,
      normalized_lists: 0,
      converted_alerts: 0,
      cleaned_inline: 0,
      trimmed_tables: 0,
    }),
    '',
  );
  assert.equal(
    formatAutoPolishSummary({
      promoted_headings: 3,
      normalized_lists: 2,
      converted_alerts: 1,
      cleaned_inline: 4,
      trimmed_tables: 0,
    }),
    '规则自动规整：标题 +3 · 列表 +2 · 提示框 +1 · 行内格式 +4',
  );
});

import assert from 'node:assert/strict';
import test from 'node:test';
import type { ImageState, PreflightReport } from '../types/api.ts';
import {
  countSyntaxErrors,
  filterImages,
  formatBytes,
  getPreflightStatusLevel,
} from './markdocxUtils.ts';

test('formatBytes formats bytes and KB cleanly', () => {
  assert.equal(formatBytes(512), '512 B');
  assert.equal(formatBytes(2048), '2.0 KB');
  assert.equal(formatBytes(524288), '512.0 KB');
});

test('filterImages filters attention including syntax errors', () => {
  const images: ImageState[] = [
    {
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
      unsupported_vector: false,
      category: 'uml_diagram',
      mermaid_code: 'flowchart TD\n A-->B',
      confidence: 'high',
    },
    {
      image_id: 'img-002',
      orig_filename: 'b.png',
      section_path: '2. 跳过',
      caption: '',
      context_before: '',
      context_after: '',
      width: 40,
      height: 40,
      status: 'skipped',
      skipped: true,
      unsupported_vector: false,
    },
  ];
  const errs = { 'img-001': 'Parse error on line 2' };
  assert.equal(filterImages(images, 'attention', errs).length, 1);
  assert.equal(filterImages(images, 'skipped', errs).length, 1);
  assert.equal(countSyntaxErrors(images, errs), 1);
});

test('getPreflightStatusLevel reflects errors and warnings', () => {
  const base: PreflightReport = {
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
    },
    secret_warnings: [],
  };
  assert.equal(getPreflightStatusLevel(base, 0), 'ok');
  assert.equal(getPreflightStatusLevel({ ...base, exceeds_size_limit: true }, 0), 'warn');
  assert.equal(getPreflightStatusLevel(base, 1), 'error');
});

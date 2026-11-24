import React, { useMemo, useState } from 'react';
import {
  AlertTriangle,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Copy,
  Download,
  FileArchive,
  Image as ImageIcon,
  MapPin,
  ShieldAlert,
} from 'lucide-react';
import { getExportUrl } from '../api/client';
import type { ImageState, PreflightReport } from '../types/api';
import {
  collectPreflightIssues,
  formatBytes,
  getPreflightStatusLevel,
} from '../utils/markdocxUtils';

export interface PreflightBannerProps {
  jobId: string;
  markdown: string;
  preflight: PreflightReport;
  images: ImageState[];
  syntaxErrors: Record<string, string | undefined>;
  syntaxErrorCount: number;
  onLocateImage?: (imageId: string) => void;
}

export const PreflightBanner: React.FC<PreflightBannerProps> = ({
  jobId,
  markdown,
  preflight,
  images,
  syntaxErrors,
  syntaxErrorCount,
  onLocateImage,
}) => {
  const [copied, setCopied] = useState(false);
  const [showSecrets, setShowSecrets] = useState(false);
  const [showErrors, setShowErrors] = useState(true);
  const [showWarns, setShowWarns] = useState(false);

  const level = getPreflightStatusLevel(preflight, syntaxErrorCount);

  const issues = useMemo(
    () => collectPreflightIssues(images, preflight, syntaxErrors),
    [images, preflight, syntaxErrors],
  );
  const errorIssues = useMemo(
    () => issues.filter((item) => item.severity === 'error'),
    [issues],
  );
  const warnIssues = useMemo(
    () => issues.filter((item) => item.severity === 'warn'),
    [issues],
  );

  const keptOriginalCount = preflight.image_stats.kept_original ?? 0;

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(markdown);
      setCopied(true);
      setTimeout(() => setCopied(false), 1800);
    } catch {
      // ignore clipboard errors
    }
  };

  return (
    <div className={`code-markdocx-preflight is-${level}`}>
      <div className="code-markdocx-preflight-main">
        <div className="code-markdocx-preflight-status">
          {level === 'ok' ? (
            <CheckCircle2 size={18} />
          ) : (
            <AlertTriangle size={18} />
          )}
          <div>
            <strong>
              {level === 'ok'
                ? '已通过 code-archdoc 合规预检'
                : level === 'error'
                  ? `存在待处理错误（共 ${errorIssues.length} 项，建议先修复或改为引用原图后再导出）`
                  : '存在体积/凭据/低置信度提醒'}
            </strong>
            <div className="code-markdocx-preflight-metrics">
              <span>
                体积: {formatBytes(preflight.byte_size)} / {formatBytes(preflight.max_bytes)}
              </span>
              <span>预估 Token: ~{preflight.estimated_tokens}</span>
              <span>标题: {preflight.heading_count}</span>
              <span>Mermaid 图: {preflight.mermaid_block_count}</span>
              {(preflight.code_block_count ?? 0) > 0 && (
                <span>代码块: {preflight.code_block_count}</span>
              )}
              {keptOriginalCount > 0 && (
                <span className="is-info-text">
                  <ImageIcon size={13} />
                  <span>引用原图: {keptOriginalCount}</span>
                </span>
              )}
              {preflight.image_stats.failed > 0 && (
                <button
                  type="button"
                  className="code-markdocx-link-btn is-danger"
                  onClick={() => setShowErrors((v) => !v)}
                >
                  <span>转译失败: {preflight.image_stats.failed}</span>
                </button>
              )}
              {syntaxErrorCount > 0 && (
                <button
                  type="button"
                  className="code-markdocx-link-btn is-danger"
                  onClick={() => setShowErrors((v) => !v)}
                >
                  <span>语法错误: {syntaxErrorCount}</span>
                </button>
              )}
              {preflight.has_inline_base64 && (
                <button
                  type="button"
                  className="code-markdocx-link-btn is-danger"
                  onClick={() => setShowErrors((v) => !v)}
                >
                  <span>内嵌 Base64</span>
                </button>
              )}
              {preflight.image_stats.unsupported_vector > 0 && (
                <span className="is-warn-text">
                  矢量图降级: {preflight.image_stats.unsupported_vector}
                </span>
              )}
              {preflight.image_stats.low_confidence > 0 && (
                <span className="is-warn-text">
                  低置信度: {preflight.image_stats.low_confidence}
                </span>
              )}
              {preflight.secret_warnings.length > 0 && (
                <button
                  type="button"
                  className="code-markdocx-link-btn"
                  onClick={() => setShowSecrets((v) => !v)}
                >
                  <ShieldAlert size={14} />
                  <span>疑似凭据: {preflight.secret_warnings.length} 处</span>
                </button>
              )}
              {errorIssues.length > 0 && (
                <button
                  type="button"
                  className="code-markdocx-link-btn is-danger"
                  onClick={() => setShowErrors((v) => !v)}
                >
                  <span>{showErrors ? '收起错误详情' : `展开错误详情 (${errorIssues.length})`}</span>
                  {showErrors ? <ChevronUp size={13} /> : <ChevronDown size={13} />}
                </button>
              )}
              {warnIssues.length > 0 && (
                <button
                  type="button"
                  className="code-markdocx-link-btn"
                  onClick={() => setShowWarns((v) => !v)}
                >
                  <span>{showWarns ? '收起提醒' : `查看提醒 (${warnIssues.length})`}</span>
                  {showWarns ? <ChevronUp size={13} /> : <ChevronDown size={13} />}
                </button>
              )}
            </div>
          </div>
        </div>

        <div className="code-markdocx-preflight-actions">
          <button type="button" className="code-markdocx-btn is-secondary is-sm" onClick={handleCopy}>
            {copied ? <Check size={14} /> : <Copy size={14} />}
            <span>{copied ? '已复制' : '复制 Markdown'}</span>
          </button>
          <a
            href={getExportUrl(jobId)}
            download
            className="code-markdocx-btn is-primary is-sm"
          >
            <Download size={14} />
            <span>导出 .md</span>
          </a>
          {keptOriginalCount > 0 && (
            <a
              href={getExportUrl(jobId, { bundle: true })}
              download
              className="code-markdocx-btn is-secondary is-sm"
              title="将 Markdown 与引用的原图 assets/ 合并打包为 ZIP 下载"
            >
              <FileArchive size={14} />
              <span>打包下载 (.zip 含原图)</span>
            </a>
          )}
          {preflight.exceeds_size_limit && (
            <a
              href={getExportUrl(jobId, {
                split: true,
                bundle: keptOriginalCount > 0,
              })}
              download
              className="code-markdocx-btn is-warning is-sm"
              title="超过 512 KB，按二级标题拆分为多份合规 .md 打包下载"
            >
              <FileArchive size={14} />
              <span>
                {keptOriginalCount > 0 ? '按 H2 拆分打包 (.zip 含原图)' : '按 H2 拆分导出 (.zip)'}
              </span>
            </a>
          )}
        </div>
      </div>

      {showErrors && errorIssues.length > 0 && (
        <div className="code-markdocx-errors-box">
          <strong>待处理错误清单：</strong>
          <ul className="code-markdocx-issue-list">
            {errorIssues.map((issue) => (
              <li key={issue.id} className="code-markdocx-issue-item">
                <div className="code-markdocx-issue-text">
                  {issue.imageId && (
                    <span className="code-markdocx-badge is-id">{issue.imageId}</span>
                  )}
                  <span className="code-markdocx-issue-title">{issue.title}</span>
                  <span className="code-markdocx-issue-detail">{issue.detail}</span>
                </div>
                {issue.imageId && onLocateImage && (
                  <button
                    type="button"
                    className="code-markdocx-btn is-secondary is-sm"
                    onClick={() => onLocateImage(issue.imageId!)}
                  >
                    <MapPin size={13} />
                    <span>定位查看</span>
                  </button>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}

      {showWarns && warnIssues.length > 0 && (
        <div className="code-markdocx-warns-box">
          <strong>提醒事项：</strong>
          <ul className="code-markdocx-issue-list">
            {warnIssues.map((issue) => (
              <li key={issue.id} className="code-markdocx-issue-item">
                <div className="code-markdocx-issue-text">
                  {issue.imageId && (
                    <span className="code-markdocx-badge is-id">{issue.imageId}</span>
                  )}
                  <span className="code-markdocx-issue-title">{issue.title}</span>
                  <span className="code-markdocx-issue-detail">{issue.detail}</span>
                </div>
                {issue.imageId && onLocateImage && (
                  <button
                    type="button"
                    className="code-markdocx-btn is-secondary is-sm"
                    onClick={() => onLocateImage(issue.imageId!)}
                  >
                    <MapPin size={13} />
                    <span>定位查看</span>
                  </button>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}

      {showSecrets && preflight.secret_warnings.length > 0 && (
        <div className="code-markdocx-secrets-box">
          <strong>检测到疑似敏感信息（上传 code-archdoc 时可能触发凭据门禁）：</strong>
          <ul>
            {preflight.secret_warnings.map((w, idx) => (
              <li key={`${w.line}-${idx}`}>
                第 {w.line} 行 [{w.rule}]: <code>{w.snippet}</code>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
};

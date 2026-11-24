import React, { useState } from 'react';
import {
  AlertTriangle,
  Check,
  CheckCircle2,
  Copy,
  Download,
  FileArchive,
  ShieldAlert,
} from 'lucide-react';
import { getExportUrl } from '../api/client';
import type { PreflightReport } from '../types/api';
import { formatBytes, getPreflightStatusLevel } from '../utils/markdocxUtils';

export interface PreflightBannerProps {
  jobId: string;
  markdown: string;
  preflight: PreflightReport;
  syntaxErrorCount: number;
}

export const PreflightBanner: React.FC<PreflightBannerProps> = ({
  jobId,
  markdown,
  preflight,
  syntaxErrorCount,
}) => {
  const [copied, setCopied] = useState(false);
  const [showSecrets, setShowSecrets] = useState(false);

  const level = getPreflightStatusLevel(preflight, syntaxErrorCount);

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
                  ? '存在待处理错误（建议先修复再导出上传）'
                  : '存在体积/凭据/低置信度提醒'}
            </strong>
            <div className="code-markdocx-preflight-metrics">
              <span>
                体积: {formatBytes(preflight.byte_size)} / {formatBytes(preflight.max_bytes)}
              </span>
              <span>预估 Token: ~{preflight.estimated_tokens}</span>
              <span>标题: {preflight.heading_count}</span>
              <span>Mermaid 图: {preflight.mermaid_block_count}</span>
              {syntaxErrorCount > 0 && (
                <span className="is-danger-text">语法错误: {syntaxErrorCount}</span>
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
            </div>
          </div>
        </div>

        <div className="code-markdocx-preflight-actions">
          <button type="button" className="code-markdocx-btn is-secondary is-sm" onClick={handleCopy}>
            {copied ? <Check size={14} /> : <Copy size={14} />}
            <span>{copied ? '已复制' : '复制 Markdown'}</span>
          </button>
          <a
            href={getExportUrl(jobId, false)}
            download
            className="code-markdocx-btn is-primary is-sm"
          >
            <Download size={14} />
            <span>导出 .md</span>
          </a>
          {preflight.exceeds_size_limit && (
            <a
              href={getExportUrl(jobId, true)}
              download
              className="code-markdocx-btn is-warning is-sm"
              title="超过 512 KB，按二级标题拆分为多份合规 .md 打包下载"
            >
              <FileArchive size={14} />
              <span>按 H2 拆分导出 (.zip)</span>
            </a>
          )}
        </div>
      </div>

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

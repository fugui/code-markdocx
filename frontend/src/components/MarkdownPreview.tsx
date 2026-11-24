import React, { useState } from 'react';
import { Check, Copy } from 'lucide-react';

export interface MarkdownPreviewProps {
  markdown: string;
}

export const MarkdownPreview: React.FC<MarkdownPreviewProps> = ({ markdown }) => {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(markdown);
      setCopied(true);
      setTimeout(() => setCopied(false), 1800);
    } catch {
      // ignore
    }
  };

  return (
    <div className="code-markdocx-card">
      <div className="code-markdocx-card-header">
        <div>
          <h3 className="code-markdocx-section-title">完整 Markdown 预览</h3>
          <p className="code-markdocx-muted">
            已剥离页眉页脚与 Base64 内嵌图像，可直接复制或导出上传至 code-archdoc。
          </p>
        </div>
        <button
          type="button"
          className="code-markdocx-btn is-secondary is-sm"
          onClick={handleCopy}
        >
          {copied ? <Check size={14} /> : <Copy size={14} />}
          <span>{copied ? '已复制' : '复制全文'}</span>
        </button>
      </div>
      <pre className="code-markdocx-md-pre">
        <code>{markdown}</code>
      </pre>
    </div>
  );
};

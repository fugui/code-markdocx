import React, { useRef, useState } from 'react';
import { FileUp, Info, Loader2, Sparkles, Upload } from 'lucide-react';
import type { CreateJobParams } from '../api/client';
import type { HealthInfo } from '../types/api';

export interface UploadDropzoneProps {
  health: HealthInfo | null;
  submitting: boolean;
  onSubmit: (params: CreateJobParams) => void;
}

export const UploadDropzone: React.FC<UploadDropzoneProps> = ({
  health,
  submitting,
  onSubmit,
}) => {
  const inputRef = useRef<HTMLInputElement | null>(null);
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [dragActive, setDragActive] = useState(false);
  const [stripTOC, setStripTOC] = useState(true);
  const [stripRevisionTable, setStripRevisionTable] = useState(true);
  const [stripCover, setStripCover] = useState(false);
  const [skipVLM, setSkipVLM] = useState(false);
  const [glossary, setGlossary] = useState('');
  const [fileError, setFileError] = useState('');

  const handleSelectFile = (file: File | null) => {
    setFileError('');
    if (!file) {
      setSelectedFile(null);
      return;
    }
    if (!file.name.toLowerCase().endsWith('.docx')) {
      setFileError('仅支持 .docx 格式文档（老版 .doc 请先用 Word/WPS 另存为 .docx）');
      setSelectedFile(null);
      return;
    }
    setSelectedFile(file);
  };

  const handleDrop = (e: React.DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    setDragActive(false);
    const file = e.dataTransfer.files?.[0] ?? null;
    handleSelectFile(file);
  };

  const handleStart = () => {
    if (!selectedFile || submitting) {
      return;
    }
    onSubmit({
      file: selectedFile,
      glossary,
      strip_toc: stripTOC,
      strip_cover: stripCover,
      strip_revision_table: stripRevisionTable,
      skip_vlm: skipVLM,
    });
  };

  return (
    <div className="code-markdocx-card">
      <div className="code-markdocx-card-header">
        <div>
          <h2 className="code-markdocx-section-title">上传 .docx 架构设计文档</h2>
          <p className="code-markdocx-muted">
            自动提取文本、表格与章节上下文，并将嵌入式 UML / 架构图转译为可渲染 Mermaid 与语义说明。
          </p>
        </div>
        {health && (
          <div className="code-markdocx-health-pills">
            <span
              className={`code-markdocx-pill ${
                health.vlm_configured ? 'is-success' : 'is-warning'
              }`}
            >
              {health.vlm_configured
                ? `VLM 就绪: ${health.vlm_model}`
                : 'VLM 未配置 (仅提取正文)'}
            </span>
            <span className="code-markdocx-pill">
              {health.soffice_available ? '支持 EMF/WMF 矢量图' : 'EMF 矢量图提示降级'}
            </span>
          </div>
        )}
      </div>

      {health && !health.vlm_configured && (
        <div className="code-markdocx-alert is-warning">
          <Info size={16} />
          <span>
            当前服务端 <code>config.yaml</code> 尚未配置有效 VLM 参数（base_url / api_key /
            model），将仅提取正文结构与图片占位；配置并重启服务后可自动识图转 Mermaid。
          </span>
        </div>
      )}

      <div
        className={`code-markdocx-dropzone ${dragActive ? 'is-active' : ''}`}
        onDragOver={(e) => {
          e.preventDefault();
          setDragActive(true);
        }}
        onDragLeave={() => setDragActive(false)}
        onDrop={handleDrop}
        onClick={() => inputRef.current?.click()}
      >
        <input
          ref={inputRef}
          type="file"
          accept=".docx"
          className="code-markdocx-hidden-input"
          onChange={(e) => handleSelectFile(e.target.files?.[0] ?? null)}
        />
        <Upload size={28} className="code-markdocx-drop-icon" />
        {selectedFile ? (
          <div>
            <div className="code-markdocx-drop-filename">{selectedFile.name}</div>
            <div className="code-markdocx-muted">
              {(selectedFile.size / 1024).toFixed(1)} KB · 点击可重新选择
            </div>
          </div>
        ) : (
          <div>
            <div className="code-markdocx-drop-title">拖拽 .docx 文件到此处，或点击选择文件</div>
            <div className="code-markdocx-muted">
              单文件上限 {health?.max_upload_mb ?? 50} MB · 仅临时处理，不永久落库
            </div>
          </div>
        )}
      </div>

      {fileError && <div className="code-markdocx-alert is-danger">{fileError}</div>}

      <div className="code-markdocx-options-grid">
        <div className="code-markdocx-checkboxes">
          <label className="code-markdocx-check">
            <input
              type="checkbox"
              checked={stripTOC}
              onChange={(e) => setStripTOC(e.target.checked)}
            />
            <span>剔除 Word 自动目录 (TOC)</span>
          </label>
          <label className="code-markdocx-check">
            <input
              type="checkbox"
              checked={stripRevisionTable}
              onChange={(e) => setStripRevisionTable(e.target.checked)}
            />
            <span>剔除文首修订记录表</span>
          </label>
          <label className="code-markdocx-check">
            <input
              type="checkbox"
              checked={stripCover}
              onChange={(e) => setStripCover(e.target.checked)}
            />
            <span>剔除首个标题前封面页</span>
          </label>
          <label className="code-markdocx-check">
            <input
              type="checkbox"
              checked={skipVLM}
              onChange={(e) => setSkipVLM(e.target.checked)}
            />
            <span>仅提取正文结构（跳过 VLM 识图）</span>
          </label>
        </div>

        <div className="code-markdocx-glossary">
          <label className="code-markdocx-label" htmlFor="markdocx-glossary">
            可选项目术语 / 英文缩写提示（辅助 VLM 准确识别 UML 类名与服务名）
          </label>
          <textarea
            id="markdocx-glossary"
            rows={3}
            className="code-markdocx-textarea"
            placeholder="例如：CodeBench 网关, AuthService 认证服务, OrderSaga 订单状态机..."
            value={glossary}
            onChange={(e) => setGlossary(e.target.value)}
          />
        </div>
      </div>

      <div className="code-markdocx-actions-end">
        <button
          type="button"
          className="code-markdocx-btn is-primary"
          disabled={!selectedFile || submitting}
          onClick={handleStart}
        >
          {submitting ? (
            <>
              <Loader2 size={16} className="code-markdocx-spin" />
              <span>正在解析并创建转换任务...</span>
            </>
          ) : (
            <>
              {skipVLM ? <FileUp size={16} /> : <Sparkles size={16} />}
              <span>开始转换为 Markdown</span>
            </>
          )}
        </button>
      </div>
    </div>
  );
};

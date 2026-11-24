import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import {
  FileText,
  Image as ImageIcon,
  Images,
  Loader2,
  PlusCircle,
  RefreshCw,
} from 'lucide-react';
import { useToast } from '@code/common';
import {
  createJob,
  fetchHealth,
  fetchJobDetail,
  getImageUrl,
  keepOriginalsBatch,
  patchImage,
  retryImage,
  type CreateJobParams,
  type PatchImageParams,
  type RetryImageParams,
} from '../api/client';
import { ImageReviewCard } from '../components/ImageReviewCard';
import { MarkdownPreview } from '../components/MarkdownPreview';
import { PreflightBanner } from '../components/PreflightBanner';
import { UploadDropzone } from '../components/UploadDropzone';
import type { HealthInfo, JobDetail } from '../types/api';
import {
  countSyntaxErrors,
  filterImages,
  isUnconvertedWithImage,
  type ReviewFilter,
} from '../utils/markdocxUtils';
import { validateMermaidSyntax } from '../utils/mermaidCheck';

export interface WorkbenchPageProps {
  routePrefix: string;
}

export const WorkbenchPage: React.FC<WorkbenchPageProps> = ({ routePrefix }) => {
  const { jobId } = useParams<{ jobId?: string }>();
  const navigate = useNavigate();
  const { showToast } = useToast();

  const [health, setHealth] = useState<HealthInfo | null>(null);
  const [job, setJob] = useState<JobDetail | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [loadingJob, setLoadingJob] = useState(false);
  const [batchUpdating, setBatchUpdating] = useState(false);
  const [activeTab, setActiveTab] = useState<'images' | 'markdown'>('images');
  const [filter, setFilter] = useState<ReviewFilter>('all');
  const [syntaxErrors, setSyntaxErrors] = useState<Record<string, string | undefined>>({});
  const [highlightedImageId, setHighlightedImageId] = useState<string | null>(null);
  const highlightTimerRef = useRef<number | null>(null);

  useEffect(() => {
    return () => {
      if (highlightTimerRef.current !== null) {
        window.clearTimeout(highlightTimerRef.current);
      }
    };
  }, []);

  useEffect(() => {
    void fetchHealth()
      .then(setHealth)
      .catch(() => {
        // ignore health error on initial load
      });
  }, []);

  const loadJob = useCallback(async (id: string) => {
    setLoadingJob(true);
    try {
      const detail = await fetchJobDetail(id);
      setJob(detail);
    } catch (err) {
      showToast(err instanceof Error ? err.message : '加载任务失败', 'error');
      setJob(null);
    } finally {
      setLoadingJob(false);
    }
  }, [showToast]);

  useEffect(() => {
    if (!jobId) {
      setJob(null);
      return;
    }
    void loadJob(jobId);
  }, [jobId, loadJob]);

  // Poll every 1.5s while job is running (proxy-safe, avoids SSE buffering issues)
  useEffect(() => {
    if (!job || job.status !== 'running') {
      return;
    }
    const timer = window.setInterval(() => {
      void fetchJobDetail(job.id)
        .then((detail) => setJob(detail))
        .catch(() => {
          // ignore transient poll failure
        });
    }, 1500);
    return () => window.clearInterval(timer);
  }, [job]);

  // Run mermaid.parse() syntax check whenever images change
  useEffect(() => {
    if (!job) {
      setSyntaxErrors({});
      return;
    }
    let cancelled = false;
    const checkAll = async () => {
      const next: Record<string, string | undefined> = {};
      for (const img of job.images) {
        if (
          img.skipped ||
          img.status === 'skipped' ||
          img.keep_original ||
          img.category === 'table_screenshot'
        ) {
          continue;
        }
        const code = (img.mermaid_code || '').trim();
        if (!code) {
          continue;
        }
        const res = await validateMermaidSyntax(code);
        if (!res.valid) {
          next[img.image_id] = res.error || 'Mermaid 语法解析失败';
        }
      }
      if (!cancelled) {
        setSyntaxErrors(next);
      }
    };
    void checkAll();
    return () => {
      cancelled = true;
    };
  }, [job]);

  const handleCreateJob = async (params: CreateJobParams) => {
    setSubmitting(true);
    try {
      const created = await createJob(params);
      setJob(created);
      setFilter('all');
      setActiveTab(created.images.length > 0 ? 'images' : 'markdown');
      navigate(`${routePrefix}/jobs/${created.id}`);
      showToast('文档解析完成，已开始图表转译', 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : '创建任务失败', 'error');
    } finally {
      setSubmitting(false);
    }
  };

  const handleSaveImage = async (imageId: string, patch: PatchImageParams) => {
    if (!job) {
      return;
    }
    try {
      const resp = await patchImage(job.id, imageId, patch);
      setJob((prev) =>
        prev
          ? {
              ...prev,
              markdown: resp.markdown,
              preflight: resp.preflight,
              images: prev.images.map((item) =>
                item.image_id === imageId ? resp.image : item,
              ),
            }
          : prev,
      );
      showToast(`已保存 ${imageId} 修改`, 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : '保存失败', 'error');
    }
  };

  const handleRetryImage = async (imageId: string, payload: RetryImageParams) => {
    if (!job) {
      return;
    }
    try {
      const resp = await retryImage(job.id, imageId, payload);
      setJob((prev) =>
        prev
          ? {
              ...prev,
              markdown: resp.markdown,
              preflight: resp.preflight,
              images: prev.images.map((item) =>
                item.image_id === imageId ? resp.image : item,
              ),
            }
          : prev,
      );
      showToast(`${imageId} 已重新转译`, 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : '重转失败', 'error');
    }
  };

  const handleKeepAllUnconvertedOriginals = async () => {
    if (!job) {
      return;
    }
    setBatchUpdating(true);
    try {
      const resp = await keepOriginalsBatch(job.id, { keep_original: true });
      setJob((prev) =>
        prev
          ? {
              ...prev,
              images: resp.images,
              markdown: resp.markdown,
              preflight: resp.preflight,
            }
          : prev,
      );
      if (resp.updated_count > 0) {
        showToast(`已为 ${resp.updated_count} 张未转 Mermaid 图片启用原图引用`, 'success');
      } else {
        showToast('当前没有需要切换为原图的图片', 'info');
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : '批量设置引用原图失败', 'error');
    } finally {
      setBatchUpdating(false);
    }
  };

  const syntaxErrorCount = useMemo(
    () => (job ? countSyntaxErrors(job.images, syntaxErrors) : 0),
    [job, syntaxErrors],
  );

  const visibleImages = useMemo(
    () => (job ? filterImages(job.images, filter, syntaxErrors) : []),
    [job, filter, syntaxErrors],
  );

  const unconvertedImages = useMemo(
    () => (job ? job.images.filter(isUnconvertedWithImage) : []),
    [job],
  );

  const assetUrlMap = useMemo(() => {
    if (!job) {
      return {};
    }
    const map: Record<string, string> = {};
    for (const img of job.images) {
      if (img.has_image_file && img.asset_filename) {
        const url = getImageUrl(job.id, img.image_id);
        map[`assets/${img.asset_filename}`] = url;
        map[`./assets/${img.asset_filename}`] = url;
      }
    }
    return map;
  }, [job]);

  const handleLocateImage = useCallback(
    (imageId: string) => {
      if (!job) {
        return;
      }
      setActiveTab('images');
      const currentlyVisible = filterImages(job.images, filter, syntaxErrors).some(
        (item) => item.image_id === imageId,
      );
      if (!currentlyVisible) {
        setFilter('all');
      }
      setHighlightedImageId(imageId);
      if (highlightTimerRef.current !== null) {
        window.clearTimeout(highlightTimerRef.current);
      }
      window.setTimeout(() => {
        const el = document.getElementById(`image-card-${imageId}`);
        if (el) {
          el.scrollIntoView({ behavior: 'smooth', block: 'center' });
        }
      }, 60);
      highlightTimerRef.current = window.setTimeout(() => {
        setHighlightedImageId((cur) => (cur === imageId ? null : cur));
      }, 2200);
    },
    [job, filter, syntaxErrors],
  );

  if (!jobId) {
    return (
      <div className="code-markdocx-container">
        <UploadDropzone health={health} submitting={submitting} onSubmit={handleCreateJob} />
      </div>
    );
  }

  if (loadingJob && !job) {
    return (
      <div className="code-markdocx-container">
        <div className="code-markdocx-empty">
          <Loader2 size={24} className="code-markdocx-spin" />
          <span>正在加载转换任务...</span>
        </div>
      </div>
    );
  }

  if (!job) {
    return (
      <div className="code-markdocx-container">
        <div className="code-markdocx-card">
          <p className="code-markdocx-muted">未找到该转换任务（可能已超出保留时间被自动清理）。</p>
          <button
            type="button"
            className="code-markdocx-btn is-primary"
            onClick={() => navigate(`${routePrefix}/workbench`)}
          >
            <PlusCircle size={16} />
            <span>上传新的 .docx 文档</span>
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="code-markdocx-container">
      <div className="code-markdocx-job-topbar">
        <div>
          <h2 className="code-markdocx-job-title">{job.filename}</h2>
          <div className="code-markdocx-muted">
            已剥离噪声：目录 {job.noise_stats.stripped_toc_blocks} 块 · 修订表{' '}
            {job.noise_stats.stripped_revision_tables} 张 · 封面{' '}
            {job.noise_stats.stripped_cover_blocks} 块
          </div>
        </div>
        <div className="code-markdocx-job-actions">
          {job.status === 'running' && (
            <span className="code-markdocx-pill is-info">
              <Loader2 size={14} className="code-markdocx-spin" />
              <span>
                VLM 转译中 ({job.progress_done}/{job.progress_total})
              </span>
            </span>
          )}
          <button
            type="button"
            className="code-markdocx-btn is-secondary is-sm"
            onClick={() => void loadJob(job.id)}
          >
            <RefreshCw size={14} />
            <span>刷新</span>
          </button>
          <button
            type="button"
            className="code-markdocx-btn is-primary is-sm"
            onClick={() => navigate(`${routePrefix}/workbench`)}
          >
            <PlusCircle size={14} />
            <span>转换新文档</span>
          </button>
        </div>
      </div>

      <PreflightBanner
        jobId={job.id}
        markdown={job.markdown}
        preflight={job.preflight}
        images={job.images}
        syntaxErrors={syntaxErrors}
        syntaxErrorCount={syntaxErrorCount}
        onLocateImage={handleLocateImage}
      />

      <div className="code-markdocx-tabs-row">
        <div className="code-markdocx-tabs">
          <button
            type="button"
            className={`code-markdocx-tab ${activeTab === 'images' ? 'is-active' : ''}`}
            onClick={() => setActiveTab('images')}
          >
            <Images size={15} />
            <span>图文对照审阅 ({job.images.length})</span>
          </button>
          <button
            type="button"
            className={`code-markdocx-tab ${activeTab === 'markdown' ? 'is-active' : ''}`}
            onClick={() => setActiveTab('markdown')}
          >
            <FileText size={15} />
            <span>完整 Markdown 预览</span>
          </button>
        </div>

        {activeTab === 'images' && job.images.length > 0 && (
          <div className="code-markdocx-filters">
            {(
              [
                ['all', '全部'],
                ['attention', '需关注'],
                ['mermaid', 'Mermaid 图'],
                ['original', '引用原图'],
                ['skipped', '已跳过'],
              ] as const
            ).map(([key, label]) => (
              <button
                key={key}
                type="button"
                className={`code-markdocx-filter-btn ${filter === key ? 'is-active' : ''}`}
                onClick={() => setFilter(key)}
              >
                {label}
              </button>
            ))}

            {job.status !== 'running' && unconvertedImages.length > 0 && (
              <button
                type="button"
                className="code-markdocx-btn is-secondary is-sm"
                disabled={batchUpdating}
                onClick={() => void handleKeepAllUnconvertedOriginals()}
                title="将所有未转成 Mermaid 或表格的位图批量改为引用原图（assets/）"
              >
                {batchUpdating ? (
                  <Loader2 size={14} className="code-markdocx-spin" />
                ) : (
                  <ImageIcon size={14} />
                )}
                <span>未转 Mermaid 图引用原图 ({unconvertedImages.length})</span>
              </button>
            )}
          </div>
        )}
      </div>

      {activeTab === 'images' ? (
        visibleImages.length === 0 ? (
          <div className="code-markdocx-card">
            <p className="code-markdocx-muted">
              {job.images.length === 0
                ? '该 .docx 文档未包含需要处理的嵌入式图片，已直接生成纯净 Markdown。'
                : '当前筛选条件下没有匹配的图片卡片。'}
            </p>
          </div>
        ) : (
          <div className="code-markdocx-review-list">
            {visibleImages.map((img) => (
              <ImageReviewCard
                key={img.image_id}
                jobId={job.id}
                image={img}
                syntaxError={syntaxErrors[img.image_id]}
                vlmConfigured={Boolean(health?.vlm_configured)}
                highlighted={highlightedImageId === img.image_id}
                onSave={handleSaveImage}
                onRetry={handleRetryImage}
              />
            ))}
          </div>
        )
      ) : (
        <MarkdownPreview
          markdown={job.markdown}
          assetUrlMap={assetUrlMap}
          keptOriginalCount={job.preflight.image_stats.kept_original ?? 0}
        />
      )}
    </div>
  );
};

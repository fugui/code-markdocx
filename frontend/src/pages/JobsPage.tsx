import React, { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { Download, ExternalLink, Loader2, RefreshCw, Trash2 } from 'lucide-react';
import { useToast } from '@code/common';
import { deleteJob, fetchJobs, getExportUrl } from '../api/client';
import type { JobSummary } from '../types/api';
import { formatBytes } from '../utils/markdocxUtils';

export interface JobsPageProps {
  routePrefix: string;
}

export const JobsPage: React.FC<JobsPageProps> = ({ routePrefix }) => {
  const { showToast } = useToast();
  const [jobs, setJobs] = useState<JobSummary[]>([]);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await fetchJobs();
      setJobs(res.jobs || []);
    } catch (err) {
      showToast(err instanceof Error ? err.message : '加载任务列表失败', 'error');
    } finally {
      setLoading(false);
    }
  }, [showToast]);

  useEffect(() => {
    void load();
  }, [load]);

  const handleDelete = async (id: string) => {
    try {
      await deleteJob(id);
      setJobs((prev) => prev.filter((j) => j.id !== id));
      showToast('已清理临时任务', 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : '删除失败', 'error');
    }
  };

  return (
    <div className="code-markdocx-container">
      <div className="code-markdocx-card">
        <div className="code-markdocx-card-header">
          <div>
            <h2 className="code-markdocx-section-title">本次会话临时转换任务</h2>
            <p className="code-markdocx-muted">
              无状态临时存储，默认保留 120 分钟后自动销毁（零数据库持久化）。
            </p>
          </div>
          <button
            type="button"
            className="code-markdocx-btn is-secondary is-sm"
            onClick={() => void load()}
          >
            <RefreshCw size={14} />
            <span>刷新</span>
          </button>
        </div>

        {loading ? (
          <div className="code-markdocx-empty">
            <Loader2 size={20} className="code-markdocx-spin" />
            <span>正在加载...</span>
          </div>
        ) : jobs.length === 0 ? (
          <div className="code-markdocx-empty">
            <span>暂无进行中或保留的临时任务。</span>
            <Link to={`${routePrefix}/workbench`} className="code-markdocx-btn is-primary is-sm">
              上传 .docx 开始转换
            </Link>
          </div>
        ) : (
          <div className="code-markdocx-table-wrap">
            <table className="code-markdocx-table">
              <thead>
                <tr>
                  <th>文件名</th>
                  <th>状态</th>
                  <th>图片进度</th>
                  <th>输出体积</th>
                  <th>创建时间</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {jobs.map((j) => (
                  <tr key={j.id}>
                    <td>
                      <Link to={`${routePrefix}/jobs/${j.id}`} className="code-markdocx-link">
                        {j.filename}
                      </Link>
                    </td>
                    <td>
                      <span
                        className={`code-markdocx-pill ${
                          j.status === 'done'
                            ? 'is-success'
                            : j.status === 'failed'
                              ? 'is-danger'
                              : 'is-info'
                        }`}
                      >
                        {j.status}
                      </span>
                    </td>
                    <td>
                      {j.progress_done}/{j.progress_total} (共 {j.image_count} 图)
                    </td>
                    <td>{formatBytes(j.byte_size)}</td>
                    <td>{new Date(j.created_at).toLocaleString()}</td>
                    <td>
                      <div className="code-markdocx-row-actions">
                        <Link
                          to={`${routePrefix}/jobs/${j.id}`}
                          className="code-markdocx-btn is-secondary is-sm"
                        >
                          <ExternalLink size={13} />
                          <span>审阅</span>
                        </Link>
                        <a
                          href={getExportUrl(j.id, false)}
                          download
                          className="code-markdocx-btn is-secondary is-sm"
                        >
                          <Download size={13} />
                          <span>.md</span>
                        </a>
                        <button
                          type="button"
                          className="code-markdocx-btn is-ghost is-sm"
                          onClick={() => void handleDelete(j.id)}
                        >
                          <Trash2 size={13} />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
};

import { MODULE_BASE_PATH } from '../constants';
import type {
  DiagramCategory,
  DiagramType,
  HealthInfo,
  ImageMutationResponse,
  JobDetail,
  JobSummary,
} from '../types/api';

export function getApiBaseUrl(): string {
  if (
    typeof window !== 'undefined' &&
    Boolean((window as { __POWERED_BY_PORTAL__?: boolean }).__POWERED_BY_PORTAL__)
  ) {
    return `${MODULE_BASE_PATH}/api`;
  }
  return '/api';
}

export function getImageUrl(jobId: string, imageId: string): string {
  return `${getApiBaseUrl()}/jobs/${encodeURIComponent(jobId)}/images/${encodeURIComponent(imageId)}`;
}

export function getExportUrl(jobId: string, split = false): string {
  const base = `${getApiBaseUrl()}/jobs/${encodeURIComponent(jobId)}/export`;
  return split ? `${base}?split=true` : base;
}

async function requestJson<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(`${getApiBaseUrl()}${path}`, init);
  if (!resp.ok) {
    let msg = `请求失败 (${resp.status})`;
    try {
      const body = (await resp.json()) as { error?: string };
      if (body.error) {
        msg = body.error;
      }
    } catch {
      // ignore non-json
    }
    throw new Error(msg);
  }
  return (await resp.json()) as T;
}

export function fetchHealth(): Promise<HealthInfo> {
  return requestJson<HealthInfo>('/health');
}

export function fetchJobs(): Promise<{ jobs: JobSummary[] }> {
  return requestJson<{ jobs: JobSummary[] }>('/jobs');
}

export function fetchJobDetail(jobId: string): Promise<JobDetail> {
  return requestJson<JobDetail>(`/jobs/${encodeURIComponent(jobId)}`);
}

export function deleteJob(jobId: string): Promise<{ deleted: boolean }> {
  return requestJson<{ deleted: boolean }>(`/jobs/${encodeURIComponent(jobId)}`, {
    method: 'DELETE',
  });
}

export interface CreateJobParams {
  file: File;
  glossary: string;
  strip_toc: boolean;
  strip_cover: boolean;
  strip_revision_table: boolean;
  skip_vlm: boolean;
}

export function createJob(params: CreateJobParams): Promise<JobDetail> {
  const form = new FormData();
  form.append('file', params.file);
  form.append('glossary', params.glossary);
  form.append('strip_toc', String(params.strip_toc));
  form.append('strip_cover', String(params.strip_cover));
  form.append('strip_revision_table', String(params.strip_revision_table));
  form.append('skip_vlm', String(params.skip_vlm));
  return requestJson<JobDetail>('/jobs', {
    method: 'POST',
    body: form,
  });
}

export interface PatchImageParams {
  category?: DiagramCategory;
  diagram_type?: DiagramType;
  mermaid_code?: string;
  markdown_table?: string;
  summary?: string;
  skipped?: boolean;
}

export function patchImage(
  jobId: string,
  imageId: string,
  body: PatchImageParams,
): Promise<ImageMutationResponse> {
  return requestJson<ImageMutationResponse>(
    `/jobs/${encodeURIComponent(jobId)}/images/${encodeURIComponent(imageId)}`,
    {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  );
}

export interface RetryImageParams {
  mermaid_error?: string;
  instruction?: string;
}

export function retryImage(
  jobId: string,
  imageId: string,
  body: RetryImageParams,
): Promise<ImageMutationResponse> {
  return requestJson<ImageMutationResponse>(
    `/jobs/${encodeURIComponent(jobId)}/images/${encodeURIComponent(imageId)}/retry`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  );
}

export type DiagramCategory =
  | 'uml_diagram'
  | 'complex_arch'
  | 'table_screenshot'
  | 'ui_or_decorative';

export type DiagramType =
  | 'sequence'
  | 'class'
  | 'state'
  | 'flowchart'
  | 'er'
  | 'other';

export type ConfidenceLevel = 'high' | 'medium' | 'low';

export type ImageStatus =
  | 'pending'
  | 'processing'
  | 'done'
  | 'failed'
  | 'skipped'
  | 'unsupported_vector';

export type JobStatus = 'running' | 'done' | 'failed';

export interface HealthInfo {
  status: string;
  version: string;
  vlm_configured: boolean;
  vlm_model: string;
  soffice_available: boolean;
  max_upload_mb: number;
  ref_doc_max_bytes: number;
}

export interface NoiseStats {
  stripped_toc_blocks: number;
  stripped_cover_blocks: number;
  stripped_revision_tables: number;
}

export interface JobOptions {
  strip_toc: boolean;
  strip_cover: boolean;
  strip_revision_table: boolean;
  skip_vlm: boolean;
}

export interface ImageState {
  image_id: string;
  orig_filename: string;
  section_path: string;
  caption: string;
  context_before: string;
  context_after: string;
  width: number;
  height: number;
  status: ImageStatus;
  skipped: boolean;
  skip_reason?: string;
  unsupported_vector: boolean;
  vector_warning?: string;
  error?: string;
  category?: DiagramCategory;
  diagram_type?: DiagramType;
  mermaid_code?: string;
  markdown_table?: string;
  summary?: string;
  confidence?: ConfidenceLevel;
  notes?: string;
}

export interface SecretWarning {
  line: number;
  rule: string;
  snippet: string;
}

export interface ImageStats {
  total: number;
  done: number;
  pending: number;
  failed: number;
  skipped: number;
  low_confidence: number;
  unsupported_vector: number;
}

export interface SplitSuggestion {
  filename: string;
  title: string;
  byte_size: number;
  estimated_tokens: number;
}

export interface PreflightReport {
  byte_size: number;
  max_bytes: number;
  exceeds_size_limit: boolean;
  estimated_tokens: number;
  line_count: number;
  heading_count: number;
  mermaid_block_count: number;
  has_inline_base64: boolean;
  image_stats: ImageStats;
  secret_warnings: SecretWarning[];
  split_suggestions?: SplitSuggestion[];
}

export interface JobSummary {
  id: string;
  filename: string;
  status: JobStatus;
  created_at: string;
  expires_at: string;
  progress_done: number;
  progress_total: number;
  image_count: number;
  byte_size: number;
  noise_stats: NoiseStats;
}

export interface JobDetail {
  id: string;
  filename: string;
  glossary_hints: string;
  options: JobOptions;
  status: JobStatus;
  error?: string;
  created_at: string;
  updated_at: string;
  expires_at: string;
  progress_done: number;
  progress_total: number;
  noise_stats: NoiseStats;
  images: ImageState[];
  markdown: string;
  preflight: PreflightReport;
}

export interface ImageMutationResponse {
  image: ImageState;
  markdown: string;
  preflight: PreflightReport;
}

import mermaid from 'mermaid';

let initialized = false;

export interface MermaidSyntaxResult {
  valid: boolean;
  error?: string;
}

export function ensureMermaidInitialized(): void {
  if (initialized) {
    return;
  }
  mermaid.initialize({
    startOnLoad: false,
    securityLevel: 'strict',
  });
  initialized = true;
}

export async function validateMermaidSyntax(code: string): Promise<MermaidSyntaxResult> {
  const trimmed = code.trim();
  if (!trimmed) {
    return { valid: true };
  }
  ensureMermaidInitialized();
  try {
    await mermaid.parse(trimmed);
    return { valid: true };
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return { valid: false, error: msg };
  }
}

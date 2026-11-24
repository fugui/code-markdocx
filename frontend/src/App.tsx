import React from 'react';
import { BrowserRouter, Link, Navigate, Route, Routes, useLocation } from 'react-router-dom';
import { FileCode2, ListChecks, Moon, Sun, UploadCloud } from 'lucide-react';
import { ErrorBoundary, ToastProvider, setupFetchInterceptor, useTheme } from '@code/common';
import { MODULE_BASE_PATH, MODULE_TITLE } from './constants';
import { JobsPage } from './pages/JobsPage';
import { WorkbenchPage } from './pages/WorkbenchPage';

setupFetchInterceptor({ appPrefix: MODULE_BASE_PATH });

export interface AppProps {
  isEmbedded?: boolean;
}

const EmbeddedRoutes: React.FC = () => {
  const defaultTarget = `${MODULE_BASE_PATH}/workbench`;
  return (
    <div className="code-markdocx-root is-embedded">
      <Routes>
        <Route path="/" element={<Navigate to={defaultTarget} replace />} />
        <Route path={MODULE_BASE_PATH} element={<Navigate to={defaultTarget} replace />} />
        <Route path={`${MODULE_BASE_PATH}/`} element={<Navigate to={defaultTarget} replace />} />

        <Route path="/workbench" element={<WorkbenchPage routePrefix={MODULE_BASE_PATH} />} />
        <Route
          path={`${MODULE_BASE_PATH}/workbench`}
          element={<WorkbenchPage routePrefix={MODULE_BASE_PATH} />}
        />

        <Route path="/jobs" element={<JobsPage routePrefix={MODULE_BASE_PATH} />} />
        <Route
          path={`${MODULE_BASE_PATH}/jobs`}
          element={<JobsPage routePrefix={MODULE_BASE_PATH} />}
        />

        <Route path="/jobs/:jobId" element={<WorkbenchPage routePrefix={MODULE_BASE_PATH} />} />
        <Route
          path={`${MODULE_BASE_PATH}/jobs/:jobId`}
          element={<WorkbenchPage routePrefix={MODULE_BASE_PATH} />}
        />

        <Route path="*" element={<Navigate to={defaultTarget} replace />} />
      </Routes>
    </div>
  );
};

const StandaloneShell: React.FC = () => {
  const location = useLocation();
  const { theme, toggleTheme } = useTheme('light');
  const isJobs = location.pathname === '/jobs';

  return (
    <div className="code-markdocx-root is-standalone">
      <header className="code-markdocx-topbar">
        <div className="code-markdocx-topbar-brand">
          <FileCode2 size={20} />
          <span className="code-markdocx-brand-title">{MODULE_TITLE}</span>
          <span className="code-markdocx-brand-sub">Code-Markdocx</span>
        </div>

        <nav className="code-markdocx-topbar-nav">
          <Link
            to="/workbench"
            className={`code-markdocx-nav-link ${!isJobs ? 'is-active' : ''}`}
          >
            <UploadCloud size={15} />
            <span>转换工作台</span>
          </Link>
          <Link
            to="/jobs"
            className={`code-markdocx-nav-link ${isJobs ? 'is-active' : ''}`}
          >
            <ListChecks size={15} />
            <span>临时任务</span>
          </Link>
          <button
            type="button"
            className="code-markdocx-btn is-ghost is-sm"
            onClick={toggleTheme}
            title="切换亮/暗主题"
          >
            {theme === 'dark' ? <Sun size={16} /> : <Moon size={16} />}
          </button>
        </nav>
      </header>

      <main className="code-markdocx-main">
        <Routes>
          <Route path="/" element={<Navigate to="/workbench" replace />} />
          <Route path="/workbench" element={<WorkbenchPage routePrefix="" />} />
          <Route path="/jobs" element={<JobsPage routePrefix="" />} />
          <Route path="/jobs/:jobId" element={<WorkbenchPage routePrefix="" />} />
          <Route path="*" element={<Navigate to="/workbench" replace />} />
        </Routes>
      </main>
    </div>
  );
};

export const App: React.FC<AppProps> = ({ isEmbedded = false }) => {
  if (isEmbedded) {
    return (
      <ErrorBoundary title="Word 转 Markdown 子应用异常">
        <ToastProvider>
          <EmbeddedRoutes />
        </ToastProvider>
      </ErrorBoundary>
    );
  }

  return (
    <ErrorBoundary title="Word 转 Markdown 运行异常">
      <ToastProvider>
        <BrowserRouter>
          <StandaloneShell />
        </BrowserRouter>
      </ToastProvider>
    </ErrorBoundary>
  );
};

export default App;

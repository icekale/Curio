import { Cloud, Play, RefreshCw, XCircle } from "lucide-react";
import { pageMeta, type Page } from "./navigation";
import type { Batch } from "../types";
import { sourceLabel, statusLabel } from "../utils/labels";

export function CommandBar({
  page,
  activeTask,
  taskProgress,
  busy,
  refreshing,
  onRefresh,
  onStart,
  onCloudStart,
  onStop,
}: {
  page: Page;
  activeTask: Batch | null;
  taskProgress: { total: number; done: number; percent: number };
  busy: boolean;
  refreshing: boolean;
  onRefresh: () => void;
  onStart: () => void;
  onCloudStart: () => void;
  onStop: () => void;
}) {
  const meta = pageMeta(page);
  return (
    <header className="commandBar">
      <div className="pageIdentity">
        <span>{meta.label}</span>
        <h1>{meta.title}</h1>
        <p>{meta.description}</p>
      </div>
      <div className="taskCapsule">
        <span className={activeTask ? "pulseDot running" : "pulseDot"} />
        <div>
          <b>
            {activeTask
              ? `${sourceLabel(activeTask.source)} ${statusLabel(activeTask.status)}`
              : "无活跃任务"}
          </b>
          <small>
            {activeTask
              ? `${taskProgress.done}/${taskProgress.total} · ${taskProgress.percent}%`
              : "可以开始本地或云端整理"}
          </small>
        </div>
      </div>
      <div className="commandActions">
        <button
          className={refreshing ? "iconButton spinning" : "iconButton"}
          onClick={onRefresh}
          disabled={refreshing}
          title="刷新"
          type="button"
        >
          <RefreshCw size={18} />
        </button>
        {activeTask ? (
          <button
            className="dangerButton"
            onClick={onStop}
            disabled={busy}
            title="停止任务"
            type="button"
          >
            <XCircle size={17} />
            <span>停止</span>
          </button>
        ) : (
          <>
            <button
              className="secondaryButton"
              onClick={onCloudStart}
              disabled={busy}
              title="整理云端"
              type="button"
            >
              <Cloud size={17} />
              <span>云端</span>
            </button>
            <button
              className="primaryButton"
              onClick={onStart}
              disabled={busy}
              title="开始整理"
              type="button"
            >
              <Play size={17} />
              <span>开始</span>
            </button>
          </>
        )}
      </div>
    </header>
  );
}

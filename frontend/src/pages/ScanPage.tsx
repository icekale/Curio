import { Play, XCircle } from "lucide-react";
import { Card } from "../components/Card";
import { MediaList } from "../components/MediaTable";
import { TableSearch } from "../components/TableSearch";
import type { CurioConsole } from "../hooks/useCurioConsole";
import { percent, taskDone } from "../utils/format";
import { sourceLabel, statusLabel } from "../utils/labels";

export function ScanPage({ console }: { console: CurioConsole }) {
  const total = console.latestBatch?.total ?? 0;
  const done = taskDone(console.latestBatch ?? {});
  const currentPercent = percent(done, total);
  const isCloud = console.activeTask?.source === "cloud";

  return (
    <>
      <Card variant="hero" className="scanConsole">
        <div className="scanTarget">
          <span className="eyebrow">{isCloud ? "云端根目录" : "入库目录"}</span>
          <h2>{isCloud ? "CloudDrive2" : console.directories.incoming_path || "未配置"}</h2>
          <p>
            {console.activeTask
              ? `${sourceLabel(console.activeTask.source)}${statusLabel(console.activeTask.status)}，正在推进媒体档案流水线。`
              : "选择开始后，Curio 会扫描、识别、匹配并规划归档路径。"}
          </p>
        </div>
        {console.activeTask ? (
          <button
            className="dangerButton"
            onClick={console.stopActiveTask}
            disabled={console.busy}
            title="停止任务"
            type="button"
          >
            <XCircle size={17} />
            <span>停止</span>
          </button>
        ) : (
          <button
            className="primaryButton"
            onClick={console.startScan}
            disabled={console.busy}
            title="开始整理"
            type="button"
          >
            <Play size={17} />
            <span>开始本地整理</span>
          </button>
        )}
      </Card>

      <Card title="整理流水线" eyebrow="Pipeline">
        <div className="pipeline">
          {["扫描", "解析", "识别", "匹配", "合集", "移动"].map((step, index) => (
            <div
              className={index / 5 <= currentPercent / 100 ? "pipelineStep active" : "pipelineStep"}
              key={step}
            >
              <span>{index + 1}</span>
              <b>{step}</b>
            </div>
          ))}
        </div>
        <div className="progressTrack">
          <i style={{ width: `${currentPercent}%` }} />
        </div>
        <div className="progressMeta">
          <span>{statusLabel(console.latestBatch?.status ?? "idle")}</span>
          <b>
            {done}/{total} · {currentPercent}%
          </b>
        </div>
      </Card>

      <Card
        title="扫描结果"
        eyebrow="Scan Results"
        action={<TableSearch value={console.mediaQuery} onChange={console.setMediaQuery} />}
      >
        <MediaList
          page={console.mediaPage}
          mode="processing"
          offset={console.mediaOffset}
          setOffset={console.setMediaOffset}
          selected={console.selectedMedia}
          setSelected={console.setSelectedMedia}
          onDelete={console.deleteMediaRecords}
          onRearchive={console.openRearchive}
          busy={console.busy}
        />
      </Card>
    </>
  );
}

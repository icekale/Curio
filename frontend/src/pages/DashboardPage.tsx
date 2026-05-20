import { Activity, AlertTriangle, Archive, Radio, Server } from "lucide-react";
import { Card, MetricCard } from "../components/Card";
import { StatusPill } from "../components/StatusPill";
import type { Batch, Health, MediaFile, MediaStats } from "../types";
import { formatDate, percent, taskDone } from "../utils/format";
import { queueLabel, sourceLabel, statusLabel } from "../utils/labels";
import { MediaPreviewTable } from "./MediaPreviewTable";

export function DashboardPage({
  stats,
  batches,
  health,
  mediaFiles,
  activeTask,
}: {
  stats: MediaStats;
  batches: Batch[];
  health: Health | null;
  mediaFiles: MediaFile[];
  activeTask: Batch | null;
}) {
  const done = taskDone(activeTask ?? {});
  const taskPercent = percent(done, activeTask?.total ?? 0);
  const risk = [
    { label: "失败文件", value: stats.failed, tone: "danger" as const },
    {
      label: "缺失合集",
      value: stats.incomplete_collection,
      tone: "warning" as const,
    },
    {
      label: "缺季缺集",
      value: stats.missing_tv_season_count + stats.missing_tv_episode_count,
      tone: "info" as const,
    },
  ].sort((a, b) => b.value - a.value)[0];

  return (
    <>
      <section className="dashboardHero">
        <Card variant="hero" className="taskHero">
          <div className="heroCopy">
            <span className="eyebrow">Curio Archive Console</span>
            <h2>{activeTask ? "整理任务正在推进" : "档案库处于待命状态"}</h2>
            <p>
              {activeTask
                ? `${sourceLabel(activeTask.source)}任务已处理 ${done}/${activeTask.total}，失败 ${activeTask.failed}，缺合集 ${activeTask.incomplete_collection}。`
                : "可以从本地入库目录或 CloudDrive2 云端开始一次新的媒体整理。"}
            </p>
          </div>
          {activeTask ? (
            <div
              className="progressDial"
              style={{ "--value": `${taskPercent}%` } as React.CSSProperties}
            >
              <strong>{taskPercent}</strong>
              <span>%</span>
            </div>
          ) : (
            <div className="idleStatusCard">
              <Activity size={24} />
              <strong>待命</strong>
              <span>无活跃任务</span>
            </div>
          )}
        </Card>
        <Card variant={risk.value > 0 ? "danger" : "surface"} className="riskCard">
          <div className="riskIcon">
            {risk.value > 0 ? <AlertTriangle size={24} /> : <Archive size={24} />}
          </div>
          <span className="eyebrow">待处理焦点</span>
          <h2>{risk.value > 0 ? risk.label : "暂无明显异常"}</h2>
          <strong>{risk.value}</strong>
          <p>{risk.value > 0 ? "建议优先进入对应页面处理。" : "媒体库整体状态稳定。"}</p>
        </Card>
      </section>

      <section className="metricGrid">
        <MetricCard label="总数" value={stats.total} hint="全部档案" />
        <MetricCard label="完成" value={stats.done} hint="已归档" tone="success" />
        <MetricCard label="失败" value={stats.failed} hint="需诊断" tone="danger" />
        <MetricCard
          label="缺合集"
          value={stats.incomplete_collection}
          hint="待补齐"
          tone="warning"
        />
        <MetricCard
          label="缺季"
          value={stats.missing_tv_season_count}
          hint="剧集缺口"
          tone="info"
        />
        <MetricCard
          label="缺集"
          value={stats.missing_tv_episode_count}
          hint="剧集缺口"
          tone="info"
        />
      </section>

      <section className="dashboardGrid">
        <Card title="最近批次" eyebrow="Batch Ledger">
          <BatchTable rows={batches.slice(0, 6)} />
        </Card>
        <Card title="队列水位" eyebrow="Queue Waterline">
          <div className="queueList">
            <div className="queueRow current">
              <span>
                <Radio size={16} />
                当前任务
              </span>
              <b>
                {activeTask
                  ? `${sourceLabel(activeTask.source)} ${statusLabel(activeTask.status)}`
                  : "空闲"}
              </b>
            </div>
            {Object.entries(health?.queues ?? {}).map(([name, value]) => (
              <div className="queueRow" key={name}>
                <span>{queueLabel(name)}</span>
                <div className="queueMeter">
                  <i style={{ width: `${Math.min(100, value * 12)}%` }} />
                </div>
                <b>{value}</b>
              </div>
            ))}
            {Object.keys(health?.queues ?? {}).length === 0 && (
              <div className="emptyState">
                <Server size={20} />
                <span>暂无队列数据</span>
              </div>
            )}
          </div>
        </Card>
      </section>

      <Card title="最近活动" eyebrow="Recent Activity">
        <MediaPreviewTable rows={mediaFiles.slice(0, 8)} />
      </Card>
    </>
  );
}

function BatchTable({ rows }: { rows: Batch[] }) {
  return (
    <div className="tableFrame">
      <table className="dataTable">
        <thead>
          <tr>
            <th>批次</th>
            <th>来源</th>
            <th>状态</th>
            <th>总数</th>
            <th>完成</th>
            <th>失败</th>
            <th>开始</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.batch_id}>
              <td>{row.batch_id.slice(0, 8)}</td>
              <td>{sourceLabel(row.source)}</td>
              <td>
                <StatusPill value={row.status} />
              </td>
              <td>{row.total}</td>
              <td>{row.done}</td>
              <td>{row.failed}</td>
              <td>{formatDate(row.started_at)}</td>
            </tr>
          ))}
          {rows.length === 0 && (
            <tr>
              <td className="emptyCell" colSpan={7}>
                暂无批次
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

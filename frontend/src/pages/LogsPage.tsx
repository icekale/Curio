import { useState } from "react";
import { History } from "lucide-react";
import { endpoints } from "../api";
import { Card } from "../components/Card";
import { Pager } from "../components/Pager";
import { StatusPill } from "../components/StatusPill";
import type { LogEntry, LogPage } from "../types";
import type { LogFilter } from "../hooks/useCurioConsole";
import { formatDuration, formatFullDate, prettyJSON, shortText } from "../utils/format";
import { logTypeLabel, statusLabel, statusTone } from "../utils/labels";

const logFilters: { value: LogFilter; label: string }[] = [
  { value: "all", label: "全部" },
  { value: "ai_filename", label: "AI 识别" },
  { value: "playback", label: "播放" },
  { value: "p115_sync", label: "STRM" },
  { value: "operation", label: "整理" },
  { value: "scan_batch", label: "扫描" },
];

export function LogsPage({
  page,
  filter,
  setFilter,
  offset,
  setOffset,
  loading,
}: {
  page: LogPage;
  filter: LogFilter;
  setFilter: (value: LogFilter) => void;
  offset: number;
  setOffset: (value: number) => void;
  loading: boolean;
}) {
  const rows = page.items ?? [];
  return (
    <>
      <Card variant="hero" className="pageHero">
        <div>
          <span className="eyebrow">Timeline</span>
          <h2>排障时间线</h2>
          <p>按类型查看 AI、播放、STRM、扫描和整理日志，展开后可读取接口与 JSON 详情。</p>
        </div>
        <div className="heroStat">
          <History size={24} />
          <strong>{page.total}</strong>
          <span>条日志</span>
        </div>
      </Card>

      <div className="tabBar">
        {logFilters.map((item) => (
          <button
            key={item.value}
            className={filter === item.value ? "tabButton active" : "tabButton"}
            onClick={() => setFilter(item.value)}
            type="button"
          >
            <History size={16} />
            <span>{item.label}</span>
          </button>
        ))}
      </div>

      <Card
        title="日志记录"
        eyebrow="Event Ledger"
        action={
          <span className="blockMeta">
            {loading
              ? "加载中"
              : page.total === 0
                ? "0 / 0"
                : `${offset + 1}-${Math.min(offset + page.limit, page.total)} / ${page.total}`}
          </span>
        }
      >
        <div className="tableFrame">
          <table className="dataTable logTable">
            <thead>
              <tr>
                <th>时间</th>
                <th>类型</th>
                <th>状态</th>
                <th>消息</th>
                <th>关联</th>
                <th>耗时</th>
                <th>详情</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((entry) => (
                <LogTableRow key={entry.id} entry={entry} />
              ))}
              {rows.length === 0 && (
                <tr>
                  <td className="emptyCell" colSpan={7}>
                    {loading ? "正在加载日志" : "暂无日志"}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        <Pager page={page} offset={offset} setOffset={setOffset} />
      </Card>
    </>
  );
}

function LogTableRow({ entry }: { entry: LogEntry }) {
  const [open, setOpen] = useState(false);
  const [detail, setDetail] = useState<LogEntry | null>(null);
  const [loading, setLoading] = useState(false);
  const [detailError, setDetailError] = useState("");
  const hasDetail = Boolean(
    entry.detail ||
      entry.error_message ||
      entry.request_json ||
      entry.response_json ||
      entry.parsed_json ||
      entry.type === "ai_filename",
  );
  const displayed = detail ?? entry;
  const needsRemoteDetail =
    entry.type === "ai_filename" &&
    !detail &&
    !entry.request_json &&
    !entry.response_json &&
    !entry.parsed_json;
  const toggleOpen = () => {
    if (!hasDetail) return;
    const nextOpen = !open;
    setOpen(nextOpen);
    if (!nextOpen || !needsRemoteDetail || loading) return;
    setLoading(true);
    setDetailError("");
    endpoints
      .logDetail(entry.id)
      .then(setDetail)
      .catch((error) => {
        setDetailError(error instanceof Error ? error.message : "日志详情加载失败");
      })
      .finally(() => setLoading(false));
  };
  return (
    <>
      <tr className={hasDetail ? "clickableRow" : ""} onClick={toggleOpen}>
        <td>{formatFullDate(entry.created_at)}</td>
        <td>{logTypeLabel(entry.type)}</td>
        <td>
          <StatusPill
            value={entry.status}
            label={statusLabel(entry.status)}
            tone={statusTone(entry.status)}
          />
        </td>
        <td title={entry.message}>
          <div className="logMessage">
            <b>{shortText(entry.message || "-", 64)}</b>
            <small>{shortText(entry.path || entry.file_name || "", 72)}</small>
          </div>
        </td>
        <td title={entry.batch_id || entry.file_id}>
          {shortText(entry.file_id || entry.batch_id || "-", 18)}
        </td>
        <td>{formatDuration(entry.duration_ms)}</td>
        <td>{hasDetail ? (open ? "收起" : "展开") : "-"}</td>
      </tr>
      {open && hasDetail && (
        <tr className="detailRow">
          <td colSpan={7}>
            <div className="logDetailGrid">
              {loading && <LogDetail wide label="状态" value="正在加载详情" />}
              <LogDetail label="来源" value={displayed.source} />
              <LogDetail label="模型" value={displayed.model} />
              <LogDetail label="接口" value={displayed.base_url} />
              <LogDetail label="代理" value={displayed.proxy_url} />
              <LogDetail label="响应格式" value={displayed.response_format} />
              <LogDetail
                label="HTTP"
                value={displayed.http_status ? String(displayed.http_status) : ""}
              />
              <LogDetail wide label="文件" value={displayed.file_name} />
              <LogDetail wide label="路径" value={displayed.path} />
              <LogDetail wide label="摘要" value={displayed.detail} />
              <LogDetail wide label="错误" value={displayed.error_message || detailError} />
              <LogDetail wide label="AI 解析" value={displayed.parsed_json} code />
              <LogDetail wide label="请求 JSON" value={displayed.request_json} code />
              <LogDetail wide label="响应 JSON" value={displayed.response_json} code />
            </div>
          </td>
        </tr>
      )}
    </>
  );
}

function LogDetail({
  label,
  value,
  wide,
  code,
}: {
  label: string;
  value: string;
  wide?: boolean;
  code?: boolean;
}) {
  if (!value) return null;
  return (
    <div className={wide ? "logDetailField wide" : "logDetailField"}>
      <span>{label}</span>
      {code ? <pre>{prettyJSON(value)}</pre> : <code>{value}</code>}
    </div>
  );
}

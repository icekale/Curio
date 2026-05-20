import { ArchiveRestore, Trash2 } from "lucide-react";
import { useState } from "react";
import type { MediaFile, MediaFilePage } from "../types";
import { errorCodeLabel, mediaTypeLabel, statusLabel } from "../utils/labels";
import {
  formatDate,
  formatFullDate,
  formatSize,
  shortPath,
  techTags,
  twoDigit,
} from "../utils/format";
import { Pager } from "./Pager";
import { StatusPill } from "./StatusPill";
import { Modal } from "./Modal";

export type MediaMode = "processing" | "staging" | "failed";

export function MediaList({
  page,
  mode,
  offset,
  setOffset,
  selected,
  setSelected,
  onDelete,
  onRearchive,
  busy,
}: {
  page: MediaFilePage;
  mode: MediaMode;
  offset: number;
  setOffset: (value: number) => void;
  selected: string[];
  setSelected: (value: string[]) => void;
  onDelete: (files: MediaFile[]) => void;
  onRearchive: (files: MediaFile[] | MediaFile) => void;
  busy: boolean;
}) {
  const [detail, setDetail] = useState<MediaFile | null>(null);
  const rows = page.items;
  const ids = rows.map((file) => file.file_id);
  const selectedSet = new Set(selected);
  const selectedFiles = rows.filter((file) => selectedSet.has(file.file_id));
  const allSelected = rows.length > 0 && ids.every((id) => selectedSet.has(id));
  const toggleAll = () => {
    if (allSelected) {
      setSelected(selected.filter((id) => !ids.includes(id)));
      return;
    }
    setSelected(Array.from(new Set([...selected, ...ids])));
  };
  const toggleOne = (id: string) => {
    setSelected(
      selectedSet.has(id)
        ? selected.filter((item) => item !== id)
        : [...selected, id],
    );
  };
  return (
    <>
      <div className="bulkBar">
        <span>
          已选 {selectedFiles.length} / 当前页 {rows.length} / 共 {page.total}
        </span>
        <div className="inlineActions">
          <button
            className="secondaryButton"
            onClick={() => onRearchive(selectedFiles)}
            disabled={busy || selectedFiles.length === 0}
            type="button"
          >
            <ArchiveRestore size={17} />
            <span>批量归档</span>
          </button>
          <button
            className="dangerButton"
            onClick={() => onDelete(selectedFiles)}
            disabled={busy || selectedFiles.length === 0}
            type="button"
          >
            <Trash2 size={17} />
            <span>批量删除</span>
          </button>
        </div>
      </div>
      <div className="tableFrame">
        <table className={`dataTable mediaTable media-${mode}`}>
          <colgroup>
            <col style={{ width: 54 }} />
            <col style={{ width: mode === "failed" ? "28%" : "30%" }} />
            <col style={{ width: mode === "failed" ? "24%" : "18%" }} />
            <col style={{ width: "22%" }} />
            <col style={{ width: "24%" }} />
            <col style={{ width: 124 }} />
            <col style={{ width: 110 }} />
          </colgroup>
          <thead>
            <tr>
              <th className="selectCol">
                <input
                  type="checkbox"
                  checked={allSelected}
                  onClick={(event) => event.stopPropagation()}
                  onChange={toggleAll}
                  aria-label="选择当前页"
                />
              </th>
              <th>档案</th>
              <th>{mode === "failed" ? "错误" : "状态"}</th>
              <th>参数</th>
              <th>路径</th>
              <th>时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((file) => (
              <MediaRow
                key={file.file_id}
                file={file}
                mode={mode}
                selected={selected.includes(file.file_id)}
                busy={busy}
                onToggle={() => toggleOne(file.file_id)}
                onOpen={() => setDetail(file)}
                onDelete={() => onDelete([file])}
                onRearchive={() => onRearchive(file)}
              />
            ))}
            {rows.length === 0 && (
              <tr>
                <td className="emptyCell" colSpan={7}>
                  暂无数据
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <Pager page={page} offset={offset} setOffset={setOffset} />
      <MediaDetailModal
        file={detail}
        busy={busy}
        onClose={() => setDetail(null)}
        onDelete={(file) => {
          setDetail(null);
          onDelete([file]);
        }}
        onRearchive={(file) => {
          setDetail(null);
          onRearchive(file);
        }}
      />
    </>
  );
}

function MediaRow({
  file,
  mode,
  selected,
  busy,
  onToggle,
  onOpen,
  onDelete,
  onRearchive,
}: {
  file: MediaFile;
  mode: MediaMode;
  selected: boolean;
  busy: boolean;
  onToggle: () => void;
  onOpen: () => void;
  onDelete: () => void;
  onRearchive: () => void;
}) {
  const canRearchive = ["failed", "done", "incomplete_collection"].includes(
    file.process_status,
  );
  const name =
    mode === "staging"
      ? file.final_file_name || file.original_file_name
      : file.original_file_name;
  return (
    <tr className={`clickableRow row-${file.process_status}`} onClick={onOpen}>
      <td className="selectCol">
        <input
          type="checkbox"
          checked={selected}
          onClick={(event) => event.stopPropagation()}
          onChange={onToggle}
          aria-label={`选择 ${file.original_file_name}`}
        />
      </td>
      <td>
        <div className="mediaName">
          <b title={name}>{name}</b>
          <small>{mediaSubtitle(file)}</small>
        </div>
      </td>
      <td>
        {mode === "failed" ? (
          <div className="errorCell">
            <b>{errorCodeLabel(file.error_code) || "未知错误"}</b>
            <small title={file.error_message}>{file.error_message || "-"}</small>
          </div>
        ) : (
          <div className="statusStack">
            <StatusPill value={file.process_status} />
            <small>{statusLabel(file.match_status)}</small>
          </div>
        )}
      </td>
      <td>
        <TechTagList file={file} />
      </td>
      <td title={pathForMode(file, mode)}>
        <span className="pathText">{shortPath(pathForMode(file, mode)) || "-"}</span>
      </td>
      <td>{formatDate(file.updated_at)}</td>
      <td>
        <div className="rowIconActions">
          <button
            className="iconButton warningIcon"
            onClick={(event) => {
              event.stopPropagation();
              onRearchive();
            }}
            disabled={busy || !canRearchive}
            title={canRearchive ? "重新归档" : "仅失败或已归档记录可重新归档"}
            type="button"
          >
            <ArchiveRestore size={17} />
          </button>
          <button
            className="iconButton dangerIcon"
            onClick={(event) => {
              event.stopPropagation();
              onDelete();
            }}
            disabled={busy}
            title="删除数据库记录"
            type="button"
          >
            <Trash2 size={17} />
          </button>
        </div>
      </td>
    </tr>
  );
}

function TechTagList({ file }: { file: MediaFile }) {
  const tags = techTags(file);
  if (tags.length === 0) return <span className="mutedText">未知</span>;
  return (
    <div className="techTags">
      {tags.map((tag) => (
        <span key={tag}>{tag}</span>
      ))}
    </div>
  );
}

function mediaSubtitle(file: MediaFile) {
  const meta = [
    mediaTypeLabel(file.media_type),
    file.parse_title || "",
    file.season > 0 || file.episode > 0
      ? `S${twoDigit(file.season)}E${twoDigit(file.episode)}`
      : "",
  ].filter(Boolean);
  return meta.join(" · ") || file.extension || "-";
}

function pathForMode(file: MediaFile, mode: MediaMode) {
  if (mode === "staging") return file.final_path;
  if (mode === "failed") {
    return (
      file.current_path || file.original_path || file.final_path || file.planned_target
    );
  }
  return file.final_path || file.planned_target || file.current_path;
}

function MediaDetailModal({
  file,
  busy,
  onClose,
  onDelete,
  onRearchive,
}: {
  file: MediaFile | null;
  busy: boolean;
  onClose: () => void;
  onDelete: (file: MediaFile) => void;
  onRearchive: (file: MediaFile) => void;
}) {
  const canRearchive = file
    ? ["failed", "done", "incomplete_collection"].includes(file.process_status)
    : false;
  return (
    <Modal
      open={Boolean(file)}
      eyebrow={file ? `${mediaTypeLabel(file.media_type)} · ${statusLabel(file.process_status)}` : ""}
      title={file?.final_file_name || file?.original_file_name || "档案详情"}
      className="mediaDetailModal"
      onClose={onClose}
      footer={
        file && (
          <>
            <button
              className="secondaryButton"
              onClick={() => onRearchive(file)}
              disabled={busy || !canRearchive}
              type="button"
            >
              <ArchiveRestore size={17} />
              <span>重新归档</span>
            </button>
            <button
              className="dangerButton"
              onClick={() => onDelete(file)}
              disabled={busy}
              type="button"
            >
              <Trash2 size={17} />
              <span>删除记录</span>
            </button>
          </>
        )
      }
    >
      {file && (
        <>
          <div className="detailSummary">
            <SummaryItem label="状态" value={statusLabel(file.process_status)} />
            <SummaryItem label="匹配" value={statusLabel(file.match_status)} />
            <SummaryItem label="大小" value={formatSize(file.file_size)} />
            <SummaryItem label="更新时间" value={formatDate(file.updated_at)} />
          </div>
          <div className="detailGrid">
            <DetailSection title="识别">
              <DetailField label="媒体类型" value={mediaTypeLabel(file.media_type)} />
              <DetailField label="解析标题" value={file.parse_title} />
              <DetailField label="年份" value={file.parse_year ? String(file.parse_year) : ""} />
              <DetailField
                label="季集"
                value={
                  file.season || file.episode
                    ? `S${twoDigit(file.season)}E${twoDigit(file.episode)}`
                    : ""
                }
              />
              <DetailField label="片源" value={file.source} />
              <DetailField label="扩展名" value={file.extension} />
            </DetailSection>
            <DetailSection title="技术参数">
              <DetailField label="分辨率" value={file.resolution} />
              <DetailField label="视频编码" value={file.video_codec} />
              <DetailField label="音频编码" value={file.audio_codec} />
              <DetailField label="声道" value={file.audio_channels} />
              <DetailField label="HDR" value={file.hdr_format} />
            </DetailSection>
            <DetailSection title="路径" wide>
              <DetailField label="原始路径" value={file.original_path} wide />
              <DetailField label="当前路径" value={file.current_path} wide />
              <DetailField label="计划路径" value={file.planned_target} wide />
              <DetailField label="最终路径" value={file.final_path} wide />
              <DetailField label="校验路径" value={file.last_verified_path} wide />
            </DetailSection>
            {(file.error_code || file.error_message) && (
              <DetailSection title="错误" wide tone="danger">
                <DetailField
                  label="错误码"
                  value={errorCodeLabel(file.error_code) || file.error_code}
                />
                <DetailField label="错误信息" value={file.error_message} wide />
              </DetailSection>
            )}
            <DetailSection title="记录" wide>
              <DetailField label="文件 ID" value={file.file_id} wide />
              <DetailField label="批次 ID" value={file.batch_id} wide />
              <DetailField label="哈希" value={file.file_hash} wide />
              <DetailField label="哈希类型" value={file.hash_type} />
              <DetailField label="创建时间" value={formatFullDate(file.created_at)} />
              <DetailField label="更新时间" value={formatFullDate(file.updated_at)} />
            </DetailSection>
          </div>
        </>
      )}
    </Modal>
  );
}

function SummaryItem({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <span>{label}</span>
      <strong>{value || "-"}</strong>
    </div>
  );
}

function DetailSection({
  title,
  children,
  wide = false,
  tone,
}: {
  title: string;
  children: React.ReactNode;
  wide?: boolean;
  tone?: "danger";
}) {
  return (
    <section
      className={[
        "detailSection",
        wide ? "wide" : "",
        tone ? `detail-${tone}` : "",
      ]
        .filter(Boolean)
        .join(" ")}
    >
      <h3>{title}</h3>
      <div className="detailFields">{children}</div>
    </section>
  );
}

function DetailField({
  label,
  value,
  wide = false,
}: {
  label: string;
  value: string;
  wide?: boolean;
}) {
  const display = value || "-";
  return (
    <div className={wide ? "detailField wide" : "detailField"}>
      <span>{label}</span>
      <code title={display}>{display}</code>
    </div>
  );
}

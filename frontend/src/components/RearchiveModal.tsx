import { ArchiveRestore, X } from "lucide-react";
import type { MediaFile } from "../types";
import type { RearchiveDraft } from "../hooks/useCurioConsole";
import { Modal } from "./Modal";

export function RearchiveModal({
  files,
  draft,
  busy,
  setDraft,
  onClose,
  onSubmit,
}: {
  files: MediaFile[];
  draft: RearchiveDraft;
  busy: boolean;
  setDraft: (value: RearchiveDraft) => void;
  onClose: () => void;
  onSubmit: () => void;
}) {
  const open = files.length > 0;
  const title =
    files.length === 1 ? files[0].original_file_name : `${files.length} 条记录`;
  const update = (patch: Partial<RearchiveDraft>) =>
    setDraft({ ...draft, ...patch });
  return (
    <Modal
      open={open}
      eyebrow="重新归档"
      title={title}
      className="rearchiveModal"
      onClose={onClose}
      footer={
        <>
          <button
            className="secondaryButton"
            onClick={onClose}
            disabled={busy}
            type="button"
          >
            <X size={17} />
            <span>取消</span>
          </button>
          <button
            className="primaryButton"
            onClick={onSubmit}
            disabled={busy}
            type="button"
          >
            <ArchiveRestore size={17} />
            <span>归档</span>
          </button>
        </>
      }
    >
      <div className="rearchiveBody">
        <div className="segmentedControl">
          <button
            className={draft.mediaType === "movie" ? "active" : ""}
            onClick={() => update({ mediaType: "movie" })}
            type="button"
          >
            电影
          </button>
          <button
            className={draft.mediaType === "tv_episode" ? "active" : ""}
            onClick={() => update({ mediaType: "tv_episode" })}
            type="button"
          >
            剧集
          </button>
        </div>
        <label className="field">
          <span>TMDB ID（可空）</span>
          <input
            value={draft.tmdbID}
            inputMode="numeric"
            autoFocus
            placeholder="留空则按当前文件名重新匹配"
            onChange={(event) => update({ tmdbID: event.target.value })}
            onKeyDown={(event) => {
              if (event.key === "Enter") onSubmit();
            }}
          />
        </label>
        {draft.mediaType === "tv_episode" && (
          <div className="formGrid twoCols">
            <label className="field">
              <span>季号</span>
              <input
                value={draft.season}
                inputMode="numeric"
                placeholder={files.length === 1 ? "留空使用识别值" : "批量通常留空"}
                onChange={(event) => update({ season: event.target.value })}
              />
            </label>
            <label className="field">
              <span>集号</span>
              <input
                value={draft.episode}
                inputMode="numeric"
                placeholder={files.length === 1 ? "留空使用识别值" : "批量通常留空"}
                onChange={(event) => update({ episode: event.target.value })}
              />
            </label>
            <label className="field">
              <span>季偏移</span>
              <input
                value={draft.seasonOffset}
                inputMode="numeric"
                placeholder="例如 -1、1"
                onChange={(event) => update({ seasonOffset: event.target.value })}
              />
            </label>
            <label className="field">
              <span>集偏移</span>
              <input
                value={draft.episodeOffset}
                inputMode="numeric"
                placeholder="例如 -1、1"
                onChange={(event) => update({ episodeOffset: event.target.value })}
              />
            </label>
          </div>
        )}
        <p className="inlineHint">
          TMDB ID 留空时按当前文件名重新匹配。删除记录只删除数据库数据，不删除真实源文件。
        </p>
      </div>
    </Modal>
  );
}

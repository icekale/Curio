import type { MediaFile } from "../types";
import { formatDate, shortPath, techTags, twoDigit } from "../utils/format";
import { mediaTypeLabel } from "../utils/labels";
import { StatusPill } from "../components/StatusPill";

export function MediaPreviewTable({ rows }: { rows: MediaFile[] }) {
  return (
    <div className="tableFrame">
      <table className="dataTable previewTable">
        <thead>
          <tr>
            <th>档案</th>
            <th>状态</th>
            <th>参数</th>
            <th>路径</th>
            <th>时间</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((file) => (
            <tr key={file.file_id}>
              <td>
                <div className="mediaName">
                  <b>{file.final_file_name || file.original_file_name}</b>
                  <small>
                    {[
                      mediaTypeLabel(file.media_type),
                      file.parse_title,
                      file.season || file.episode
                        ? `S${twoDigit(file.season)}E${twoDigit(file.episode)}`
                        : "",
                    ]
                      .filter(Boolean)
                      .join(" · ")}
                  </small>
                </div>
              </td>
              <td>
                <StatusPill value={file.process_status} />
              </td>
              <td>
                <div className="techTags">
                  {techTags(file).slice(0, 3).map((tag) => (
                    <span key={tag}>{tag}</span>
                  ))}
                </div>
              </td>
              <td title={file.final_path || file.planned_target || file.current_path}>
                <span className="pathText">
                  {shortPath(file.final_path || file.planned_target || file.current_path)}
                </span>
              </td>
              <td>{formatDate(file.updated_at)}</td>
            </tr>
          ))}
          {rows.length === 0 && (
            <tr>
              <td className="emptyCell" colSpan={5}>
                暂无活动
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

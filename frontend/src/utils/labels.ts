export type StatusTone =
  | "success"
  | "running"
  | "warning"
  | "danger"
  | "info"
  | "neutral";

export function statusLabel(value: string) {
  const labels: Record<string, string> = {
    idle: "空闲",
    queued: "排队中",
    running: "运行中",
    cancelling: "停止中",
    cancelled: "已停止",
    interrupted: "已中断",
    incoming: "入库中",
    scanned: "已扫描",
    parsed: "已解析",
    scraped: "已识别",
    matched: "已匹配",
    collection_checked: "已检查",
    planned: "已规划",
    moved: "已移动",
    done: "已完成",
    complete: "完成",
    failed: "失败",
    incomplete: "缺失",
    incomplete_collection: "缺失合集",
    ok: "正常",
    success: "成功",
    partial: "部分失败",
    unknown: "未知",
  };
  return labels[value] ?? value;
}

export function statusTone(value: string): StatusTone {
  const raw = value || "unknown";
  if (["done", "complete", "ok", "success"].includes(raw)) return "success";
  if (
    [
      "running",
      "queued",
      "incoming",
      "scanned",
      "parsed",
      "scraped",
      "matched",
      "collection_checked",
      "planned",
      "moved",
    ].includes(raw)
  ) {
    return "running";
  }
  if (["partial", "incomplete", "incomplete_collection"].includes(raw)) {
    return "warning";
  }
  if (
    ["failed", "cancelled", "interrupted"].includes(raw) ||
    raw.toLowerCase().includes("failed")
  ) {
    return "danger";
  }
  if (["playback", "p115_sync", "ai_filename"].includes(raw)) return "info";
  return "neutral";
}

export function logTypeLabel(value: string) {
  const labels: Record<string, string> = {
    ai_filename: "AI 识别",
    playback: "播放",
    p115_sync: "STRM",
    operation: "整理操作",
    organize_task: "整理任务",
    collection_completion: "合集补齐",
    scan_batch: "扫描批次",
  };
  return labels[value] ?? value;
}

export function mediaTypeLabel(value: string) {
  const labels: Record<string, string> = {
    movie: "电影",
    tv_episode: "剧集",
    collection_movie: "合集电影",
  };
  return labels[value] ?? value;
}

export function errorCodeLabel(value: string) {
  if (!value) return "";
  const labels: Record<string, string> = {
    UNSUPPORTED_EXTENSION: "扩展名不支持",
    FILE_TOO_SMALL: "文件过小",
    FILE_NOT_READABLE: "文件不可读",
    FILE_HASH_FAILED: "哈希失败",
    PARSE_TITLE_EMPTY: "标题为空",
    PARSE_YEAR_EMPTY: "年份为空",
    PARSE_SEASON_EMPTY: "季号为空",
    PARSE_EPISODE_EMPTY: "集号为空",
    SCRAPE_EMPTY_RESULT: "无搜索结果",
    SCRAPE_REQUEST_FAILED: "识别请求失败",
    MATCH_NOT_FOUND: "未找到匹配",
    MATCH_NOT_UNIQUE: "匹配不唯一",
    TV_EPISODE_NOT_FOUND: "剧集分集不存在",
    COLLECTION_FETCH_FAILED: "合集获取失败",
    COLLECTION_CHECK_FAILED: "合集检查失败",
    TEMPLATE_NOT_FOUND: "模板不存在",
    TEMPLATE_FIELD_INVALID: "模板字段无效",
    TEMPLATE_PATH_ESCAPE: "模板路径越界",
    TARGET_PATH_EXISTS: "目标已存在",
    TARGET_DIR_CREATE_FAILED: "目录创建失败",
    MOVE_TO_STAGING_FAILED: "移动到整理目录失败",
    MOVE_TO_FAILED_DIR_FAILED: "移动到失败目录失败",
    MOVE_TO_INCOMPLETE_COLLECTION_FAILED: "移动到缺失合集失败",
    COLLECTION_COMPLETE_MOVE_FAILED: "合集迁移失败",
    CLOUDDRIVE_REQUEST_FAILED: "云端请求失败",
    DATABASE_WRITE_FAILED: "数据库写入失败",
    SUBTITLE_MOVE_FAILED: "字幕移动失败",
    MEDIA_PROBE_FAILED: "媒体参数读取失败",
  };
  return labels[value] ?? value;
}

export function sourceLabel(value: string) {
  const labels: Record<string, string> = {
    local: "本地",
    cloud: "云端",
  };
  return labels[value] ?? (value || "本地");
}

export function queueLabel(value: string) {
  const labels: Record<string, string> = {
    "queue:scan": "扫描",
    "queue:parse": "解析",
    "queue:scrape": "识别",
    "queue:match": "匹配",
    "queue:collection_check": "合集",
    "queue:organize": "移动",
    "queue:failed": "失败",
  };
  return labels[value] ?? value.replace("queue:", "");
}

export function p115TriggerLabel(value: string) {
  const labels: Record<string, string> = {
    manual_export: "手动快照",
    manual_sync: "手动同步",
    manual_cleanup: "手动清理",
    manual_rebuild_nodes: "重建 STRM",
    cron: "定时同步",
  };
  return labels[value] ?? value;
}

export function p115ModeLabel(value: string) {
  const labels: Record<string, string> = {
    refresh: "快照",
    events: "事件",
    events_parent_scan: "事件+局部扫描",
    scan: "扫描",
    export: "目录树",
    export_nodes: "目录树+节点",
    rebuild_nodes: "重建 STRM",
    cache: "缓存",
    snapshot: "快照",
    cleanup: "清理",
    sync: "同步",
  };
  return labels[value] ?? (value || "-");
}

export const templateFieldDocs = [
  {
    field: "{title}",
    group: "基础",
    name: "电影标题",
    description: "电影名称，优先使用简体中文，缺失时回退英文。",
  },
  {
    field: "{year}",
    group: "基础",
    name: "电影年份",
    description: "电影上映年份，用于区分同名影片。",
  },
  {
    field: "{category}",
    group: "基础",
    name: "分类目录",
    description: "分类 YAML 匹配到的目录名，例如 欧美电影、国产剧集。",
  },
  {
    field: "{extension}",
    group: "基础",
    name: "文件扩展名",
    description: "原始媒体文件扩展名，模板必须包含该字段。",
  },
  {
    field: "{resolution}",
    group: "技术",
    name: "分辨率",
    description: "ffprobe 读取到的真实视频分辨率，例如 2160p、1080p。",
  },
  {
    field: "{source}",
    group: "技术",
    name: "片源类型",
    description: "从文件名识别出的 BluRay、WEB-DL、UHD、Remux 等来源。",
  },
  {
    field: "{video_codec}",
    group: "技术",
    name: "视频编码",
    description: "ffprobe 读取到的视频编码，例如 HEVC、AVC、AV1。",
  },
  {
    field: "{audio_codec}",
    group: "技术",
    name: "音频编码",
    description: "ffprobe 读取到的主音轨编码，例如 TrueHD、DTS-HD MA、DDP。",
  },
  {
    field: "{audio_channels}",
    group: "技术",
    name: "声道",
    description: "ffprobe 读取到的主音轨声道，例如 7.1、5.1、2.0。",
  },
  {
    field: "{hdr_format}",
    group: "技术",
    name: "HDR 格式",
    description: "ffprobe 读取到的 HDR 信息，例如 DV、HDR10+、HDR10、HLG、SDR。",
  },
  {
    field: "{show_title}",
    group: "剧集",
    name: "剧集标题",
    description: "剧集名称，优先使用简体中文，缺失时回退英文。",
  },
  { field: "{show_year}", group: "剧集", name: "首播年份", description: "剧集首播年份。" },
  { field: "{season}", group: "剧集", name: "季号", description: "不补零的季号，例如 1。" },
  {
    field: "{season_2}",
    group: "剧集",
    name: "两位季号",
    description: "补零后的季号，例如 01。",
  },
  { field: "{episode}", group: "剧集", name: "集号", description: "不补零的集号，例如 3。" },
  {
    field: "{episode_2}",
    group: "剧集",
    name: "两位集号",
    description: "补零后的集号，例如 03。",
  },
  {
    field: "{episode_title}",
    group: "剧集",
    name: "分集标题",
    description: "TMDB 返回的单集标题。",
  },
  {
    field: "{collection_name}",
    group: "合集",
    name: "合集名称",
    description: "TMDB 合集名称。",
  },
  { field: "{collection_id}", group: "合集", name: "合集 ID", description: "TMDB 合集 ID。" },
];

export const templateFields = templateFieldDocs.map((item) => item.field);

export const templateLabels: Record<string, string> = {
  movie: "电影",
  tv_episode: "剧集",
  collection_movie: "完整合集",
  incomplete_collection_movie: "缺失合集",
};

export async function copyText(value: string) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "true");
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.select();
  const ok = document.execCommand("copy");
  document.body.removeChild(textarea);
  if (!ok) throw new Error("copy failed");
}

export function fieldDeleteRange(
  value: string,
  caret: number,
  direction: "backward" | "forward",
) {
  for (const field of templateFields) {
    let start = value.indexOf(field);
    while (start !== -1) {
      const end = start + field.length;
      const shouldDelete =
        direction === "backward"
          ? caret > start && caret <= end
          : caret >= start && caret < end;
      if (shouldDelete) return { start, end };
      start = value.indexOf(field, end);
    }
  }
  return null;
}

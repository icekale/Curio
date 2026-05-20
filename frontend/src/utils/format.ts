import type { MediaFile } from "../types";

export function arrayOrEmpty<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

export function twoDigit(value: number) {
  return String(value || 0).padStart(2, "0");
}

export function formatSize(value: number) {
  if (!value) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const index = Math.min(
    Math.floor(Math.log(value) / Math.log(1024)),
    units.length - 1,
  );
  return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`;
}

export function formatDate(value: string) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

export function formatFullDate(value: string) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(date);
}

export function formatDuration(value: number) {
  if (!value) return "-";
  if (value < 1000) return `${value} ms`;
  const seconds = Math.round(value / 1000);
  if (seconds < 60) return `${seconds} 秒`;
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  return rest ? `${minutes} 分 ${rest} 秒` : `${minutes} 分`;
}

export function shortPath(value: string) {
  if (!value) return "";
  return value.length > 62 ? `...${value.slice(-59)}` : value;
}

export function shortText(value: string, max = 58) {
  if (!value) return "";
  return value.length > max ? `${value.slice(0, max - 3)}...` : value;
}

export function prettyJSON(value: string) {
  if (!value) return "";
  try {
    return JSON.stringify(JSON.parse(value), null, 2);
  } catch {
    return value;
  }
}

export function techTags(file: MediaFile) {
  return [
    file.resolution,
    file.video_codec,
    file.hdr_format,
    file.audio_codec,
    file.audio_channels,
  ].filter((item) => item && item !== "Unknown");
}

export function taskDone(totalDone: {
  done?: number;
  failed?: number;
  incomplete_collection?: number;
}) {
  return (
    (totalDone.done ?? 0) +
    (totalDone.failed ?? 0) +
    (totalDone.incomplete_collection ?? 0)
  );
}

export function percent(done: number, total: number) {
  return total ? Math.min(100, Math.round((done / total) * 100)) : 0;
}

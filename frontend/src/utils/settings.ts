import type { CloudDriveSettings, DirectoryConfig, P115Settings, SystemSettings } from "../types";

export const emptyDirs: DirectoryConfig = {
  incoming_path: "",
  staging_path: "",
  failed_path: "",
  incomplete_collections_path: "",
};

export const emptySettings: SystemSettings = {
  tmdb_api_key: "",
  network_proxy: "",
  classification_yaml: "",
  ai_filename_enabled: false,
  ai_filename_force: false,
  ai_base_url: "https://api.openai.com/v1",
  ai_api_key: "",
  ai_model: "gpt-5.5",
  ai_filename_prompt: "",
  updated_at: "",
};

export const emptyCloudDrive: CloudDriveSettings = {
  address: "http://localhost:19798",
  username: "",
  password: "",
  token: "",
  root_path: "/",
  staging_path: "/Curio/staging",
  failed_path: "/Curio/failed",
  incomplete_collections_path: "/Curio/incomplete_collections",
  updated_at: "",
};

export const defaultSTRMOutputPath = "/data/Curio/strm";

export const emptyP115: P115Settings = {
  enabled: true,
  app_id: "",
  app_secret: "",
  cookies: "",
  cookie_login_app: "wechatmini",
  strm_output_path: defaultSTRMOutputPath,
  public_base_url: "",
  library_cid: "",
  delete_missing_strm: true,
  stale_before_delete: false,
  refresh_emby_after_sync: false,
  sync_cron_enabled: false,
  sync_interval_minutes: 60,
  emby_upstream_url: "",
  emby_public_url: "",
  emby_proxy_port: 8097,
  emby_proxy_base_path: "/emby",
  emby_api_key: "",
  updated_at: "",
};

export const p115CookieLoginApps = [
  ["wechatmini", "微信小程序"],
  ["alipaymini", "支付宝小程序"],
  ["android", "安卓端"],
  ["ios", "苹果端"],
  ["web", "网页端"],
  ["qandroid", "管理安卓端"],
  ["qios", "管理苹果端"],
] as const;

export type ProxyScheme = "" | "http" | "https" | "socks5" | "socks5h" | "sock5";

export type NetworkProxyDraft = {
  scheme: ProxyScheme;
  host: string;
  port: string;
  username: string;
  password: string;
};

export const proxySchemeOptions: { value: ProxyScheme; label: string }[] = [
  { value: "", label: "不使用" },
  { value: "http", label: "HTTP" },
  { value: "https", label: "HTTPS" },
  { value: "socks5", label: "SOCKS5" },
  { value: "socks5h", label: "SOCKS5H" },
  { value: "sock5", label: "SOCK5" },
];

export function emptyNetworkProxyDraft(): NetworkProxyDraft {
  return { scheme: "", host: "", port: "", username: "", password: "" };
}

export function parseNetworkProxy(value: string): NetworkProxyDraft {
  const raw = value.trim();
  if (!raw) return emptyNetworkProxyDraft();
  try {
    const parsed = new URL(raw);
    const scheme = parsed.protocol.replace(":", "") as ProxyScheme;
    if (!proxySchemeOptions.some((item) => item.value === scheme)) {
      return emptyNetworkProxyDraft();
    }
    return {
      scheme,
      host: parsed.hostname,
      port: parsed.port,
      username: decodeURIComponent(parsed.username),
      password: decodeURIComponent(parsed.password),
    };
  } catch {
    return emptyNetworkProxyDraft();
  }
}

export function buildNetworkProxy(draft: NetworkProxyDraft): string {
  const scheme = draft.scheme.trim();
  const host = draft.host.trim();
  if (!scheme || !host) return "";
  const port = draft.port.trim();
  const username = draft.username.trim();
  const password = draft.password;
  const auth =
    username || password
      ? `${encodeURIComponent(username)}${password ? `:${encodeURIComponent(password)}` : ""}@`
      : "";
  return `${scheme}://${auth}${host}${port ? `:${port}` : ""}`;
}

export type P115LibraryOutputRow = {
  cid: string;
  outputPath: string;
};

export function splitP115CIDValues(value: string) {
  return value
    .split(/[\s,，;；]+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function splitP115OutputPaths(value: string) {
  return value
    .split(/\r?\n/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function p115LibraryOutputRows(settings: P115Settings): P115LibraryOutputRow[] {
  const cids = splitP115CIDValues(settings.library_cid);
  const outputPaths = splitP115OutputPaths(settings.strm_output_path);
  const rowCount = Math.max(cids.length, outputPaths.length, 1);
  return Array.from({ length: rowCount }, (_, index) => ({
    cid: cids[index] ?? "",
    outputPath: outputPaths[index] ?? outputPaths[0] ?? defaultSTRMOutputPath,
  }));
}

export function nextP115OutputPath(rows: P115LibraryOutputRow[]) {
  const lastPath =
    [...rows].reverse().find((row) => row.outputPath.trim())?.outputPath.trim() ??
    defaultSTRMOutputPath;
  const match = lastPath.match(/^(.*?)(\d+)$/);
  if (!match) return `${lastPath}2`;
  return `${match[1]}${Number.parseInt(match[2], 10) + 1}`;
}

export function settingsWithP115LibraryOutputRows(
  settings: P115Settings,
  rows: P115LibraryOutputRow[],
): P115Settings {
  const keptRows = rows.length > 0 ? rows : [{ cid: "", outputPath: defaultSTRMOutputPath }];
  const libraryCID = keptRows
    .map((row) => row.cid.trim())
    .filter(Boolean)
    .join("\n");
  const outputPath = keptRows
    .map((row) => row.outputPath.trim())
    .filter(Boolean)
    .join("\n");
  return {
    ...settings,
    library_cid: libraryCID,
    strm_output_path: outputPath || defaultSTRMOutputPath,
  };
}

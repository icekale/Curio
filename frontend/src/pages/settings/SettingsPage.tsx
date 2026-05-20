import { useEffect, useMemo, useRef, useState } from "react";
import {
  BadgeCheck,
  CheckCircle2,
  Clock3,
  CloudCheck,
  CloudCog,
  DatabaseZap,
  FileSymlink,
  FolderInput,
  FolderOpen,
  HardDrive,
  HardDriveDownload,
  Import,
  LogIn,
  Plus,
  PlugZap,
  Router,
  Save,
  ScanQrCode,
  ScanSearch,
  Server,
  ServerCog,
  ShieldCheck,
  Trash2,
} from "lucide-react";
import { Card } from "../../components/Card";
import { SecretInput } from "../../components/SecretInput";
import type { CurioConsole } from "../../hooks/useCurioConsole";
import type { CloudDriveFile, P115SyncRun, STRMPreview } from "../../types";
import { formatDuration, formatFullDate, formatSize, shortPath, shortText } from "../../utils/format";
import { p115ModeLabel, p115TriggerLabel, statusLabel, statusTone } from "../../utils/labels";
import {
  buildNetworkProxy,
  defaultSTRMOutputPath,
  nextP115OutputPath,
  p115CookieLoginApps,
  p115LibraryOutputRows,
  parseNetworkProxy,
  proxySchemeOptions,
  settingsWithP115LibraryOutputRows,
  type NetworkProxyDraft,
} from "../../utils/settings";
import { StatusPill } from "../../components/StatusPill";

type SettingsTab = "base" | "scraper" | "network" | "ai" | "cloud" | "p115" | "emby";

const settingsTabs = [
  { id: "base", label: "本地目录", summary: "入库与整理目录", icon: HardDrive },
  { id: "scraper", label: "刮削源", summary: "TMDB", icon: DatabaseZap },
  { id: "network", label: "网络代理", summary: "HTTP、HTTPS、SOCKS5", icon: Router },
  { id: "ai", label: "AI", summary: "文件名识别", icon: PlugZap },
  { id: "cloud", label: "云端", summary: "CloudDrive2 与浏览", icon: CloudCog },
  { id: "p115", label: "115", summary: "STRM、授权、CID", icon: ShieldCheck },
  { id: "emby", label: "Emby", summary: "反代与媒体服务器", icon: ServerCog },
] satisfies {
  id: SettingsTab;
  label: string;
  summary: string;
  icon: typeof HardDrive;
}[];

export function SettingsPage({ console }: { console: CurioConsole }) {
  const [visibleSecrets, setVisibleSecrets] = useState<Record<string, boolean>>({});
  const [settingsTab, setSettingsTab] = useState<SettingsTab>("base");
  const [networkProxyDraft, setNetworkProxyDraft] = useState<NetworkProxyDraft>(() =>
    parseNetworkProxy(console.systemSettings.network_proxy),
  );
  const lastNetworkProxy = useRef(console.systemSettings.network_proxy);
  const secretVisible = (key: string) => Boolean(visibleSecrets[key]);
  const toggleSecret = (key: string) =>
    setVisibleSecrets((current) => ({ ...current, [key]: !current[key] }));

  useEffect(() => {
    if (lastNetworkProxy.current === console.systemSettings.network_proxy) return;
    lastNetworkProxy.current = console.systemSettings.network_proxy;
    setNetworkProxyDraft(parseNetworkProxy(console.systemSettings.network_proxy));
  }, [console.systemSettings.network_proxy]);

  const updateNetworkProxy = (patch: Partial<NetworkProxyDraft>) => {
    setNetworkProxyDraft((current) => {
      const next = { ...current, ...patch };
      const networkProxy = buildNetworkProxy(next);
      lastNetworkProxy.current = networkProxy;
      console.setSystemSettings({ ...console.systemSettings, network_proxy: networkProxy });
      return next;
    });
  };

  return (
    <section className="settingsLayout">
      <div className="settingsTabs" role="tablist" aria-label="设置分类">
        {settingsTabs.map((item) => {
          const Icon = item.icon;
          return (
            <button
              className={settingsTab === item.id ? "settingsTab active" : "settingsTab"}
              key={item.id}
              onClick={() => setSettingsTab(item.id)}
              role="tab"
              aria-selected={settingsTab === item.id}
              type="button"
            >
              <Icon size={17} />
              <span>{item.label}</span>
              <small>{item.summary}</small>
            </button>
          );
        })}
      </div>

      <div className="settingsContent">
        {settingsTab === "base" && <BaseSettings console={console} />}
        {settingsTab === "scraper" && (
          <ScraperSettings console={console} secretVisible={secretVisible} toggleSecret={toggleSecret} />
        )}
        {settingsTab === "network" && (
          <NetworkSettings
            console={console}
            draft={networkProxyDraft}
            updateDraft={updateNetworkProxy}
            secretVisible={secretVisible}
            toggleSecret={toggleSecret}
          />
        )}
        {settingsTab === "ai" && (
          <AISettings console={console} secretVisible={secretVisible} toggleSecret={toggleSecret} />
        )}
        {settingsTab === "cloud" && (
          <CloudSettings console={console} secretVisible={secretVisible} toggleSecret={toggleSecret} />
        )}
        {settingsTab === "p115" && (
          <P115SettingsPanel console={console} secretVisible={secretVisible} toggleSecret={toggleSecret} />
        )}
        {settingsTab === "emby" && (
          <EmbySettings console={console} secretVisible={secretVisible} toggleSecret={toggleSecret} />
        )}
      </div>
    </section>
  );
}

function BaseSettings({ console }: { console: CurioConsole }) {
  const dirFields = [
    ["incoming_path", "入库目录"],
    ["staging_path", "整理目录"],
    ["failed_path", "失败目录"],
    ["incomplete_collections_path", "缺失合集目录"],
  ] as const;
  return (
    <Card title="本地目录" eyebrow="Local Archive">
      <p className="cardIntro">定义 Curio 从哪里读取媒体、把成功和失败文件放到哪里。</p>
      <div className="formGrid">
        {dirFields.map(([key, label]) => (
          <label className="field" key={key}>
            <span>{label}</span>
            <input
              value={console.directories[key]}
              onChange={(event) =>
                console.setDirectories({
                  ...console.directories,
                  [key]: event.target.value,
                })
              }
            />
          </label>
        ))}
      </div>
      <div className="settingsActions">
        <button
          className="primaryButton"
          onClick={console.saveDirectories}
          disabled={console.busy}
          type="button"
        >
          <FolderInput size={17} />
          <span>保存本地目录</span>
        </button>
      </div>
    </Card>
  );
}

function ScraperSettings({
  console,
  secretVisible,
  toggleSecret,
}: {
  console: CurioConsole;
  secretVisible: (key: string) => boolean;
  toggleSecret: (key: string) => void;
}) {
  return (
    <Card title="刮削源" eyebrow="TMDB">
      <p className="cardIntro">配置 TMDB API 密钥，用于电影、剧集与合集元数据识别。</p>
      <div className="formGrid">
        <label className="field">
          <span>TMDB API 密钥</span>
          <SecretInput
            value={console.systemSettings.tmdb_api_key}
            visible={secretVisible("tmdb_api_key")}
            onToggle={() => toggleSecret("tmdb_api_key")}
            onChange={(value) =>
              console.setSystemSettings({
                ...console.systemSettings,
                tmdb_api_key: value,
              })
            }
          />
        </label>
      </div>
      <div className="settingsActions">
        <button
          className="primaryButton"
          onClick={console.saveSystemSettings}
          disabled={console.busy}
          type="button"
        >
          <DatabaseZap size={17} />
          <span>保存刮削源配置</span>
        </button>
      </div>
    </Card>
  );
}

function NetworkSettings({
  console,
  draft,
  updateDraft,
  secretVisible,
  toggleSecret,
}: {
  console: CurioConsole;
  draft: NetworkProxyDraft;
  updateDraft: (patch: Partial<NetworkProxyDraft>) => void;
  secretVisible: (key: string) => boolean;
  toggleSecret: (key: string) => void;
}) {
  return (
    <Card title="网络代理" eyebrow="Proxy">
      <p className="cardIntro">用于刮削、AI 文件名识别等需要访问外部接口的请求。</p>
      <div className="formGrid proxyGrid">
        <label className="field">
          <span>协议</span>
          <select
            value={draft.scheme}
            onChange={(event) => updateDraft({ scheme: event.target.value as NetworkProxyDraft["scheme"] })}
          >
            {proxySchemeOptions.map((item) => (
              <option key={item.value || "none"} value={item.value}>
                {item.label}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          <span>主机</span>
          <input
            value={draft.host}
            disabled={!draft.scheme}
            placeholder="127.0.0.1"
            onChange={(event) => updateDraft({ host: event.target.value })}
          />
        </label>
        <label className="field">
          <span>端口</span>
          <input
            value={draft.port}
            disabled={!draft.scheme}
            inputMode="numeric"
            placeholder="7890"
            onChange={(event) => updateDraft({ port: event.target.value })}
          />
        </label>
        <label className="field">
          <span>用户名</span>
          <input
            value={draft.username}
            disabled={!draft.scheme}
            autoComplete="off"
            onChange={(event) => updateDraft({ username: event.target.value })}
          />
        </label>
        <label className="field">
          <span>密码</span>
          <SecretInput
            value={draft.password}
            disabled={!draft.scheme}
            visible={secretVisible("network_proxy_password")}
            onToggle={() => toggleSecret("network_proxy_password")}
            onChange={(value) => updateDraft({ password: value })}
          />
        </label>
      </div>
      <div className="settingsActions">
        <button
          className="primaryButton"
          onClick={console.saveSystemSettings}
          disabled={console.busy}
          type="button"
        >
          <Router size={17} />
          <span>保存代理配置</span>
        </button>
      </div>
    </Card>
  );
}

function AISettings({
  console,
  secretVisible,
  toggleSecret,
}: {
  console: CurioConsole;
  secretVisible: (key: string) => boolean;
  toggleSecret: (key: string) => void;
}) {
  return (
    <Card title="AI 文件名识别" eyebrow="Filename Intelligence">
      <p className="cardIntro">当普通解析无法稳定识别文件名时，可启用 AI 辅助提取标题、年份和季集。</p>
      <div className="formGrid">
        <div className="checkGroup">
          <label className="checkLine">
            <input
              checked={console.systemSettings.ai_filename_enabled}
              type="checkbox"
              onChange={(event) =>
                console.setSystemSettings({
                  ...console.systemSettings,
                  ai_filename_enabled: event.target.checked,
                })
              }
            />
            <span>AI 文件名识别</span>
          </label>
          <label className="checkLine">
            <input
              checked={console.systemSettings.ai_filename_force}
              type="checkbox"
              onChange={(event) =>
                console.setSystemSettings({
                  ...console.systemSettings,
                  ai_filename_force: event.target.checked,
                })
              }
            />
            <span>强制使用 AI</span>
          </label>
        </div>
        <label className="field">
          <span>AI 接口地址</span>
          <input
            value={console.systemSettings.ai_base_url}
            placeholder="https://api.openai.com/v1"
            onChange={(event) =>
              console.setSystemSettings({
                ...console.systemSettings,
                ai_base_url: event.target.value,
              })
            }
          />
        </label>
        <label className="field">
          <span>AI 模型</span>
          <input
            value={console.systemSettings.ai_model}
            placeholder="gpt-5.5"
            onChange={(event) =>
              console.setSystemSettings({
                ...console.systemSettings,
                ai_model: event.target.value,
              })
            }
          />
        </label>
        <label className="field">
          <span>AI API Key</span>
          <SecretInput
            value={console.systemSettings.ai_api_key}
            visible={secretVisible("ai_api_key")}
            onToggle={() => toggleSecret("ai_api_key")}
            onChange={(value) =>
              console.setSystemSettings({
                ...console.systemSettings,
                ai_api_key: value,
              })
            }
          />
        </label>
        <label className="field wideField">
          <span>AI 文件名识别提示词</span>
          <textarea
            className="promptTextarea"
            value={console.systemSettings.ai_filename_prompt}
            onChange={(event) =>
              console.setSystemSettings({
                ...console.systemSettings,
                ai_filename_prompt: event.target.value,
              })
            }
          />
        </label>
      </div>
      <div className="settingsActions">
        <button
          className="primaryButton"
          onClick={console.saveSystemSettings}
          disabled={console.busy}
          type="button"
        >
          <PlugZap size={17} />
          <span>保存 AI 配置</span>
        </button>
      </div>
    </Card>
  );
}

function CloudSettings({
  console,
  secretVisible,
  toggleSecret,
}: {
  console: CurioConsole;
  secretVisible: (key: string) => boolean;
  toggleSecret: (key: string) => void;
}) {
  const cloudFields = [
    ["address", "服务地址", "http://host.docker.internal:19798"],
    ["username", "用户名", ""],
    ["password", "密码", ""],
    ["token", "访问令牌", ""],
    ["root_path", "扫描根目录", "/"],
    ["staging_path", "整理目录", "/Curio/staging"],
    ["failed_path", "失败目录", "/Curio/failed"],
    ["incomplete_collections_path", "缺失合集目录", "/Curio/incomplete_collections"],
  ] as const;

  return (
    <>
      <Card title="CloudDrive2" eyebrow="Cloud Connector">
        <p className="cardIntro">连接 CloudDrive2 后，可以直接整理云端挂载文件。</p>
        <div className="formGrid">
          {cloudFields.map(([key, label, placeholder]) => (
            <label className="field" key={key}>
              <span>{label}</span>
              {key === "password" || key === "token" ? (
                <SecretInput
                  value={String(console.cloudDriveSettings[key] ?? "")}
                  placeholder={placeholder}
                  visible={secretVisible(`cloud_${key}`)}
                  onToggle={() => toggleSecret(`cloud_${key}`)}
                  onChange={(value) =>
                    console.setCloudDriveSettings({
                      ...console.cloudDriveSettings,
                      [key]: value,
                    })
                  }
                />
              ) : (
                <input
                  value={String(console.cloudDriveSettings[key] ?? "")}
                  placeholder={placeholder}
                  onChange={(event) =>
                    console.setCloudDriveSettings({
                      ...console.cloudDriveSettings,
                      [key]: event.target.value,
                    })
                  }
                />
              )}
            </label>
          ))}
        </div>
        <div className="settingsActions">
          <button className="primaryButton" onClick={console.saveCloudDrive} disabled={console.busy} type="button">
            <CloudCheck size={17} />
            <span>保存云端配置</span>
          </button>
          <button className="secondaryButton" onClick={console.testCloudDrive} disabled={console.busy} type="button">
            <PlugZap size={17} />
            <span>测试连接</span>
          </button>
          <button className="secondaryButton" onClick={console.startCloudDriveScan} disabled={console.busy} type="button">
            <HardDriveDownload size={17} />
            <span>整理云端</span>
          </button>
        </div>
      </Card>

      <Card title="云端浏览" eyebrow="Cloud Browser">
        <div className="browserBar">
          <input
            value={console.cloudDrivePath}
            onChange={(event) => console.setCloudDrivePath(event.target.value)}
          />
          <button
            className="primaryButton"
            onClick={() => console.openCloudDrivePath(console.cloudDrivePath)}
            disabled={console.busy}
            type="button"
          >
            <ScanSearch size={17} />
            <span>打开</span>
          </button>
        </div>
        <CloudDriveTable rows={console.cloudDriveFiles} onOpen={console.openCloudDrivePath} />
      </Card>
    </>
  );
}

function P115SettingsPanel({
  console,
  secretVisible,
  toggleSecret,
}: {
  console: CurioConsole;
  secretVisible: (key: string) => boolean;
  toggleSecret: (key: string) => void;
}) {
  const libraryOutputRows = useMemo(
    () => p115LibraryOutputRows(console.p115Settings),
    [console.p115Settings],
  );
  const updateLibraryOutputRows = (rows: ReturnType<typeof p115LibraryOutputRows>) =>
    console.setP115Settings(
      settingsWithP115LibraryOutputRows(console.p115Settings, rows),
    );
  const updateLibraryOutputRow = (
    index: number,
    patch: Partial<ReturnType<typeof p115LibraryOutputRows>[number]>,
  ) => {
    updateLibraryOutputRows(
      libraryOutputRows.map((row, rowIndex) =>
        rowIndex === index ? { ...row, ...patch } : row,
      ),
    );
  };

  return (
    <>
      <Card title="115 STRM" eyebrow="STRM Output">
        <p className="cardIntro">配置媒体库 CID 与 STRM 输出目录，支持多个媒体库并行生成。</p>
        <div className="formGrid">
          <label className="field wideField">
            <span>STRM 生成地址</span>
            <input
              value={console.p115Settings.public_base_url}
              placeholder="http://172.16.0.1:8080"
              onChange={(event) =>
                console.setP115Settings({
                  ...console.p115Settings,
                  public_base_url: event.target.value,
                })
              }
            />
          </label>
          <div className="libraryOutputEditor">
            <div className="libraryOutputHeader">
              <span>媒体库 CID</span>
              <span>STRM 输出目录</span>
              <span>操作</span>
            </div>
            {libraryOutputRows.map((row, index) => (
              <div className="libraryOutputRow" key={index}>
                <label className="field">
                  <span>媒体库 CID {index + 1}</span>
                  <input
                    value={row.cid}
                    placeholder="3429318291990438503"
                    onChange={(event) =>
                      updateLibraryOutputRow(index, { cid: event.target.value })
                    }
                  />
                </label>
                <label className="field">
                  <span>STRM 输出目录 {index + 1}</span>
                  <input
                    value={row.outputPath}
                    placeholder={defaultSTRMOutputPath}
                    onChange={(event) =>
                      updateLibraryOutputRow(index, { outputPath: event.target.value })
                    }
                  />
                </label>
                <button
                  className="iconButton dangerIcon"
                  onClick={() =>
                    updateLibraryOutputRows(
                      libraryOutputRows.filter((_, rowIndex) => rowIndex !== index),
                    )
                  }
                  disabled={console.busy || libraryOutputRows.length <= 1}
                  title="删除媒体库"
                  type="button"
                >
                  <Trash2 size={17} />
                </button>
              </div>
            ))}
            <button
              className="secondaryButton libraryOutputAdd"
              onClick={() =>
                updateLibraryOutputRows([
                  ...libraryOutputRows,
                  { cid: "", outputPath: nextP115OutputPath(libraryOutputRows) },
                ])
              }
              disabled={console.busy}
              type="button"
            >
              <Plus size={17} />
              <span>添加媒体库</span>
            </button>
          </div>
          <div className="checkGroup">
            <CheckLine
              label="同步时删除缺失 STRM"
              checked={console.p115Settings.delete_missing_strm}
              onChange={(checked) =>
                console.setP115Settings({
                  ...console.p115Settings,
                  delete_missing_strm: checked,
                })
              }
            />
            <CheckLine
              label="删除前先标记失效"
              checked={console.p115Settings.stale_before_delete}
              onChange={(checked) =>
                console.setP115Settings({
                  ...console.p115Settings,
                  stale_before_delete: checked,
                })
              }
            />
            <CheckLine
              label="同步后刷新 Emby"
              checked={console.p115Settings.refresh_emby_after_sync}
              onChange={(checked) =>
                console.setP115Settings({
                  ...console.p115Settings,
                  refresh_emby_after_sync: checked,
                })
              }
            />
          </div>
        </div>
        <div className="settingsActions">
          <button className="primaryButton" onClick={console.saveP115} disabled={console.busy} type="button">
            <FileSymlink size={17} />
            <span>保存 STRM 配置</span>
          </button>
        </div>
      </Card>

      <Card title="登录授权" eyebrow="115 Authorization">
        <div className="formGrid">
          <label className="field">
            <span>App ID</span>
            <input
              value={console.p115Settings.app_id}
              onChange={(event) =>
                console.setP115Settings({
                  ...console.p115Settings,
                  app_id: event.target.value,
                })
              }
            />
          </label>
          <label className="field">
            <span>App Secret</span>
            <SecretInput
              value={console.p115Settings.app_secret}
              visible={secretVisible("p115_app_secret")}
              onToggle={() => toggleSecret("p115_app_secret")}
              onChange={(value) =>
                console.setP115Settings({ ...console.p115Settings, app_secret: value })
              }
            />
          </label>
          <label className="field wideField">
            <span>Cookies</span>
            <SecretInput
              value={console.p115Settings.cookies}
              placeholder="UID=...; CID=...; SEID=..."
              visible={secretVisible("p115_cookies")}
              onToggle={() => toggleSecret("p115_cookies")}
              onChange={(value) =>
                console.setP115Settings({ ...console.p115Settings, cookies: value })
              }
            />
          </label>
          <label className="field">
            <span>扫码设备</span>
            <select
              value={console.p115Settings.cookie_login_app || "wechatmini"}
              onChange={(event) =>
                console.setP115Settings({
                  ...console.p115Settings,
                  cookie_login_app: event.target.value,
                })
              }
            >
              {p115CookieLoginApps.map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </select>
          </label>
        </div>
        <div className="settingsActions">
          <button className="primaryButton" onClick={console.saveP115} disabled={console.busy} type="button">
            <Save size={17} />
            <span>保存授权</span>
          </button>
          <button className="secondaryButton" onClick={console.testP115} disabled={console.busy} type="button">
            <Router size={17} />
            <span>测试连接</span>
          </button>
          <button className="secondaryButton" onClick={console.startP115QRCode} disabled={console.busy} type="button">
            <ScanQrCode size={17} />
            <span>扫码获取 Cookies</span>
          </button>
          <button className="secondaryButton" onClick={console.startP115OAuth} disabled={console.busy} type="button">
            <LogIn size={17} />
            <span>OAuth 登录</span>
          </button>
          <button className="secondaryButton" onClick={console.refreshP115Token} disabled={console.busy} type="button">
            <BadgeCheck size={17} />
            <span>刷新令牌</span>
          </button>
        </div>
        <div className="tokenImportGrid">
          <label className="field">
            <span>OpenList Access Token</span>
            <SecretInput
              value={console.p115TokenDraft.accessToken}
              placeholder="access_token"
              visible={secretVisible("p115_openlist_access")}
              onToggle={() => toggleSecret("p115_openlist_access")}
              onChange={(value) =>
                console.setP115TokenDraft({
                  ...console.p115TokenDraft,
                  accessToken: value,
                })
              }
            />
          </label>
          <label className="field">
            <span>OpenList Refresh Token</span>
            <SecretInput
              value={console.p115TokenDraft.refreshToken}
              placeholder="refresh_token"
              visible={secretVisible("p115_openlist_refresh")}
              onToggle={() => toggleSecret("p115_openlist_refresh")}
              onChange={(value) =>
                console.setP115TokenDraft({
                  ...console.p115TokenDraft,
                  refreshToken: value,
                })
              }
            />
          </label>
          <button
            className="primaryButton"
            onClick={() =>
              console.importP115Token(
                console.p115TokenDraft.accessToken,
                console.p115TokenDraft.refreshToken,
              )
            }
            disabled={console.busy}
            type="button"
          >
            <Import size={17} />
            <span>导入 Token</span>
          </button>
        </div>
        {console.p115QRSession && (
          <div className="qrPanel">
            <img src={console.p115QRSession.qrcode_url} alt="115 Cookies 登录二维码" />
            <div>
              <strong>{console.p115QRStatus || "等待扫码"}</strong>
              <span>{new Date(console.p115QRSession.expires_at).toLocaleString()}</span>
              <div className="inlineActions">
                <button className="secondaryButton" onClick={console.refreshP115QRCodeStatus} disabled={console.busy} type="button">
                  <Clock3 size={17} />
                  <span>刷新状态</span>
                </button>
                <button className="primaryButton" onClick={console.completeP115QRCode} disabled={console.busy} type="button">
                  <CheckCircle2 size={17} />
                  <span>保存 Cookies</span>
                </button>
              </div>
            </div>
          </div>
        )}
        {console.p115OAuthRedirect && (
          <div className="inlineHint">OAuth 回调地址：{console.p115OAuthRedirect}</div>
        )}
      </Card>

      <Card title="STRM 同步" eyebrow="Sync Jobs">
        <div className="formGrid scheduleGrid">
          <CheckLine
            label="定时增量同步"
            checked={console.p115Settings.sync_cron_enabled}
            onChange={(checked) =>
              console.setP115Settings({
                ...console.p115Settings,
                sync_cron_enabled: checked,
              })
            }
          />
          <label className="field">
            <span>同步间隔（分钟）</span>
            <input
              type="number"
              min="5"
              max="10080"
              value={console.p115Settings.sync_interval_minutes || 60}
              onChange={(event) =>
                console.setP115Settings({
                  ...console.p115Settings,
                  sync_interval_minutes: Number.parseInt(event.target.value, 10) || 60,
                })
              }
            />
          </label>
        </div>
        <div className="settingsActions">
          <button className="primaryButton" onClick={console.saveP115} disabled={console.busy} type="button">
            <Clock3 size={17} />
            <span>保存同步设置</span>
          </button>
          <button className="secondaryButton" onClick={console.exportP115Tree} disabled={console.busy} type="button">
            <FolderOpen size={17} />
            <span>刷新目录快照</span>
          </button>
          <button
            className="secondaryButton"
            onClick={console.previewP115STRM}
            disabled={console.busy || console.p115STRMPreviewLoading}
            type="button"
          >
            <FileSymlink size={17} />
            <span>{console.p115STRMPreviewLoading ? "预览中" : "预览路径"}</span>
          </button>
          <button className="secondaryButton" onClick={console.syncP115STRM} disabled={console.busy} type="button">
            <FileSymlink size={17} />
            <span>同步 STRM</span>
          </button>
          <button className="secondaryButton" onClick={console.rebuildP115Nodes} disabled={console.busy} type="button">
            <DatabaseZap size={17} />
            <span>重新生成 STRM</span>
          </button>
          <button className="secondaryButton" onClick={console.cleanupP115STRM} disabled={console.busy} type="button">
            <Trash2 size={17} />
            <span>清理孤儿文件</span>
          </button>
        </div>
        <P115STRMPreviewPanel preview={console.p115STRMPreview} loading={console.p115STRMPreviewLoading} />
        <P115SyncRunTable rows={console.p115SyncRuns} />
      </Card>
    </>
  );
}

function EmbySettings({
  console,
  secretVisible,
  toggleSecret,
}: {
  console: CurioConsole;
  secretVisible: (key: string) => boolean;
  toggleSecret: (key: string) => void;
}) {
  return (
    <Card title="Emby 反代" eyebrow="Playback Proxy">
      <p className="cardIntro">Emby 反代用于 115 播放链路，把媒体服务器请求转发到 Curio 播放代理。</p>
      <div className="formGrid">
        <label className="field">
          <span>Emby 原始地址</span>
          <input
            value={console.p115Settings.emby_upstream_url}
            placeholder="http://emby:8096"
            onChange={(event) =>
              console.setP115Settings({
                ...console.p115Settings,
                emby_upstream_url: event.target.value,
              })
            }
          />
        </label>
        <label className="field">
          <span>API Key</span>
          <SecretInput
            value={console.p115Settings.emby_api_key}
            visible={secretVisible("p115_emby_api_key")}
            onToggle={() => toggleSecret("p115_emby_api_key")}
            onChange={(value) =>
              console.setP115Settings({
                ...console.p115Settings,
                emby_api_key: value,
              })
            }
          />
        </label>
        <label className="field">
          <span>反代端口</span>
          <input
            type="number"
            min="1"
            max="65535"
            value={console.p115Settings.emby_proxy_port || 8097}
            placeholder="8097"
            onChange={(event) =>
              console.setP115Settings({
                ...console.p115Settings,
                emby_proxy_port: Number.parseInt(event.target.value, 10) || 0,
              })
            }
          />
        </label>
      </div>
      <div className="inlineHint">
        示例：公网访问地址可结合反代端口与基础路径 `/emby` 使用。
      </div>
      <div className="settingsActions">
        <button className="primaryButton" onClick={console.saveP115} disabled={console.busy} type="button">
          <Server size={17} />
          <span>保存 Emby 配置</span>
        </button>
      </div>
    </Card>
  );
}

function CloudDriveTable({
  rows,
  onOpen,
}: {
  rows: CloudDriveFile[];
  onOpen: (path: string) => void;
}) {
  return (
    <div className="tableFrame">
      <table className="dataTable">
        <thead>
          <tr>
            <th>名称</th>
            <th>类型</th>
            <th>大小</th>
            <th>路径</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((file) => (
            <tr
              className={file.is_directory ? "clickableRow" : ""}
              key={file.path}
              onClick={() => file.is_directory && onOpen(file.path)}
            >
              <td>{file.name}</td>
              <td>{file.is_directory ? "目录" : "文件"}</td>
              <td>{file.is_directory ? "" : formatSize(file.size)}</td>
              <td title={file.path}>{shortPath(file.path)}</td>
            </tr>
          ))}
          {rows.length === 0 && (
            <tr>
              <td className="emptyCell" colSpan={4}>
                暂无数据
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

function P115STRMPreviewPanel({
  preview,
  loading,
}: {
  preview: STRMPreview;
  loading: boolean;
}) {
  const rows = preview.items ?? [];
  if (!loading && rows.length === 0 && !preview.message) return null;
  return (
    <div className="strmPreviewPanel">
      <div className="strmPreviewHeader">
        <span>STRM 路径预览</span>
        <strong>
          {loading ? "加载中" : `${rows.length} / ${preview.total || rows.length}`}
        </strong>
      </div>
      {preview.message && rows.length === 0 ? (
        <div className="inlineHint">{preview.message}</div>
      ) : (
        <div className="tableFrame strmPreviewTable">
          <table className="dataTable">
            <thead>
              <tr>
                <th>CID</th>
                <th>源文件</th>
                <th>STRM 路径</th>
                <th>播放地址</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((item) => (
                <tr key={`${item.library_cid}:${item.relative_path}`}>
                  <td>{item.library_cid}</td>
                  <td title={item.relative_path}>{shortPath(item.relative_path)}</td>
                  <td title={item.strm_path}>{shortPath(item.strm_path)}</td>
                  <td title={item.play_path}>{shortPath(item.play_path)}</td>
                </tr>
              ))}
              {rows.length === 0 && (
                <tr>
                  <td className="emptyCell" colSpan={4}>
                    {loading ? "正在生成预览" : "暂无预览"}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function P115SyncRunTable({ rows }: { rows: P115SyncRun[] }) {
  return (
    <div className="tableFrame syncRunTable">
      <table className="dataTable">
        <thead>
          <tr>
            <th>时间</th>
            <th>来源</th>
            <th>状态</th>
            <th>模式</th>
            <th>结果</th>
            <th>摘要</th>
            <th>耗时</th>
            <th>错误</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((run) => (
            <tr key={run.id}>
              <td>{formatFullDate(run.started_at)}</td>
              <td>{p115TriggerLabel(run.trigger)}</td>
              <td>
                <StatusPill value={run.status} label={statusLabel(run.status)} tone={statusTone(run.status)} />
              </td>
              <td>{p115ModeLabel(run.mode)}</td>
              <td>{`新增 ${run.generated} / 更新 ${run.updated} / 删除 ${run.deleted} / 失败 ${run.failed}`}</td>
              <td title={run.event_summary}>
                {run.event_summary ? shortText(run.event_summary, 36) : "-"}
              </td>
              <td>{formatDuration(run.duration_ms)}</td>
              <td title={run.error_message}>
                {run.error_message ? shortText(run.error_message, 28) : "-"}
              </td>
            </tr>
          ))}
          {rows.length === 0 && (
            <tr>
              <td className="emptyCell" colSpan={8}>
                暂无同步记录
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

function CheckLine({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label className="checkLine">
      <input
        type="checkbox"
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span>{label}</span>
    </label>
  );
}

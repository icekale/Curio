import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  API_URL,
  endpoints,
  getAuthToken,
  isAuthError,
  setAuthToken,
} from "../api";
import { isPage, type Page, pageStorageKey } from "../app/navigation";
import type {
  Batch,
  CloudDriveFile,
  CloudDriveSettings,
  CollectionPage,
  DirectoryConfig,
  Health,
  LogPage,
  MediaFile,
  MediaFilePage,
  MediaStats,
  NamingTemplate,
  P115QRCodeSession,
  P115Settings,
  P115SyncRun,
  RearchivePayload,
  STRMPreview,
  SystemSettings,
  TVShowPage,
} from "../types";
import { arrayOrEmpty, percent, taskDone } from "../utils/format";
import {
  emptyCloudDrive,
  emptyDirs,
  emptyP115,
  emptySettings,
} from "../utils/settings";
import { p115ModeLabel } from "../utils/labels";

export type ToastState = {
  id: number;
  message: string;
  tone: "success" | "error" | "info";
};

export type CollectionStatusFilter = "all" | "incomplete" | "complete";

export type LogFilter =
  | "all"
  | "ai_filename"
  | "playback"
  | "p115_sync"
  | "operation"
  | "scan_batch";

export type RearchiveDraft = {
  tmdbID: string;
  mediaType: "movie" | "tv_episode";
  season: string;
  episode: string;
  seasonOffset: string;
  episodeOffset: string;
};

export type P115TokenDraft = {
  accessToken: string;
  refreshToken: string;
};

const emptyStats: MediaStats = {
  total: 0,
  done: 0,
  failed: 0,
  incomplete_collection: 0,
  missing_tv_season_count: 0,
  missing_tv_episode_count: 0,
};

export const mediaPageLimit = 50;
export const logPageLimit = 50;

const emptyMediaPage: MediaFilePage = {
  items: [],
  total: 0,
  limit: mediaPageLimit,
  offset: 0,
};

const emptyTVShowPage: TVShowPage = {
  items: [],
  total: 0,
  limit: mediaPageLimit,
  offset: 0,
};

const emptyCollectionPage: CollectionPage = {
  items: [],
  total: 0,
  limit: mediaPageLimit,
  offset: 0,
};

const emptyLogPage: LogPage = {
  items: [],
  total: 0,
  limit: logPageLimit,
  offset: 0,
  type: "all",
};

const emptySTRMPreview: STRMPreview = {
  items: [],
  total: 0,
  limit: 50,
  source: "",
};

const processingStatuses = [
  "incoming",
  "scanned",
  "parsed",
  "scraped",
  "matched",
  "collection_checked",
  "planned",
].join(",");

function normalizeMediaPage(
  value: MediaFilePage | null | undefined,
): MediaFilePage {
  return {
    items: arrayOrEmpty(value?.items),
    total: Number.isFinite(value?.total) ? Number(value?.total) : 0,
    limit:
      Number.isFinite(value?.limit) && Number(value?.limit) > 0
        ? Number(value?.limit)
        : mediaPageLimit,
    offset:
      Number.isFinite(value?.offset) && Number(value?.offset) >= 0
        ? Number(value?.offset)
        : 0,
  };
}

function normalizeTVShowPage(value: TVShowPage | null | undefined): TVShowPage {
  return {
    items: arrayOrEmpty(value?.items),
    total: Number.isFinite(value?.total) ? Number(value?.total) : 0,
    limit:
      Number.isFinite(value?.limit) && Number(value?.limit) > 0
        ? Number(value?.limit)
        : mediaPageLimit,
    offset:
      Number.isFinite(value?.offset) && Number(value?.offset) >= 0
        ? Number(value?.offset)
        : 0,
  };
}

function normalizeCollectionPage(
  value: CollectionPage | null | undefined,
): CollectionPage {
  return {
    items: arrayOrEmpty(value?.items),
    total: Number.isFinite(value?.total) ? Number(value?.total) : 0,
    limit:
      Number.isFinite(value?.limit) && Number(value?.limit) > 0
        ? Number(value?.limit)
        : mediaPageLimit,
    offset:
      Number.isFinite(value?.offset) && Number(value?.offset) >= 0
        ? Number(value?.offset)
        : 0,
  };
}

function normalizeLogPage(
  value: LogPage | null | undefined,
  type: LogFilter,
  fallbackOffset: number,
): LogPage {
  return {
    items: arrayOrEmpty(value?.items),
    total: Number.isFinite(value?.total) ? Number(value?.total) : 0,
    limit:
      Number.isFinite(value?.limit) && Number(value?.limit) > 0
        ? Number(value?.limit)
        : logPageLimit,
    offset:
      Number.isFinite(value?.offset) && Number(value?.offset) >= 0
        ? Number(value?.offset)
        : fallbackOffset,
    type: value?.type || type,
  };
}

function optionalPositiveInt(value: string, allowZero = false) {
  const trimmed = value.trim();
  if (!trimmed) return undefined;
  if (!/^\d+$/.test(trimmed)) return Number.NaN;
  const parsed = Number.parseInt(trimmed, 10);
  if (!Number.isFinite(parsed) || parsed < (allowZero ? 0 : 1)) {
    return Number.NaN;
  }
  return parsed;
}

function optionalInt(value: string) {
  const trimmed = value.trim();
  if (!trimmed) return undefined;
  if (!/^-?\d+$/.test(trimmed)) return Number.NaN;
  const parsed = Number.parseInt(trimmed, 10);
  if (!Number.isFinite(parsed)) return Number.NaN;
  return parsed;
}

function isTVLike(file: MediaFile) {
  return (
    file.media_type === "tv_episode" ||
    file.season > 0 ||
    file.episode > 0 ||
    /S\d{1,2}E\d{1,3}/i.test(file.original_file_name)
  );
}

function rearchivePayload(draft: RearchiveDraft): RearchivePayload | null {
  const tmdbID = optionalPositiveInt(draft.tmdbID);
  const season = optionalPositiveInt(draft.season, true);
  const episode = optionalPositiveInt(draft.episode);
  const seasonOffset = optionalInt(draft.seasonOffset);
  const episodeOffset = optionalInt(draft.episodeOffset);
  if (
    [tmdbID, season, episode, seasonOffset, episodeOffset].some((value) =>
      Number.isNaN(value),
    )
  ) {
    return null;
  }
  const payload: RearchivePayload = { media_type: draft.mediaType };
  if (tmdbID !== undefined) payload.tmdb_id = tmdbID;
  if (draft.mediaType === "tv_episode") {
    if (season !== undefined) payload.season = season;
    if (episode !== undefined) payload.episode = episode;
    if (seasonOffset !== undefined) payload.season_offset = seasonOffset;
    if (episodeOffset !== undefined) payload.episode_offset = episodeOffset;
  }
  return payload;
}

export function useCurioConsole() {
  const [page, setPageState] = useState<Page>(() => {
    if (typeof window === "undefined") return "dashboard";
    const saved = window.localStorage.getItem(pageStorageKey);
    return isPage(saved) ? saved : "dashboard";
  });
  const [health, setHealth] = useState<Health | null>(null);
  const [stats, setStats] = useState<MediaStats>(emptyStats);
  const [activeTask, setActiveTask] = useState<Batch | null>(null);
  const [batches, setBatches] = useState<Batch[]>([]);
  const [directories, setDirectories] = useState<DirectoryConfig>(emptyDirs);
  const [systemSettings, setSystemSettings] =
    useState<SystemSettings>(emptySettings);
  const [cloudDriveSettings, setCloudDriveSettings] =
    useState<CloudDriveSettings>(emptyCloudDrive);
  const [p115Settings, setP115Settings] = useState<P115Settings>(emptyP115);
  const [p115SyncRuns, setP115SyncRuns] = useState<P115SyncRun[]>([]);
  const [p115STRMPreview, setP115STRMPreview] =
    useState<STRMPreview>(emptySTRMPreview);
  const [p115STRMPreviewLoading, setP115STRMPreviewLoading] = useState(false);
  const [templates, setTemplates] = useState<NamingTemplate[]>([]);
  const [classification, setClassification] = useState("");
  const [mediaPage, setMediaPage] = useState<MediaFilePage>(emptyMediaPage);
  const [stagingPage, setStagingPage] = useState<MediaFilePage>(emptyMediaPage);
  const [failedPage, setFailedPage] = useState<MediaFilePage>(emptyMediaPage);
  const [tvShowPage, setTVShowPage] = useState<TVShowPage>(emptyTVShowPage);
  const [collectionPage, setCollectionPage] =
    useState<CollectionPage>(emptyCollectionPage);
  const [logPage, setLogPage] = useState<LogPage>(emptyLogPage);
  const [logLoading, setLogLoading] = useState(false);
  const [mediaQuery, setMediaQueryValue] = useState("");
  const [stagingQuery, setStagingQueryValue] = useState("");
  const [failedQuery, setFailedQueryValue] = useState("");
  const [tvQuery, setTVQueryValue] = useState("");
  const [collectionQuery, setCollectionQueryValue] = useState("");
  const [collectionStatus, setCollectionStatusValue] =
    useState<CollectionStatusFilter>("all");
  const [logFilter, setLogFilterValue] = useState<LogFilter>("all");
  const [mediaOffset, setMediaOffsetValue] = useState(0);
  const [stagingOffset, setStagingOffsetValue] = useState(0);
  const [failedOffset, setFailedOffsetValue] = useState(0);
  const [tvOffset, setTVOffsetValue] = useState(0);
  const [collectionOffset, setCollectionOffsetValue] = useState(0);
  const [logOffset, setLogOffsetValue] = useState(0);
  const [selectedMedia, setSelectedMedia] = useState<string[]>([]);
  const [selectedStaging, setSelectedStaging] = useState<string[]>([]);
  const [selectedFailed, setSelectedFailed] = useState<string[]>([]);
  const [cloudDriveFiles, setCloudDriveFiles] = useState<CloudDriveFile[]>([]);
  const [cloudDrivePath, setCloudDrivePath] = useState("/");
  const [p115QRSession, setP115QRSession] = useState<P115QRCodeSession | null>(
    null,
  );
  const [p115QRStatus, setP115QRStatus] = useState("");
  const [p115OAuthRedirect, setP115OAuthRedirect] = useState("");
  const [p115TokenDraft, setP115TokenDraft] = useState<P115TokenDraft>({
    accessToken: "",
    refreshToken: "",
  });
  const [preview, setPreview] = useState("");
  const [toast, setToast] = useState<ToastState | null>(null);
  const [rearchiveTargets, setRearchiveTargets] = useState<MediaFile[]>([]);
  const [rearchiveDraft, setRearchiveDraft] = useState<RearchiveDraft>({
    tmdbID: "",
    mediaType: "movie",
    season: "",
    episode: "",
    seasonOffset: "",
    episodeOffset: "",
  });
  const [busy, setBusy] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [authChecked, setAuthChecked] = useState(false);
  const [authRequired, setAuthRequired] = useState(false);
  const [authTokenDraft, setAuthTokenDraft] = useState("");
  const draftsReady = useRef(false);
  const toastSeq = useRef(0);
  const refreshTimerRef = useRef<number | null>(null);

  const latestBatch = activeTask ?? batches[0];
  const taskProgress = useMemo(() => {
    const total = activeTask?.total ?? 0;
    const done = taskDone(activeTask ?? {});
    return { total, done, percent: percent(done, total) };
  }, [activeTask]);

  const showToast = useCallback(
    (message: string, tone: ToastState["tone"] = "info") => {
      toastSeq.current += 1;
      setToast({ id: toastSeq.current, message, tone });
    },
    [],
  );

  const handleLoadError = useCallback(
    (error: unknown, fallback = "数据加载失败") => {
      if (isAuthError(error)) {
        setAuthRequired(true);
        return;
      }
      showToast(error instanceof Error ? error.message : fallback, "error");
    },
    [showToast],
  );

  const setPage = useCallback((next: Page) => {
    setPageState(next);
    window.localStorage.setItem(pageStorageKey, next);
  }, []);

  const clearSelected = useCallback(() => {
    setSelectedMedia([]);
    setSelectedStaging([]);
    setSelectedFailed([]);
  }, []);

  const load = useCallback(
    async (includeSettings = !draftsReady.current) => {
      const currentPage = page;
      const loadMedia =
        currentPage === "dashboard" ||
        currentPage === "scan" ||
        currentPage === "processing";
      const loadStaging = currentPage === "staging";
      const loadFailed = currentPage === "failed";
      const loadTV = currentPage === "tv";
      const loadCollections = currentPage === "collections";
      const loadLogs = currentPage === "logs";
      const loadSettings = currentPage === "settings";

      if (loadLogs) setLogLoading(true);
      try {
        const [
          healthData,
          statsData,
          activeData,
          batchData,
          mediaData,
          stagingData,
          failedData,
          tvData,
          collectionData,
          logData,
          p115RunsData,
        ] = await Promise.all([
          endpoints.health(),
          endpoints.stats(),
          endpoints.activeTask(),
          endpoints.batches(),
          loadMedia
            ? endpoints.mediaFiles({
                query: mediaQuery,
                offset: mediaOffset,
                limit: mediaPageLimit,
                status:
                  currentPage === "processing" ? processingStatuses : undefined,
              })
            : Promise.resolve(null),
          loadStaging
            ? endpoints.staging({
                query: stagingQuery,
                offset: stagingOffset,
                limit: mediaPageLimit,
              })
            : Promise.resolve(null),
          loadFailed
            ? endpoints.failed({
                query: failedQuery,
                offset: failedOffset,
                limit: mediaPageLimit,
              })
            : Promise.resolve(null),
          loadTV
            ? endpoints.tvShows({
                query: tvQuery,
                offset: tvOffset,
                limit: mediaPageLimit,
              })
            : Promise.resolve(null),
          loadCollections
            ? endpoints.collections({
                query: collectionQuery,
                offset: collectionOffset,
                limit: mediaPageLimit,
                status: collectionStatus === "all" ? undefined : collectionStatus,
              })
            : Promise.resolve(null),
          loadLogs
            ? endpoints.logs({
                type: logFilter,
                offset: logOffset,
                limit: logPageLimit,
              })
            : Promise.resolve(null),
          loadSettings ? endpoints.p115SyncRuns() : Promise.resolve(null),
        ]);

        setHealth(healthData);
        setStats(statsData);
        setActiveTask(activeData ?? healthData.active_task ?? null);
        setBatches(arrayOrEmpty(batchData));
        if (loadMedia) setMediaPage(normalizeMediaPage(mediaData));
        if (loadStaging) setStagingPage(normalizeMediaPage(stagingData));
        if (loadFailed) setFailedPage(normalizeMediaPage(failedData));
        if (loadTV) setTVShowPage(normalizeTVShowPage(tvData));
        if (loadCollections) {
          setCollectionPage(normalizeCollectionPage(collectionData));
        }
        if (loadLogs) setLogPage(normalizeLogPage(logData, logFilter, logOffset));
        if (loadSettings) setP115SyncRuns(arrayOrEmpty(p115RunsData));

        if (includeSettings) {
          const [
            dirData,
            settingsData,
            cloudDriveData,
            p115Data,
            templateData,
            classificationData,
          ] = await Promise.all([
            endpoints.directories(),
            endpoints.systemSettings(),
            endpoints.cloudDriveSettings(),
            endpoints.p115Settings(),
            endpoints.templates(),
            endpoints.classification(),
          ]);
          setDirectories(dirData);
          setSystemSettings(settingsData);
          setCloudDriveSettings(cloudDriveData);
          setP115Settings(p115Data);
          setTemplates(arrayOrEmpty(templateData));
          setClassification(classificationData?.classification_yaml ?? "");
          setCloudDrivePath(cloudDriveData.root_path || "/");
          draftsReady.current = true;
        }
      } finally {
        if (loadLogs) setLogLoading(false);
      }
    },
    [
      page,
      mediaQuery,
      mediaOffset,
      stagingQuery,
      stagingOffset,
      failedQuery,
      failedOffset,
      tvQuery,
      tvOffset,
      collectionQuery,
      collectionOffset,
      collectionStatus,
      logFilter,
      logOffset,
    ],
  );

  const scheduleLoad = useCallback(
    (includeSettings = false, delay = 180) => {
      if (refreshTimerRef.current) window.clearTimeout(refreshTimerRef.current);
      refreshTimerRef.current = window.setTimeout(() => {
        refreshTimerRef.current = null;
        load(includeSettings).catch(() => undefined);
      }, delay);
    },
    [load],
  );

  useEffect(() => {
    let closed = false;
    endpoints
      .authStatus()
      .then((status) => {
        if (closed) return;
        setAuthRequired(status.enabled && !getAuthToken());
        setAuthChecked(true);
      })
      .catch((error) => {
        if (closed) return;
        setAuthChecked(true);
        handleLoadError(error, "鉴权状态读取失败");
      });
    return () => {
      closed = true;
    };
  }, [handleLoadError]);

  useEffect(() => {
    if (!authChecked || authRequired) return undefined;
    load().catch(handleLoadError);
    if (typeof EventSource === "undefined") {
      const timer = window.setInterval(() => scheduleLoad(false, 0), 8000);
      return () => {
        window.clearInterval(timer);
        if (refreshTimerRef.current) window.clearTimeout(refreshTimerRef.current);
      };
    }
    const eventURL = new URL(`${API_URL}/api/events`, window.location.origin);
    const token = getAuthToken();
    if (token) eventURL.searchParams.set("token", token);
    const events = new EventSource(eventURL.toString());
    events.onmessage = () => scheduleLoad(false);
    events.onerror = () => undefined;
    return () => {
      events.close();
      if (refreshTimerRef.current) window.clearTimeout(refreshTimerRef.current);
    };
  }, [authChecked, authRequired, handleLoadError, load, scheduleLoad]);

  useEffect(() => {
    if (!authChecked || authRequired) return undefined;
    const timer = window.setTimeout(() => load(false).catch(handleLoadError), 260);
    return () => window.clearTimeout(timer);
  }, [
    authChecked,
    authRequired,
    page,
    mediaQuery,
    stagingQuery,
    failedQuery,
    tvQuery,
    collectionQuery,
    collectionStatus,
    logFilter,
    mediaOffset,
    stagingOffset,
    failedOffset,
    tvOffset,
    collectionOffset,
    logOffset,
    load,
    handleLoadError,
  ]);

  useEffect(() => {
    if (!toast) return undefined;
    const timer = window.setTimeout(
      () => setToast((current) => (current?.id === toast.id ? null : current)),
      3200,
    );
    return () => window.clearTimeout(timer);
  }, [toast]);

  const loginWithToken = useCallback(async () => {
    const token = authTokenDraft.trim();
    if (!token) {
      showToast("请输入管理令牌", "error");
      return;
    }
    setBusy(true);
    try {
      const result = await endpoints.authLogin(token);
      if (result.enabled) setAuthToken(token);
      setAuthRequired(false);
      setAuthTokenDraft("");
      draftsReady.current = false;
      await load(true);
      showToast("登录成功", "success");
    } catch (error) {
      showToast(error instanceof Error ? error.message : "登录失败", "error");
    } finally {
      setBusy(false);
    }
  }, [authTokenDraft, load, showToast]);

  const refreshData = useCallback(async () => {
    setRefreshing(true);
    try {
      await load(true);
      showToast("数据已刷新", "success");
    } catch (error) {
      handleLoadError(error, "刷新失败");
    } finally {
      setRefreshing(false);
    }
  }, [handleLoadError, load, showToast]);

  const startScan = useCallback(async () => {
    setBusy(true);
    try {
      const result = await endpoints.startScan();
      showToast(
        result.status === "started" ? "本地整理已开始" : result.status,
        "success",
      );
      setPage("scan");
      await load();
    } catch (error) {
      showToast(error instanceof Error ? error.message : "启动失败", "error");
    } finally {
      setBusy(false);
    }
  }, [load, setPage, showToast]);

  const startCloudDriveScan = useCallback(async () => {
    setBusy(true);
    try {
      const result = await endpoints.startCloudDriveScan();
      showToast(
        result.status === "started" ? "云端整理已开始" : result.status,
        "success",
      );
      setPage("scan");
      await load();
    } catch (error) {
      showToast(
        error instanceof Error ? error.message : "云端整理启动失败",
        "error",
      );
    } finally {
      setBusy(false);
    }
  }, [load, setPage, showToast]);

  const stopActiveTask = useCallback(async () => {
    if (!activeTask) return;
    setBusy(true);
    try {
      await endpoints.stopTask(activeTask.batch_id);
      showToast("已请求停止任务", "info");
      await load();
    } catch (error) {
      showToast(error instanceof Error ? error.message : "停止失败", "error");
    } finally {
      setBusy(false);
    }
  }, [activeTask, load, showToast]);

  const deleteMediaRecords = useCallback(
    async (files: MediaFile[]) => {
      const targets = files.filter(Boolean);
      if (targets.length === 0) return;
      const label =
        targets.length === 1
          ? targets[0].original_file_name
          : `${targets.length} 条记录`;
      const ok = window.confirm(`仅删除数据库记录，不会删除真实文件：${label}`);
      if (!ok) return;
      setBusy(true);
      try {
        if (targets.length === 1) {
          await endpoints.deleteMediaFile(targets[0].file_id);
        } else {
          await endpoints.deleteMediaFiles(targets.map((file) => file.file_id));
        }
        clearSelected();
        showToast(`已删除 ${targets.length} 条记录，源文件未改动`, "success");
        await load(false);
      } catch (error) {
        showToast(
          error instanceof Error ? error.message : "记录删除失败",
          "error",
        );
      } finally {
        setBusy(false);
      }
    },
    [clearSelected, load, showToast],
  );

  const openRearchive = useCallback((files: MediaFile[] | MediaFile) => {
    const targets = Array.isArray(files) ? files : [files];
    if (targets.length === 0) return;
    setRearchiveTargets(targets);
    setRearchiveDraft({
      tmdbID: "",
      mediaType: targets.some(isTVLike) ? "tv_episode" : "movie",
      season:
        targets.length === 1 && targets[0].season > 0
          ? String(targets[0].season)
          : "",
      episode:
        targets.length === 1 && targets[0].episode > 0
          ? String(targets[0].episode)
          : "",
      seasonOffset: "",
      episodeOffset: "",
    });
  }, []);

  const submitRearchive = useCallback(async () => {
    if (rearchiveTargets.length === 0) return;
    const payload = rearchivePayload(rearchiveDraft);
    if (!payload) {
      showToast("请输入有效的季集或偏移", "error");
      return;
    }
    setBusy(true);
    try {
      let successCount = 1;
      let failedCount = 0;
      if (rearchiveTargets.length === 1) {
        await endpoints.rearchiveMediaFile(rearchiveTargets[0].file_id, payload);
      } else {
        const result = await endpoints.rearchiveMediaFiles(
          rearchiveTargets.map((file) => file.file_id),
          payload,
        );
        successCount = result.count;
        failedCount = result.failed ?? 0;
      }
      showToast(
        failedCount > 0
          ? `重新归档完成：成功 ${successCount}，失败 ${failedCount}`
          : `已重新归档 ${successCount} 条记录`,
        failedCount > 0 ? "error" : "success",
      );
      setRearchiveTargets([]);
      clearSelected();
      await load(false);
    } catch (error) {
      showToast(
        error instanceof Error ? error.message : "重新归档失败",
        "error",
      );
    } finally {
      setBusy(false);
    }
  }, [clearSelected, load, rearchiveDraft, rearchiveTargets, showToast]);

  const saveDirectories = useCallback(async () => {
    setBusy(true);
    try {
      const saved = await endpoints.saveDirectories(directories);
      setDirectories(saved);
      showToast("本地目录已保存", "success");
    } catch (error) {
      showToast(error instanceof Error ? error.message : "目录保存失败", "error");
    } finally {
      setBusy(false);
    }
  }, [directories, showToast]);

  const saveSystemSettings = useCallback(async () => {
    setBusy(true);
    try {
      const saved = await endpoints.saveSystemSettings(systemSettings);
      setSystemSettings(saved);
      showToast("系统配置已保存", "success");
    } catch (error) {
      showToast(
        error instanceof Error ? error.message : "系统配置保存失败",
        "error",
      );
    } finally {
      setBusy(false);
    }
  }, [showToast, systemSettings]);

  const saveClassification = useCallback(async () => {
    setBusy(true);
    try {
      const saved = await endpoints.saveClassification({
        classification_yaml: classification,
      });
      setClassification(saved.classification_yaml);
      showToast("分类规则已保存", "success");
    } catch (error) {
      showToast(
        error instanceof Error ? error.message : "分类规则保存失败",
        "error",
      );
    } finally {
      setBusy(false);
    }
  }, [classification, showToast]);

  const saveTemplate = useCallback(
    async (template: NamingTemplate) => {
      setBusy(true);
      try {
        const saved = await endpoints.saveTemplate(
          template.template_type,
          template,
        );
        setTemplates((items) =>
          items.map((item) =>
            item.template_type === saved.template_type ? saved : item,
          ),
        );
        showToast("命名模板已保存", "success");
      } catch (error) {
        showToast(
          error instanceof Error ? error.message : "模板保存失败",
          "error",
        );
      } finally {
        setBusy(false);
      }
    },
    [showToast],
  );

  const showTemplatePreview = useCallback(
    async (template: NamingTemplate) => {
      try {
        const result = await endpoints.previewTemplate({
          template_type: template.template_type,
          template: template.template,
        });
        setPreview(result.preview);
        showToast("预览已生成", "success");
      } catch (error) {
        setPreview(error instanceof Error ? error.message : "预览失败");
        showToast(error instanceof Error ? error.message : "预览失败", "error");
      }
    },
    [showToast],
  );

  const saveCloudDrive = useCallback(async () => {
    setBusy(true);
    try {
      const saved = await endpoints.saveCloudDriveSettings(cloudDriveSettings);
      setCloudDriveSettings(saved);
      showToast("CloudDrive2 配置已保存", "success");
    } catch (error) {
      showToast(
        error instanceof Error ? error.message : "CloudDrive2 配置保存失败",
        "error",
      );
    } finally {
      setBusy(false);
    }
  }, [cloudDriveSettings, showToast]);

  const testCloudDrive = useCallback(async () => {
    setBusy(true);
    try {
      const status = await endpoints.testCloudDrive();
      showToast(
        status.can_browse ? "CloudDrive2 连接正常" : "CloudDrive2 已响应",
        "success",
      );
    } catch (error) {
      showToast(
        error instanceof Error ? error.message : "CloudDrive2 测试失败",
        "error",
      );
    } finally {
      setBusy(false);
    }
  }, [showToast]);

  const openCloudDrivePath = useCallback(
    async (path: string) => {
      setBusy(true);
      try {
        const files = await endpoints.cloudDriveFiles(path);
        setCloudDriveFiles(arrayOrEmpty(files));
        setCloudDrivePath(path);
        showToast("云端目录已打开", "success");
      } catch (error) {
        showToast(
          error instanceof Error ? error.message : "云端目录读取失败",
          "error",
        );
      } finally {
        setBusy(false);
      }
    },
    [showToast],
  );

  const saveP115 = useCallback(async () => {
    setBusy(true);
    try {
      const saved = await endpoints.saveP115Settings(p115Settings);
      setP115Settings(saved);
      showToast("115 配置已保存", "success");
    } catch (error) {
      showToast(
        error instanceof Error ? error.message : "115 配置保存失败",
        "error",
      );
    } finally {
      setBusy(false);
    }
  }, [p115Settings, showToast]);

  const startP115QRCode = useCallback(async () => {
    setBusy(true);
    try {
      const saved = await endpoints.saveP115Settings(p115Settings);
      setP115Settings(saved);
      const session = await endpoints.startP115QRCode();
      setP115QRSession(session);
      setP115QRStatus("等待扫码");
      showToast("二维码已生成，请使用 115 App 扫码获取 Cookies", "success");
    } catch (error) {
      showToast(
        error instanceof Error ? error.message : "二维码生成失败",
        "error",
      );
    } finally {
      setBusy(false);
    }
  }, [p115Settings, showToast]);

  const refreshP115QRCodeStatus = useCallback(async () => {
    if (!p115QRSession) return;
    setBusy(true);
    try {
      const status = await endpoints.p115QRCodeStatus(p115QRSession.uid);
      setP115QRStatus(status.message || status.status || "等待扫码");
      showToast(status.message || "扫码状态已刷新", "info");
    } catch (error) {
      showToast(
        error instanceof Error ? error.message : "扫码状态读取失败",
        "error",
      );
    } finally {
      setBusy(false);
    }
  }, [p115QRSession, showToast]);

  const completeP115QRCode = useCallback(async () => {
    if (!p115QRSession) {
      showToast("请先生成二维码", "error");
      return;
    }
    setBusy(true);
    try {
      const result = await endpoints.completeP115QRCode(p115QRSession.uid);
      const saved = await endpoints.p115Settings();
      setP115Settings(saved);
      setP115QRSession(null);
      setP115QRStatus("");
      showToast(result.message || "115 登录成功", "success");
    } catch (error) {
      showToast(
        error instanceof Error ? error.message : "Cookies 获取失败",
        "error",
      );
    } finally {
      setBusy(false);
    }
  }, [p115QRSession, showToast]);

  const startP115OAuth = useCallback(async () => {
    setBusy(true);
    try {
      const saved = await endpoints.saveP115Settings(p115Settings);
      setP115Settings(saved);
      const result = await endpoints.startP115OAuth();
      setP115OAuthRedirect(result.redirect_uri);
      window.open(result.authorize_url, "_blank", "noopener,noreferrer");
      showToast("OAuth 授权页已打开", "success");
    } catch (error) {
      showToast(
        error instanceof Error ? error.message : "OAuth 登录失败",
        "error",
      );
    } finally {
      setBusy(false);
    }
  }, [p115Settings, showToast]);

  const importP115Token = useCallback(
    async (accessToken: string, refreshToken: string) => {
      if (!accessToken.trim()) {
        showToast("请填写 Access Token", "error");
        return;
      }
      if (!refreshToken.trim()) {
        showToast("请填写 Refresh Token", "error");
        return;
      }
      setBusy(true);
      try {
        const result = await endpoints.importP115Token({
          access_token: accessToken.trim(),
          refresh_token: refreshToken.trim(),
        });
        const saved = await endpoints.p115Settings();
        setP115Settings(saved);
        setP115TokenDraft({ accessToken: "", refreshToken: "" });
        showToast(result.message || "OpenList Token 已导入", "success");
      } catch (error) {
        showToast(
          error instanceof Error ? error.message : "OpenList Token 导入失败",
          "error",
        );
      } finally {
        setBusy(false);
      }
    },
    [showToast],
  );

  const refreshP115Token = useCallback(async () => {
    setBusy(true);
    try {
      const result = await endpoints.refreshP115Token();
      const saved = await endpoints.p115Settings();
      setP115Settings(saved);
      showToast(result.message || "115 令牌已刷新", "success");
    } catch (error) {
      showToast(error instanceof Error ? error.message : "令牌刷新失败", "error");
    } finally {
      setBusy(false);
    }
  }, [showToast]);

  const testP115 = useCallback(async () => {
    setBusy(true);
    try {
      const status = await endpoints.testP115();
      const ok = status.ready && status.can_export && status.can_play;
      const detail =
        status.message ||
        [
          status.cookie_valid ? "Cookies 可导出目录树" : status.cookie_error,
          status.token_valid ? "Open Token 有效" : status.token_error,
        ]
          .filter(Boolean)
          .join("；");
      showToast(
        ok ? detail || "115 连接正常" : detail || "115 未就绪",
        ok ? "success" : "error",
      );
    } catch (error) {
      showToast(error instanceof Error ? error.message : "115 测试失败", "error");
    } finally {
      setBusy(false);
    }
  }, [showToast]);

  const refreshP115Runs = useCallback(() => {
    void endpoints
      .p115SyncRuns()
      .then((runs) => setP115SyncRuns(arrayOrEmpty(runs)))
      .catch(() => undefined);
  }, []);

  const exportP115Tree = useCallback(async () => {
    setBusy(true);
    try {
      const result = await endpoints.exportP115Tree();
      showToast(
        `目录快照已刷新：${result.exported} 项，媒体 ${result.skipped} 个，失败 ${result.failed} 个`,
        result.failed > 0 ? "error" : "success",
      );
      refreshP115Runs();
    } catch (error) {
      showToast(
        error instanceof Error ? error.message : "目录快照刷新失败",
        "error",
      );
    } finally {
      setBusy(false);
    }
  }, [refreshP115Runs, showToast]);

  const previewP115STRM = useCallback(async () => {
    setP115STRMPreviewLoading(true);
    try {
      const result = await endpoints.previewP115STRM(p115Settings);
      setP115STRMPreview(result);
      const count = result.total ?? 0;
      showToast(
        count > 0
          ? `已生成 ${Math.min(result.items.length, count)} / ${count} 条 STRM 路径预览`
          : result.message || "暂无可预览路径",
        count > 0 ? "success" : "info",
      );
    } catch (error) {
      showToast(
        error instanceof Error ? error.message : "STRM 路径预览失败",
        "error",
      );
    } finally {
      setP115STRMPreviewLoading(false);
    }
  }, [p115Settings, showToast]);

  const syncP115STRM = useCallback(async () => {
    setBusy(true);
    try {
      const result = await endpoints.syncP115STRM();
      const source = p115ModeLabel(result.mode ?? "");
      showToast(
        `STRM 已同步：来源 ${source}，新增 ${result.generated}，恢复 ${result.restored ?? 0}，更新 ${result.updated}，删除 ${result.deleted}，跳过 ${result.skipped}，失败 ${result.failed}`,
        result.failed > 0 ? "error" : "success",
      );
      refreshP115Runs();
    } catch (error) {
      showToast(
        error instanceof Error ? error.message : "STRM 同步失败",
        "error",
      );
    } finally {
      setBusy(false);
    }
  }, [refreshP115Runs, showToast]);

  const rebuildP115Nodes = useCallback(async () => {
    setBusy(true);
    try {
      const result = await endpoints.rebuildP115Nodes();
      showToast(
        `目录树差异同步已完成：目录树 ${result.exported} 项，新增 ${result.generated}，恢复 ${result.restored ?? 0}，更新 ${result.updated}，删除 ${result.deleted}，失败 ${result.failed}`,
        result.failed > 0 ? "error" : "success",
      );
      refreshP115Runs();
    } catch (error) {
      showToast(error instanceof Error ? error.message : "节点重建失败", "error");
    } finally {
      setBusy(false);
    }
  }, [refreshP115Runs, showToast]);

  const cleanupP115STRM = useCallback(async () => {
    setBusy(true);
    try {
      const result = await endpoints.cleanupP115STRM();
      showToast(
        `孤儿 STRM 已清理：删除 ${result.deleted} 个，失败 ${result.failed} 个`,
        result.failed > 0 ? "error" : "success",
      );
      refreshP115Runs();
    } catch (error) {
      showToast(
        error instanceof Error ? error.message : "STRM 清理失败",
        "error",
      );
    } finally {
      setBusy(false);
    }
  }, [refreshP115Runs, showToast]);

  const repairCompleteCollections = useCallback(async () => {
    setBusy(true);
    try {
      const result = await endpoints.repairCompleteCollections();
      showToast(
        result.count > 0
          ? `已修复 ${result.count} 个完整合集`
          : "没有需要修复的完整合集",
        "success",
      );
      await load(false);
    } catch (error) {
      showToast(
        error instanceof Error ? error.message : "合集修复失败",
        "error",
      );
    } finally {
      setBusy(false);
    }
  }, [load, showToast]);

  const refreshDoubanTop250 = useCallback(async () => {
    setBusy(true);
    try {
      const collection =
        await endpoints.refreshCuratedCollection("douban_top250");
      showToast(
        `已刷新豆瓣 Top250：${collection.movie_count} 条，本地 ${collection.local_count} 条`,
        "success",
      );
      await load(false);
    } catch (error) {
      showToast(
        error instanceof Error ? error.message : "豆瓣 Top250 刷新失败",
        "error",
      );
    } finally {
      setBusy(false);
    }
  }, [load, showToast]);

  const queryActions = {
    setMediaQuery: (value: string) => {
      setMediaQueryValue(value);
      setMediaOffsetValue(0);
      setSelectedMedia([]);
    },
    setStagingQuery: (value: string) => {
      setStagingQueryValue(value);
      setStagingOffsetValue(0);
      setSelectedStaging([]);
    },
    setFailedQuery: (value: string) => {
      setFailedQueryValue(value);
      setFailedOffsetValue(0);
      setSelectedFailed([]);
    },
    setTVQuery: (value: string) => {
      setTVQueryValue(value);
      setTVOffsetValue(0);
    },
    setCollectionQuery: (value: string) => {
      setCollectionQueryValue(value);
      setCollectionOffsetValue(0);
    },
    setCollectionStatus: (value: CollectionStatusFilter) => {
      setCollectionStatusValue(value);
      setCollectionOffsetValue(0);
    },
    setLogFilter: (value: LogFilter) => {
      setLogFilterValue(value);
      setLogOffsetValue(0);
    },
  };

  return {
    page,
    setPage,
    authChecked,
    authRequired,
    authTokenDraft,
    setAuthTokenDraft,
    loginWithToken,
    toast,
    showToast,
    busy,
    refreshing,
    refreshData,
    health,
    stats,
    activeTask,
    latestBatch,
    taskProgress,
    batches,
    directories,
    setDirectories,
    systemSettings,
    setSystemSettings,
    cloudDriveSettings,
    setCloudDriveSettings,
    p115Settings,
    setP115Settings,
    p115SyncRuns,
    p115STRMPreview,
    p115STRMPreviewLoading,
    templates,
    setTemplates,
    classification,
    setClassification,
    mediaPage,
    stagingPage,
    failedPage,
    tvShowPage,
    collectionPage,
    logPage,
    logLoading,
    mediaQuery,
    stagingQuery,
    failedQuery,
    tvQuery,
    collectionQuery,
    collectionStatus,
    logFilter,
    mediaOffset,
    setMediaOffset: setMediaOffsetValue,
    stagingOffset,
    setStagingOffset: setStagingOffsetValue,
    failedOffset,
    setFailedOffset: setFailedOffsetValue,
    tvOffset,
    setTVOffset: setTVOffsetValue,
    collectionOffset,
    setCollectionOffset: setCollectionOffsetValue,
    logOffset,
    setLogOffset: setLogOffsetValue,
    selectedMedia,
    setSelectedMedia,
    selectedStaging,
    setSelectedStaging,
    selectedFailed,
    setSelectedFailed,
    cloudDriveFiles,
    cloudDrivePath,
    setCloudDrivePath,
    p115QRSession,
    p115QRStatus,
    p115OAuthRedirect,
    p115TokenDraft,
    setP115TokenDraft,
    preview,
    rearchiveTargets,
    rearchiveDraft,
    setRearchiveDraft,
    closeRearchive: () => setRearchiveTargets([]),
    startScan,
    startCloudDriveScan,
    stopActiveTask,
    deleteMediaRecords,
    openRearchive,
    submitRearchive,
    saveDirectories,
    saveSystemSettings,
    saveClassification,
    saveTemplate,
    showTemplatePreview,
    saveCloudDrive,
    testCloudDrive,
    openCloudDrivePath,
    saveP115,
    startP115QRCode,
    refreshP115QRCodeStatus,
    completeP115QRCode,
    startP115OAuth,
    importP115Token,
    refreshP115Token,
    testP115,
    exportP115Tree,
    previewP115STRM,
    syncP115STRM,
    rebuildP115Nodes,
    cleanupP115STRM,
    repairCompleteCollections,
    refreshDoubanTop250,
    ...queryActions,
  };
}

export type CurioConsole = ReturnType<typeof useCurioConsole>;

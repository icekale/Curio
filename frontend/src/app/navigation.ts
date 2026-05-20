import {
  Activity,
  AlertTriangle,
  FileCode2,
  FolderCheck,
  History,
  LayoutDashboard,
  Library,
  Search,
  Settings,
  Tags,
  Tv,
} from "lucide-react";

export type Page =
  | "dashboard"
  | "scan"
  | "processing"
  | "staging"
  | "failed"
  | "tv"
  | "collections"
  | "logs"
  | "classification"
  | "templates"
  | "settings";

export type NavItem = {
  id: Page;
  label: string;
  title: string;
  description: string;
  icon: typeof LayoutDashboard;
  badge?: "failed" | "missing";
};

export const navGroups: { label: string; items: NavItem[] }[] = [
  {
    label: "指挥台",
    items: [
      {
        id: "dashboard",
        label: "总览",
        title: "档案总览",
        description: "媒体库健康、队列水位与近期活动",
        icon: LayoutDashboard,
      },
      {
        id: "scan",
        label: "扫描",
        title: "扫描舱",
        description: "启动本地或云端整理，追踪当前任务",
        icon: Search,
      },
      {
        id: "processing",
        label: "处理",
        title: "处理流水线",
        description: "查看扫描、解析、识别、匹配中的文件",
        icon: Activity,
      },
    ],
  },
  {
    label: "档案库",
    items: [
      {
        id: "staging",
        label: "完成",
        title: "归档完成",
        description: "已完成重命名并进入整理目录的档案",
        icon: FolderCheck,
      },
      {
        id: "failed",
        label: "失败",
        title: "异常诊断",
        description: "需要人工处理或重新归档的文件",
        icon: AlertTriangle,
        badge: "failed",
      },
      {
        id: "tv",
        label: "剧集",
        title: "剧集缺口",
        description: "按剧集追踪缺季、缺集与未播状态",
        icon: Tv,
        badge: "missing",
      },
      {
        id: "collections",
        label: "合集",
        title: "合集补齐",
        description: "追踪电影合集和榜单的本地完整度",
        icon: Library,
        badge: "missing",
      },
    ],
  },
  {
    label: "自动化",
    items: [
      {
        id: "logs",
        label: "日志",
        title: "排障时间线",
        description: "查看 AI、播放、STRM、扫描与整理日志",
        icon: History,
      },
      {
        id: "classification",
        label: "分类",
        title: "分类规则",
        description: "编辑影响归档路径的 YAML 分类规则",
        icon: Tags,
      },
      {
        id: "templates",
        label: "命名",
        title: "命名模板",
        description: "维护电影、剧集和合集的目标路径模板",
        icon: FileCode2,
      },
    ],
  },
  {
    label: "连接器",
    items: [
      {
        id: "settings",
        label: "设置",
        title: "连接器工作台",
        description: "配置本地目录、刮削源、云端、115 与 Emby",
        icon: Settings,
      },
    ],
  },
];

export const navItems = navGroups.flatMap((group) => group.items);

export const pageStorageKey = "curio.newui.page";

export function isPage(value: string | null): value is Page {
  return Boolean(value && navItems.some((item) => item.id === value));
}

export function pageMeta(page: Page) {
  return navItems.find((item) => item.id === page) ?? navItems[0];
}

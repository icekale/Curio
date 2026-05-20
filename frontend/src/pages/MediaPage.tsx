import { AlertTriangle, Archive, Activity } from "lucide-react";
import { Card } from "../components/Card";
import { MediaList, type MediaMode } from "../components/MediaTable";
import { TableSearch } from "../components/TableSearch";
import type { CurioConsole } from "../hooks/useCurioConsole";

export function MediaPage({
  console,
  mode,
}: {
  console: CurioConsole;
  mode: MediaMode;
}) {
  const config = {
    processing: {
      title: "处理流水线",
      eyebrow: "Processing",
      description: "扫描、解析、识别、匹配、规划中的媒体档案。",
      icon: Activity,
      page: console.mediaPage,
      query: console.mediaQuery,
      setQuery: console.setMediaQuery,
      offset: console.mediaOffset,
      setOffset: console.setMediaOffset,
      selected: console.selectedMedia,
      setSelected: console.setSelectedMedia,
    },
    staging: {
      title: "归档完成",
      eyebrow: "Staging",
      description: "这些文件已完成重命名，可以进入目标媒体库。",
      icon: Archive,
      page: console.stagingPage,
      query: console.stagingQuery,
      setQuery: console.setStagingQuery,
      offset: console.stagingOffset,
      setOffset: console.setStagingOffset,
      selected: console.selectedStaging,
      setSelected: console.setSelectedStaging,
    },
    failed: {
      title: "异常诊断",
      eyebrow: "Failed",
      description: "失败文件需要重新匹配、修正规则或人工介入。",
      icon: AlertTriangle,
      page: console.failedPage,
      query: console.failedQuery,
      setQuery: console.setFailedQuery,
      offset: console.failedOffset,
      setOffset: console.setFailedOffset,
      selected: console.selectedFailed,
      setSelected: console.setSelectedFailed,
    },
  }[mode];
  const Icon = config.icon;

  return (
    <>
      <Card
        variant="hero"
        className={mode === "failed" ? "pageHero failedHero" : "pageHero"}
      >
        <div>
          <span className="eyebrow">{config.eyebrow}</span>
          <h2>{config.title}</h2>
          <p>{config.description}</p>
        </div>
        <div className="heroStat">
          <Icon size={24} />
          <strong>{config.page.total}</strong>
          <span>条记录</span>
        </div>
      </Card>
      <Card
        title={config.title}
        eyebrow="Archive Table"
        action={<TableSearch value={config.query} onChange={config.setQuery} />}
      >
        <MediaList
          page={config.page}
          mode={mode}
          offset={config.offset}
          setOffset={config.setOffset}
          selected={config.selected}
          setSelected={config.setSelected}
          onDelete={console.deleteMediaRecords}
          onRearchive={console.openRearchive}
          busy={console.busy}
        />
      </Card>
    </>
  );
}

import { useRef, useState } from "react";
import { RefreshCw, SlidersHorizontal, X } from "lucide-react";
import { endpoints } from "../api";
import { Card } from "../components/Card";
import { Modal } from "../components/Modal";
import { Pager } from "../components/Pager";
import { PartState, StatusPill } from "../components/StatusPill";
import { TableSearch } from "../components/TableSearch";
import type { Collection, CollectionMovie, CollectionPage } from "../types";
import type { CollectionStatusFilter } from "../hooks/useCurioConsole";

const collectionStatusOptions: {
  value: CollectionStatusFilter;
  label: string;
}[] = [
  { value: "all", label: "全部" },
  { value: "incomplete", label: "未完整" },
  { value: "complete", label: "完整" },
];

export function CollectionsPage({
  page,
  query,
  setQuery,
  statusFilter,
  setStatusFilter,
  offset,
  setOffset,
  onRepairComplete,
  onRefreshCurated,
  busy,
}: {
  page: CollectionPage;
  query: string;
  setQuery: (value: string) => void;
  statusFilter: CollectionStatusFilter;
  setStatusFilter: (value: CollectionStatusFilter) => void;
  offset: number;
  setOffset: (value: number) => void;
  onRepairComplete: () => void;
  onRefreshCurated: () => void;
  busy: boolean;
}) {
  const rows = page.items ?? [];
  const [selected, setSelected] = useState<Collection | null>(null);
  const [detail, setDetail] = useState<Collection | null>(null);
  const [loading, setLoading] = useState(false);
  const detailRequestRef = useRef(0);

  const openCollection = async (item: Collection) => {
    const requestID = detailRequestRef.current + 1;
    detailRequestRef.current = requestID;
    setSelected(item);
    setDetail(null);
    setLoading(true);
    try {
      const next =
        item.kind === "curated_list"
          ? await endpoints.curatedCollection(item.id ?? String(item.tmdb_id))
          : await endpoints.collection(item.tmdb_id);
      if (detailRequestRef.current === requestID) setDetail(next);
    } catch {
      if (detailRequestRef.current === requestID) setDetail({ ...item, parts: [] });
    } finally {
      if (detailRequestRef.current === requestID) setLoading(false);
    }
  };

  const active = detail ?? selected;

  return (
    <>
      <Card
        variant="hero"
        className="pageHero"
      >
        <div>
          <span className="eyebrow">Collections</span>
          <h2>合集补齐</h2>
          <p>追踪 TMDB 合集和豆瓣榜单的本地完整度，优先处理缺失电影。</p>
        </div>
        <div className="inlineActions">
          <button
            className="secondaryButton"
            onClick={onRepairComplete}
            disabled={busy}
            title="修复已完整但仍在缺失目录的合集"
            type="button"
          >
            <RefreshCw size={16} />
            <span>修复完整合集</span>
          </button>
          <button
            className="secondaryButton"
            onClick={onRefreshCurated}
            disabled={busy}
            title="刷新豆瓣电影 Top250 榜单明细"
            type="button"
          >
            <RefreshCw size={16} />
            <span>刷新豆瓣 Top250</span>
          </button>
        </div>
      </Card>

      <Card
        title="合集状态"
        eyebrow="Collection Ledger"
        action={
          <div className="tableActions">
            <label className="selectWithIcon">
              <SlidersHorizontal size={16} aria-hidden="true" />
              <select
                aria-label="合集完整性筛选"
                value={statusFilter}
                onChange={(event) =>
                  setStatusFilter(event.target.value as CollectionStatusFilter)
                }
              >
                {collectionStatusOptions.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label>
            <TableSearch value={query} onChange={setQuery} />
          </div>
        }
      >
        <div className="tableFrame">
          <table className="dataTable collectionsTable">
            <thead>
              <tr>
                <th>合集</th>
                <th>来源 ID</th>
                <th>已上映/榜单</th>
                <th>本地</th>
                <th>缺失</th>
                <th>未上映/未解析</th>
                <th>状态</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((item) => {
                const isCurated = item.kind === "curated_list";
                const unresolved = item.unresolved_count ?? item.unreleased_count;
                const missing = isCurated
                  ? Math.max(item.movie_count - unresolved - item.local_count, 0)
                  : Math.max(item.movie_count - item.local_count, 0);
                return (
                  <tr
                    className="clickableRow"
                    key={`${item.kind ?? "tmdb_collection"}:${item.id ?? item.tmdb_id}`}
                    tabIndex={0}
                    onClick={() => openCollection(item)}
                    onKeyDown={(event) => {
                      if (event.key === "Enter" || event.key === " ") openCollection(item);
                    }}
                  >
                    <td>
                      <div className="mediaName">
                        <b>{item.name}</b>
                        <small>{isCurated ? item.source || "curated" : "TMDB Collection"}</small>
                      </div>
                    </td>
                    <td>{isCurated ? (item.id ?? "-") : item.tmdb_id}</td>
                    <td>{item.movie_count}</td>
                    <td>{item.local_count}</td>
                    <td>{missing}</td>
                    <td>{isCurated ? unresolved : item.unreleased_count}</td>
                    <td>
                      <StatusPill value={item.status} />
                    </td>
                  </tr>
                );
              })}
              {rows.length === 0 && (
                <tr>
                  <td className="emptyCell" colSpan={7}>
                    暂无数据
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        <Pager page={page} offset={offset} setOffset={setOffset} />
      </Card>

      <CollectionDetailModal
        collection={active}
        loading={loading}
        busy={busy}
        onRefreshCurated={onRefreshCurated}
        onClose={() => {
          detailRequestRef.current += 1;
          setSelected(null);
          setDetail(null);
          setLoading(false);
        }}
      />
    </>
  );
}

function CollectionDetailModal({
  collection,
  loading,
  busy,
  onRefreshCurated,
  onClose,
}: {
  collection: Collection | null;
  loading: boolean;
  busy: boolean;
  onRefreshCurated: () => void;
  onClose: () => void;
}) {
  const parts = collection?.parts ?? [];
  const isCurated = collection?.kind === "curated_list";
  const isResolved = (item: CollectionMovie) => item.resolved ?? item.movie_tmdb_id > 0;
  const local = parts.filter((item) => item.local);
  const missing = parts.filter((item) =>
    isCurated ? isResolved(item) && !item.local : item.released && !item.local,
  );
  const unreleased = parts.filter((item) =>
    isCurated ? !isResolved(item) : !item.released,
  );
  const unresolvedCount = collection?.unresolved_count ?? collection?.unreleased_count ?? 0;
  const hasDetail = parts.length > 0;

  return (
    <Modal
      open={Boolean(collection)}
      eyebrow={
        collection
          ? isCurated
            ? collection.source || collection.id || "curated"
            : `TMDB ${collection.tmdb_id}`
          : ""
      }
      title={collection?.name ?? "合集详情"}
      className="collectionModal"
      onClose={onClose}
      footer={
        <button className="secondaryButton" onClick={onClose} type="button">
          <X size={17} />
          <span>关闭</span>
        </button>
      }
    >
      {collection && (
        <>
          <div className="collectionSummary">
            <Summary label={isCurated ? "榜单电影" : "已上映"} value={collection.movie_count} />
            <Summary label="本地已有" value={hasDetail ? local.length : collection.local_count} />
            <Summary
              label="缺失"
              value={
                hasDetail
                  ? missing.length
                  : isCurated
                    ? Math.max(collection.movie_count - unresolvedCount - collection.local_count, 0)
                    : Math.max(collection.movie_count - collection.local_count, 0)
              }
            />
            <Summary
              label={isCurated ? "未解析" : "未上映"}
              value={hasDetail ? unreleased.length : unresolvedCount}
            />
          </div>
          {loading && <div className="modalLoading">正在读取合集电影</div>}
          {!loading && !hasDetail && (
            <div className="modalLoading">
              <span>暂无电影明细</span>
              {isCurated && (
                <button
                  className="secondaryButton"
                  onClick={onRefreshCurated}
                  disabled={busy}
                  title="刷新榜单"
                  type="button"
                >
                  <RefreshCw size={16} />
                  <span>刷新榜单</span>
                </button>
              )}
            </div>
          )}
          {!loading && hasDetail && (
            <div className="partGroups">
              <CollectionPartGroup title="缺失电影" tone="danger" items={missing} />
              <CollectionPartGroup title="本地已有" tone="success" items={local} />
              <CollectionPartGroup
                title={isCurated ? "待匹配" : "未上映"}
                tone="neutral"
                items={unreleased}
                idleLabel={isCurated ? "待匹配" : "未上映"}
              />
            </div>
          )}
        </>
      )}
    </Modal>
  );
}

function CollectionPartGroup({
  title,
  tone,
  items,
  idleLabel = "未上映",
}: {
  title: string;
  tone: "success" | "danger" | "neutral";
  items: CollectionMovie[];
  idleLabel?: string;
}) {
  return (
    <section className="partGroup">
      <div className="partGroupTitle">
        <span>{title}</span>
        <b>{items.length}</b>
      </div>
      <div className="partList">
        {items.map((item) => (
          <article className="partItem" key={`${item.douban_id ?? item.movie_tmdb_id}:${item.sort_order}`}>
            <div>
              <b>{item.title}</b>
              <small>
                {item.movie_tmdb_id > 0
                  ? `TMDB ${item.movie_tmdb_id}`
                  : item.douban_id
                    ? `Douban ${item.douban_id}`
                    : "TMDB -"}
                {item.rating ? ` · ${item.rating}` : ""}
                {item.year ? ` · ${item.year}` : ""}
                {item.release_date ? ` · ${item.release_date}` : ""}
              </small>
              {item.file_path && <em>{item.file_path}</em>}
            </div>
            <PartState tone={tone}>
              {tone === "success" ? "已有" : tone === "danger" ? "缺失" : idleLabel}
            </PartState>
          </article>
        ))}
        {items.length === 0 && <div className="partEmpty">无</div>}
      </div>
    </section>
  );
}

function Summary({ label, value }: { label: string; value: number }) {
  return (
    <div>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

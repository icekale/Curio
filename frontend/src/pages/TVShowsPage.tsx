import { useRef, useState } from "react";
import { X } from "lucide-react";
import { endpoints } from "../api";
import { Card } from "../components/Card";
import { Modal } from "../components/Modal";
import { Pager } from "../components/Pager";
import { PartState, StatusPill } from "../components/StatusPill";
import { TableSearch } from "../components/TableSearch";
import type { TVSeason, TVShow, TVShowPage } from "../types";
import { twoDigit } from "../utils/format";

export function TVShowsPage({
  page,
  query,
  setQuery,
  offset,
  setOffset,
}: {
  page: TVShowPage;
  query: string;
  setQuery: (value: string) => void;
  offset: number;
  setOffset: (value: number) => void;
}) {
  const rows = page.items ?? [];
  const [selected, setSelected] = useState<TVShow | null>(null);
  const [detail, setDetail] = useState<TVShow | null>(null);
  const [loading, setLoading] = useState(false);
  const detailRequestRef = useRef(0);

  const openShow = async (item: TVShow) => {
    const requestID = detailRequestRef.current + 1;
    detailRequestRef.current = requestID;
    setSelected(item);
    setDetail(null);
    setLoading(true);
    try {
      const next = await endpoints.tvShow(item.tmdb_id);
      if (detailRequestRef.current === requestID) setDetail(next);
    } catch {
      if (detailRequestRef.current === requestID) setDetail({ ...item, seasons: [] });
    } finally {
      if (detailRequestRef.current === requestID) setLoading(false);
    }
  };

  const active = detail ?? selected;

  return (
    <>
      <Card variant="hero" className="pageHero">
        <div>
          <span className="eyebrow">TV Library</span>
          <h2>剧集缺口</h2>
          <p>按剧集追踪缺季、缺集和未播状态，快速确认哪些剧集需要补齐。</p>
        </div>
        <div className="heroStat">
          <strong>{page.total}</strong>
          <span>部剧集</span>
        </div>
      </Card>

      <Card
        title="剧集状态"
        eyebrow="Show Ledger"
        action={<TableSearch value={query} onChange={setQuery} />}
      >
        <div className="tableFrame">
          <table className="dataTable showsTable">
            <thead>
              <tr>
                <th>剧集</th>
                <th>TMDB</th>
                <th>完成率</th>
                <th>本地</th>
                <th>缺季</th>
                <th>缺集</th>
                <th>未播</th>
                <th>状态</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((item) => {
                const ratio = item.released_episode_count
                  ? Math.round((item.local_episode_count / item.released_episode_count) * 100)
                  : 0;
                return (
                  <tr
                    className="clickableRow"
                    key={item.tmdb_id}
                    tabIndex={0}
                    onClick={() => openShow(item)}
                    onKeyDown={(event) => {
                      if (event.key === "Enter" || event.key === " ") openShow(item);
                    }}
                  >
                    <td>
                      <div className="mediaName">
                        <b>{item.name}</b>
                        <small>{item.year || item.first_air_date || "年份未知"}</small>
                      </div>
                    </td>
                    <td>{item.tmdb_id}</td>
                    <td>
                      <div className="completionCell">
                        <div className="miniTrack">
                          <i style={{ width: `${Math.min(100, ratio)}%` }} />
                        </div>
                        <span>{ratio}%</span>
                      </div>
                    </td>
                    <td>{item.local_episode_count}</td>
                    <td>{item.missing_season_count}</td>
                    <td>{item.missing_episode_count}</td>
                    <td>{item.unreleased_episode_count}</td>
                    <td>
                      <StatusPill value={item.status} />
                    </td>
                  </tr>
                );
              })}
              {rows.length === 0 && (
                <tr>
                  <td className="emptyCell" colSpan={8}>
                    暂无数据
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        <Pager page={page} offset={offset} setOffset={setOffset} />
      </Card>

      <TVShowDetailModal
        show={active}
        loading={loading}
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

function TVShowDetailModal({
  show,
  loading,
  onClose,
}: {
  show: TVShow | null;
  loading: boolean;
  onClose: () => void;
}) {
  const seasons = show?.seasons ?? [];
  const hasDetail = seasons.length > 0;
  return (
    <Modal
      open={Boolean(show)}
      eyebrow={show ? `TMDB ${show.tmdb_id}` : ""}
      title={show?.name ?? "剧集详情"}
      className="collectionModal"
      onClose={onClose}
      footer={
        <button className="secondaryButton" onClick={onClose} type="button">
          <X size={17} />
          <span>关闭</span>
        </button>
      }
    >
      {show && (
        <>
          <div className="collectionSummary tvSummary">
            <Summary label="已上映" value={show.released_episode_count} />
            <Summary label="本地已有" value={show.local_episode_count} />
            <Summary label="缺失季" value={show.missing_season_count} />
            <Summary label="缺失集" value={show.missing_episode_count} />
            <Summary label="未播" value={show.unreleased_episode_count} />
          </div>
          {loading && <div className="modalLoading">正在读取剧集季集</div>}
          {!loading && !hasDetail && <div className="modalLoading">暂无季集明细</div>}
          {!loading && hasDetail && (
            <div className="partGroups">
              {seasons.map((season) => (
                <TVSeasonGroup key={season.season} season={season} />
              ))}
            </div>
          )}
        </>
      )}
    </Modal>
  );
}

function TVSeasonGroup({ season }: { season: TVSeason }) {
  const episodes = season.episodes ?? [];
  return (
    <section className="partGroup">
      <div className="partGroupTitle">
        <span>第 {season.season} 季</span>
        <b>
          已有 {season.local_episode_count} / 缺失 {season.missing_episode_count} / 未播{" "}
          {season.unreleased_episode_count}
        </b>
      </div>
      <div className="partList">
        {episodes.map((episode) => {
          const tone = episode.local ? "success" : episode.released ? "danger" : "neutral";
          return (
            <article className="partItem" key={episode.id || `${episode.season}-${episode.episode}`}>
              <div>
                <b>
                  S{twoDigit(episode.season)}E{twoDigit(episode.episode)}{" "}
                  {episode.title || `第 ${episode.episode} 集`}
                </b>
                <small>
                  TMDB {episode.tmdb_id || "-"}
                  {episode.air_date ? ` · ${episode.air_date}` : ""}
                </small>
                {episode.file_path && <em>{episode.file_path}</em>}
              </div>
              <PartState tone={tone}>
                {tone === "success" ? "已有" : tone === "danger" ? "缺失" : "未播"}
              </PartState>
            </article>
          );
        })}
        {episodes.length === 0 && <div className="partEmpty">无</div>}
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

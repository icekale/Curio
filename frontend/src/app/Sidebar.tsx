import { navGroups, type Page } from "./navigation";
import type { MediaStats } from "../types";

export function Sidebar({
  page,
  setPage,
  stats,
  active,
}: {
  page: Page;
  setPage: (page: Page) => void;
  stats: MediaStats;
  active: boolean;
}) {
  return (
    <aside className="sidebar">
      <div className="brandPanel">
        <img className="brandIcon" src="/curio-icon.svg" alt="" aria-hidden="true" />
        <div>
          <strong>Curio</strong>
          <span>Archive Console</span>
        </div>
      </div>
      <div className="systemBadge">
        <span className={active ? "pulseDot running" : "pulseDot"} />
        <div>
          <b>{active ? "整理任务运行中" : "档案库待命"}</b>
          <small>失败 {stats.failed} · 缺集 {stats.missing_tv_episode_count}</small>
        </div>
      </div>
      <nav className="sidebarNav" aria-label="主导航">
        {navGroups.map((group) => (
          <div className="navGroup" key={group.label}>
            <div className="navGroupLabel">{group.label}</div>
            <div className="navGroupItems">
              {group.items.map((item) => {
                const Icon = item.icon;
                const badge =
                  item.badge === "failed"
                    ? stats.failed
                    : item.badge === "missing"
                      ? stats.missing_tv_episode_count +
                        stats.missing_tv_season_count +
                        stats.incomplete_collection
                      : 0;
                return (
                  <button
                    className={page === item.id ? "navItem active" : "navItem"}
                    key={item.id}
                    onClick={() => setPage(item.id)}
                    title={item.title}
                    type="button"
                  >
                    <Icon size={18} />
                    <span>{item.label}</span>
                    {badge > 0 && <em>{badge > 99 ? "99+" : badge}</em>}
                  </button>
                );
              })}
            </div>
          </div>
        ))}
      </nav>
    </aside>
  );
}

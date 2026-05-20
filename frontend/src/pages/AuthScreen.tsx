import { LogIn, RefreshCw } from "lucide-react";

export function AuthScreen({
  token,
  setToken,
  onSubmit,
  busy,
  loading = false,
}: {
  token: string;
  setToken: (value: string) => void;
  onSubmit: () => void;
  busy: boolean;
  loading?: boolean;
}) {
  return (
    <main className="authShell">
      <section className="authCard">
        <div className="authBrand">
          <img className="brandIcon" src="/curio-icon.svg" alt="" aria-hidden="true" />
          <div>
            <strong>Curio</strong>
            <span>整理、识别、归档你的媒体宇宙。</span>
          </div>
        </div>
        {loading ? (
          <div className="authLoading">
            <RefreshCw size={18} className="spinIcon" />
            <span>正在连接档案库</span>
          </div>
        ) : (
          <>
            <label className="field">
              <span>管理令牌</span>
              <input
                value={token}
                type="password"
                autoComplete="current-password"
                spellCheck={false}
                onChange={(event) => setToken(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter") onSubmit();
                }}
              />
            </label>
            <button
              className="primaryButton authSubmit"
              onClick={onSubmit}
              disabled={busy}
              type="button"
            >
              <LogIn size={17} />
              <span>进入 Curio</span>
            </button>
          </>
        )}
      </section>
    </main>
  );
}

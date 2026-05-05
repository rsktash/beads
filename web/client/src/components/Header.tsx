import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { api, writeToken } from "../lib/api";

// Header: shows the project prefix, current user, and backend driver. One-shot
// fetch of /api/me cached forever within the session.
export function Header() {
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: api.me });

  const onLogout = async () => {
    try { await api.logout(); } catch {}
    writeToken("");
    location.reload();
  };

  return (
    <header
      className="sticky top-0 z-10"
      style={{
        background: "var(--color-bg-elevated)",
        borderBottom: "1px solid var(--color-border-subtle)",
      }}
    >
      <div className="mx-auto max-w-screen-2xl px-4 h-12 flex items-center gap-6 text-sm">
        <Link
          to="/"
          className="font-semibold"
          style={{ color: "var(--color-ink-primary)" }}
        >bd</Link>
        <nav className="flex gap-4" style={{ color: "var(--color-ink-tertiary)" }}>
          <NavLink to="/">board</NavLink>
          <NavLink to="/list">list</NavLink>
          {me?.driver === "postgres" && <NavLink to="/projects">projects</NavLink>}
        </nav>

        <div className="ml-auto flex items-center gap-4" style={{ color: "var(--color-ink-tertiary)" }}>
          {me ? (
            <>
              <KV label="project" value={me.project.prefix || "—"} />
              <KV label="db" value={me.driver} />
              <KV label="user" value={me.user.username} />
              {me.user.role !== "Anonymous" && (
                <span className="text-xs">({me.user.role})</span>
              )}
              {me.auth_enabled && (
                <button
                  onClick={onLogout}
                  className="text-xs hover:opacity-80"
                  style={{ color: "var(--color-accent)" }}
                >logout</button>
              )}
            </>
          ) : (
            <span style={{ color: "var(--color-ink-tertiary)" }}>connecting…</span>
          )}
        </div>
      </div>
    </header>
  );
}

function NavLink({ to, children }: { to: string; children: React.ReactNode }) {
  return (
    <Link
      to={to as never}
      className="hover:opacity-80"
      activeProps={{ style: { color: "var(--color-ink-primary)" } }}
    >
      {children}
    </Link>
  );
}

function KV({ label, value }: { label: string; value: string }) {
  return (
    <span title={label}>
      <span style={{ color: "var(--color-ink-tertiary)" }}>{label}:</span>{" "}
      <span className="font-mono" style={{ color: "var(--color-ink-secondary)" }}>{value}</span>
    </span>
  );
}

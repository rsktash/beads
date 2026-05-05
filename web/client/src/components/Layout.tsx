import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { api, writeToken } from "../lib/api";

const BoardIcon = () => (
  <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor"
       strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
    <rect x="1" y="1" width="5" height="6" rx="1" />
    <rect x="10" y="1" width="5" height="9" rx="1" />
    <rect x="1" y="9" width="5" height="6" rx="1" />
    <rect x="10" y="12" width="5" height="3" rx="1" />
  </svg>
);

const ListIcon = () => (
  <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor"
       strokeWidth="1.5" strokeLinecap="round">
    <line x1="5" y1="4" x2="14" y2="4" />
    <line x1="5" y1="8" x2="14" y2="8" />
    <line x1="5" y1="12" x2="14" y2="12" />
    <circle cx="2" cy="4" r="0.75" fill="currentColor" stroke="none" />
    <circle cx="2" cy="8" r="0.75" fill="currentColor" stroke="none" />
    <circle cx="2" cy="12" r="0.75" fill="currentColor" stroke="none" />
  </svg>
);

const ProjectsIcon = () => (
  <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor"
       strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
    <path d="M2 4l6-2 6 2" />
    <path d="M2 8l6-2 6 2" />
    <path d="M2 12l6-2 6 2" />
  </svg>
);

const SignOutIcon = () => (
  <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor"
       strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
    <path d="M6 2H3a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h3" />
    <path d="M11 11l3-3-3-3" />
    <line x1="6" y1="8" x2="14" y2="8" />
  </svg>
);

const LogoIcon = () => (
  <svg width="14" height="14" viewBox="0 0 14 14" fill="white">
    <circle cx="7" cy="4" r="2.5" />
    <circle cx="4" cy="10" r="2.5" />
    <circle cx="10" cy="10" r="2.5" />
  </svg>
);

function NavLink({
  to,
  label,
  icon,
}: {
  to: string;
  label: string;
  icon: ReactNode;
}) {
  return (
    <Link
      to={to as never}
      className="flex items-center gap-2.5 px-3 py-2 rounded-md text-sm transition-colors"
      style={{ color: "var(--color-ink-secondary)" }}
      activeProps={{
        style: {
          background: "var(--color-bg-hover)",
          color: "var(--color-ink-primary)",
          fontWeight: 500,
        },
      }}
    >
      {icon}
      {label}
    </Link>
  );
}

export function Layout({ children }: { children: ReactNode }) {
  const me = useQuery({ queryKey: ["me"], queryFn: api.me });

  const onLogout = async () => {
    try { await api.logout(); } catch {}
    writeToken("");
    location.reload();
  };

  const showProjects = me.data?.driver === "postgres";

  return (
    <div className="flex h-screen" style={{ background: "var(--color-bg-base)" }}>
      <nav
        className="flex flex-col"
        style={{
          width: 220,
          borderRight: "1px solid var(--color-border-subtle)",
          background: "var(--color-bg-base)",
        }}
      >
        {/* logo */}
        <div className="flex items-center gap-2.5 px-4 py-5">
          <span
            className="flex items-center justify-center rounded-md"
            style={{ width: 28, height: 28, background: "var(--color-accent)" }}
          >
            <LogoIcon />
          </span>
          <span
            className="font-bold"
            style={{ fontSize: 16, color: "var(--color-ink-primary)" }}
          >
            bd
          </span>
        </div>

        {/* project name */}
        {me.data?.project.prefix && (
          <div
            className="px-4 pb-3 mb-1"
            style={{ borderBottom: "1px solid var(--color-border-subtle)" }}
          >
            <span
              className="text-xs font-mono"
              style={{ color: "var(--color-ink-tertiary)" }}
            >
              {me.data.project.prefix}
            </span>
            <span
              className="ml-2 text-[10px]"
              style={{ color: "var(--color-ink-tertiary)", opacity: 0.7 }}
            >
              ({me.data.driver})
            </span>
          </div>
        )}

        {/* nav */}
        <div className="px-2 space-y-0.5">
          <NavLink to="/" label="Board" icon={<BoardIcon />} />
          <NavLink to="/list" label="List" icon={<ListIcon />} />
          {showProjects && <NavLink to="/projects" label="Projects" icon={<ProjectsIcon />} />}
        </div>

        <div className="flex-1" />

        {/* user card */}
        {me.data && (
          <div
            className="mx-3 mb-3 px-3 py-2.5 rounded-md flex items-center gap-2.5"
            style={{ border: "1px solid var(--color-border-subtle)" }}
          >
            <span
              className="flex items-center justify-center rounded-full text-xs font-bold shrink-0"
              style={{
                width: 28,
                height: 28,
                background: "var(--color-accent)",
                color: "white",
              }}
            >
              {(me.data.user.username[0] ?? "?").toUpperCase()}
            </span>
            <div className="flex-1 min-w-0">
              <div
                className="text-sm font-medium truncate"
                style={{ color: "var(--color-ink-primary)" }}
              >
                {me.data.user.username}
              </div>
              {me.data.user.role !== "Anonymous" && (
                <div
                  style={{ fontSize: 11, color: "var(--color-ink-tertiary)" }}
                >
                  {me.data.user.role}
                </div>
              )}
            </div>
            {me.data.auth_enabled && (
              <button
                onClick={onLogout}
                className="shrink-0 p-1 rounded transition-colors"
                style={{ color: "var(--color-ink-tertiary)" }}
                title="Sign out"
              >
                <SignOutIcon />
              </button>
            )}
          </div>
        )}
      </nav>
      <main className="flex-1 overflow-auto">{children}</main>
    </div>
  );
}

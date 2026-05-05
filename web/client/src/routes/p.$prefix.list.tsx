import { createFileRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useState, useMemo } from "react";
import { api } from "../lib/api";
import { Link } from "../lib/router";
import { PriorityBadge, StatusBadge, TypeBadge } from "../components/badges";
import { CopyId } from "../components/CopyId";
import { getAvatarColor, getInitials } from "../lib/avatar";

const STATUS_OPTS = ["", "open", "in_progress", "blocked", "closed", "pinned"];
const TYPE_OPTS = ["", "task", "bug", "epic", "feature", "message", "wisp", "molecule", "role", "event"];

const SearchIcon = () => (
  <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor"
       strokeWidth="1.5" strokeLinecap="round">
    <circle cx="7" cy="7" r="5" />
    <line x1="11" y1="11" x2="14" y2="14" />
  </svg>
);

function Toolbar({
  search, setSearch,
  status, setStatus,
  type, setType,
}: {
  search: string; setSearch: (v: string) => void;
  status: string; setStatus: (v: string) => void;
  type: string; setType: (v: string) => void;
}) {
  return (
    <div
      className="flex items-center gap-3 mb-4 px-4 py-3 rounded-lg"
      style={{
        background: "var(--color-bg-elevated)",
        border: "1px solid var(--color-border-subtle)",
        boxShadow: "var(--shadow-sm)",
      }}
    >
      <div className="relative flex-1 max-w-xs">
        <span
          className="absolute left-3 top-1/2 -translate-y-1/2"
          style={{ color: "var(--color-ink-tertiary)" }}
        >
          <SearchIcon />
        </span>
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search issues…"
          className="w-full pl-9 pr-3 py-1.5 text-sm rounded-md outline-none"
          style={{
            border: "1px solid var(--color-border-default)",
            background: "var(--color-bg-base)",
            color: "var(--color-ink-primary)",
          }}
        />
      </div>
      <Select value={status} onChange={setStatus} options={STATUS_OPTS} placeholder="All statuses" />
      <Select value={type} onChange={setType} options={TYPE_OPTS} placeholder="All types" />
    </div>
  );
}

function Select({
  value, onChange, options, placeholder,
}: {
  value: string; onChange: (v: string) => void; options: string[]; placeholder: string;
}) {
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="px-3 py-1.5 text-sm rounded-md"
      style={{
        border: "1px solid var(--color-border-default)",
        background: "var(--color-bg-base)",
        color: "var(--color-ink-primary)",
      }}
    >
      {options.map((o) => (
        <option key={o} value={o}>{o || placeholder}</option>
      ))}
    </select>
  );
}

function SkeletonRow() {
  return (
    <div
      className="flex items-center gap-3 px-4 py-3"
      style={{ borderBottom: "1px solid var(--color-border-subtle)" }}
    >
      <div className="w-32 h-3 rounded skeleton-shimmer" />
      <div className="w-20 h-5 rounded-full skeleton-shimmer" />
      <div className="w-10 h-5 rounded skeleton-shimmer" />
      <div className="flex-1 h-3 rounded skeleton-shimmer" />
    </div>
  );
}

function ListComponent() {
  const [status, setStatus] = useState("");
  const [type, setType] = useState("");
  const [search, setSearch] = useState("");

  const params: Record<string, string> = {};
  if (status) params.status = status;
  if (type) params.type = type;

  const { prefix } = Route.useParams();
  const q = useQuery({
    queryKey: ["issues", prefix, params],
    queryFn: () => api.listIssues(params),
    refetchInterval: 60_000, // SSE pushes invalidations; this is a safety fallback
  });

  const issues = useMemo(() => {
    const all = q.data?.issues ?? [];
    if (!search) return all;
    const lc = search.toLowerCase();
    return all.filter((i) =>
      i.title.toLowerCase().includes(lc) ||
      i.id.toLowerCase().includes(lc) ||
      (i.assignee || "").toLowerCase().includes(lc),
    );
  }, [q.data, search]);

  return (
    <div>
      <div className="mb-4">
        <h1 className="text-xl font-bold" style={{ color: "var(--color-ink-primary)" }}>
          List
        </h1>
        <p className="text-sm mt-0.5" style={{ color: "var(--color-ink-tertiary)" }}>
          {q.isLoading ? "loading…" : `${issues.length} of ${q.data?.issues.length ?? 0} issues`}
        </p>
      </div>

      <Toolbar
        search={search} setSearch={setSearch}
        status={status} setStatus={setStatus}
        type={type} setType={setType}
      />

      <div
        className="rounded-lg overflow-hidden"
        style={{
          background: "var(--color-bg-elevated)",
          border: "1px solid var(--color-border-subtle)",
          boxShadow: "var(--shadow-sm)",
        }}
      >
        {q.isLoading ? (
          <>
            <SkeletonRow /><SkeletonRow /><SkeletonRow /><SkeletonRow /><SkeletonRow />
          </>
        ) : (
          <table className="w-full text-sm">
            <thead style={{ background: "color-mix(in srgb, var(--color-bg-surface) 60%, transparent)" }}>
              <tr style={{ color: "var(--color-ink-tertiary)" }}>
                <Th>id</Th>
                <Th>status</Th>
                <Th>p</Th>
                <Th>type</Th>
                <Th>title</Th>
                <Th>assignee</Th>
              </tr>
            </thead>
            <tbody>
              {issues.map((i) => (
                <tr
                  key={i.id}
                  className="hover:bg-stone-50"
                  style={{ borderTop: "1px solid var(--color-border-subtle)" }}
                >
                  <Td>
                    <span className="inline-flex items-center gap-2">
                      <Link
                        to="/issue/$id"
                        params={{ id: i.id }}
                        className="font-mono text-xs"
                        style={{ color: "var(--color-ink-secondary)" }}
                      >
                        {i.id}
                      </Link>
                      <CopyId id={i.id} className="text-xs" />
                    </span>
                  </Td>
                  <Td><StatusBadge status={i.status} /></Td>
                  <Td><PriorityBadge priority={i.priority} /></Td>
                  <Td><TypeBadge type={i.issue_type} /></Td>
                  <Td>
                    <span style={{ color: "var(--color-ink-primary)" }}>{i.title}</span>
                  </Td>
                  <Td>
                    {i.assignee ? (
                      <span className="inline-flex items-center gap-1.5">
                        <span
                          className="flex items-center justify-center rounded-full text-[10px] font-bold"
                          style={{
                            width: 20, height: 20,
                            background: getAvatarColor(i.assignee),
                            color: "white",
                          }}
                        >
                          {getInitials(i.assignee)}
                        </span>
                        <span className="text-xs" style={{ color: "var(--color-ink-secondary)" }}>
                          {i.assignee}
                        </span>
                      </span>
                    ) : (
                      <span style={{ color: "var(--color-ink-tertiary)" }}>—</span>
                    )}
                  </Td>
                </tr>
              ))}
              {issues.length === 0 && (
                <tr>
                  <td colSpan={6} className="py-8 text-center text-sm" style={{ color: "var(--color-ink-tertiary)" }}>
                    No issues match.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}

function Th({ children }: { children: React.ReactNode }) {
  return (
    <th className="text-left px-3 py-2 text-[11px] uppercase font-semibold tracking-wide">
      {children}
    </th>
  );
}

function Td({ children }: { children: React.ReactNode }) {
  return <td className="px-3 py-2">{children}</td>;
}

export const Route = createFileRoute("/p/$prefix/list")({ component: ListComponent });

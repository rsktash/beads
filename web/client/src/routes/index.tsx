import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import type { Issue } from "../lib/types";
import { PriorityBadge, StatusBadge, TypeBadge } from "../components/badges";

// "/" — board view: simple kanban-ish grouping by status. Shows ready issues
// in a separate column at the top.
function BoardComponent() {
  const issues = useQuery({
    queryKey: ["issues"],
    queryFn: () => api.listIssues({}),
    refetchInterval: 5000,
  });
  const ready = useQuery({
    queryKey: ["issues", "ready"],
    queryFn: api.ready,
    refetchInterval: 5000,
  });

  if (issues.isLoading) return <div className="text-stone-500">loading…</div>;
  if (issues.error) return <div className="text-red-600">{(issues.error as Error).message}</div>;

  const all = issues.data?.issues ?? [];
  const byStatus = group(all, (i) => i.status);

  return (
    <div className="space-y-6">
      <Column title="ready (no open blockers)" issues={ready.data?.issues ?? []} />
      <div className="grid grid-cols-1 md:grid-cols-3 lg:grid-cols-5 gap-4">
        <Column title="open" issues={byStatus.get("open") ?? []} />
        <Column title="in_progress" issues={byStatus.get("in_progress") ?? []} />
        <Column title="blocked" issues={byStatus.get("blocked") ?? []} />
        <Column title="pinned" issues={byStatus.get("pinned") ?? []} />
        <Column title="closed" issues={byStatus.get("closed") ?? []} />
      </div>
    </div>
  );
}

function Column({ title, issues }: { title: string; issues: Issue[] }) {
  return (
    <div
      className="rounded-md"
      style={{
        background: "var(--color-bg-surface)",
        border: "1px solid var(--color-border-subtle)",
      }}
    >
      <div
        className="px-3 py-2 text-xs uppercase tracking-wide font-mono"
        style={{
          color: "var(--color-ink-tertiary)",
          borderBottom: "1px solid var(--color-border-subtle)",
        }}
      >
        {title} <span style={{ opacity: 0.6 }}>({issues.length})</span>
      </div>
      <div className="p-2 space-y-2 min-h-[80px]">
        {issues.map((i) => (
          <Card key={i.id} issue={i} />
        ))}
      </div>
    </div>
  );
}

function Card({ issue }: { issue: Issue }) {
  return (
    <Link
      to="/issue/$id"
      params={{ id: issue.id }}
      className="issue-card block p-2.5 hover:no-underline"
    >
      <div className="flex items-center gap-2 text-xs">
        <span className="font-mono" style={{ color: "var(--color-ink-tertiary)" }}>{issue.id}</span>
        <PriorityBadge priority={issue.priority} />
        <TypeBadge type={issue.issue_type} />
      </div>
      <div className="text-sm mt-1.5" style={{ color: "var(--color-ink-primary)" }}>{issue.title}</div>
      {issue.assignee && (
        <div className="text-xs mt-1" style={{ color: "var(--color-ink-tertiary)" }}>@{issue.assignee}</div>
      )}
    </Link>
  );
}

function group<T, K>(xs: T[], key: (x: T) => K): Map<K, T[]> {
  const m = new Map<K, T[]>();
  for (const x of xs) {
    const k = key(x);
    const arr = m.get(k);
    if (arr) arr.push(x); else m.set(k, [x]);
  }
  return m;
}

// inline status badge import for type, only used to build the column titles
// (not currently rendered). kept here in case a follow-up wants it.
export const __unused_StatusBadge = StatusBadge;

export const Route = createFileRoute("/")({ component: BoardComponent });

import { createFileRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "../lib/api";
import { IssueCard } from "../components/IssueCard";
import type { Issue } from "../lib/types";

const COLUMN_PAGE_SIZE = 20;

const STATUS_DOTS: Record<string, string> = {
  open: "var(--color-status-open)",
  in_progress: "var(--color-status-in-progress)",
  blocked: "var(--color-status-blocked)",
  closed: "var(--color-status-closed)",
};

function Column({
  title,
  statusKey,
  issues,
  dimmed = false,
}: {
  title: string;
  statusKey: string;
  issues: Issue[];
  dimmed?: boolean;
}) {
  const [limit, setLimit] = useState(COLUMN_PAGE_SIZE);
  const visible = issues.slice(0, limit);
  const remaining = Math.max(0, issues.length - visible.length);
  const dotColor = STATUS_DOTS[statusKey] ?? "var(--color-ink-tertiary)";

  return (
    <div className="flex-1 min-w-[280px] max-w-[360px] flex flex-col self-start">
      {/* column header */}
      <div
        className="flex items-center gap-2.5 px-1 pb-3 mb-3"
        style={{ borderBottom: "1px solid var(--color-border-subtle)" }}
      >
        <span
          className="rounded-full shrink-0"
          style={{
            width: 10,
            height: 10,
            backgroundColor: dotColor,
            boxShadow: `0 0 0 3px color-mix(in srgb, ${dotColor} 20%, transparent)`,
          }}
        />
        <h2 className="text-sm font-semibold" style={{ color: "var(--color-ink-primary)" }}>
          {title}
        </h2>
        <span
          className="text-[11px] font-medium px-2 py-0.5 rounded-full ml-auto"
          style={{ background: "rgba(0,0,0,0.05)", color: "var(--color-ink-tertiary)" }}
        >
          {issues.length}
        </span>
      </div>

      {/* card list */}
      <div className="space-y-2 pr-1">
        {issues.length === 0 ? (
          <div
            className="py-8 text-center"
            style={{
              border: "2px dashed var(--color-border-default)",
              borderRadius: "var(--radius-md)",
              minHeight: 100,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
            }}
          >
            <p className="text-xs" style={{ color: "var(--color-ink-tertiary)" }}>
              No items
            </p>
          </div>
        ) : (
          visible.map((i) => <IssueCard key={i.id} issue={i} dimmed={dimmed} />)
        )}
        {remaining > 0 && (
          <button
            onClick={() => setLimit((l) => l + COLUMN_PAGE_SIZE)}
            className="w-full text-xs py-2"
            style={{ color: "var(--color-ink-secondary)" }}
          >
            Show {remaining} more…
          </button>
        )}
      </div>
    </div>
  );
}

function BoardComponent() {
  const all = useQuery({
    queryKey: ["issues"],
    queryFn: () => api.listIssues({}),
    refetchInterval: 5000,
  });
  const ready = useQuery({
    queryKey: ["issues", "ready"],
    queryFn: api.ready,
    refetchInterval: 5000,
  });

  const issues = all.data?.issues ?? [];
  const epics = issues.filter((i) => i.issue_type === "epic").length;
  const byStatus = group(issues, (i) => i.status);

  return (
    <div>
      <div className="mb-6">
        <h1 className="text-xl font-bold" style={{ color: "var(--color-ink-primary)" }}>
          Board
        </h1>
        <p className="text-sm mt-0.5" style={{ color: "var(--color-ink-tertiary)" }}>
          {all.isLoading ? "loading…" : `${issues.length} issues across ${epics} epics`}
        </p>
      </div>
      <div className="flex gap-4 overflow-x-auto items-start">
        <Column title="Open" statusKey="open" issues={ready.data?.issues ?? []} />
        <Column title="In Progress" statusKey="in_progress" issues={byStatus.get("in_progress") ?? []} />
        <Column title="Blocked" statusKey="blocked" issues={byStatus.get("blocked") ?? []} />
        <Column title="Closed" statusKey="closed" issues={byStatus.get("closed") ?? []} dimmed />
      </div>
    </div>
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

export const Route = createFileRoute("/")({ component: BoardComponent });

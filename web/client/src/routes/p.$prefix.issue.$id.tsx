import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { api } from "../lib/api";
import { Link } from "../lib/router";
import type { Comment, Issue } from "../lib/types";
import { PriorityBadge, StatusBadge, TypeBadge } from "../components/badges";
import { CopyId } from "../components/CopyId";
import { Markdown } from "../components/Markdown";
import { TableOfContents } from "../components/TableOfContents";
import { getAvatarColor, getInitials } from "../lib/avatar";

function IssueDetail() {
  const { id } = Route.useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  // Callback-state ref so children that depend on the scroll container
  // (TableOfContents) re-render when it mounts. A plain useRef wouldn't
  // trigger their effect because React doesn't re-render on `.current`
  // mutation.
  const [scrollEl, setScrollEl] = useState<HTMLDivElement | null>(null);

  const me = useQuery({ queryKey: ["me"], queryFn: api.me });
  const q = useQuery({
    queryKey: ["issue", id],
    queryFn: () => api.getIssue(id),
    refetchInterval: 5000,
  });

  // Scroll to fragment after first render (e.g. /issue/foo#section).
  useEffect(() => {
    if (!q.data || !scrollEl) return;
    const hash = window.location.hash.slice(1);
    if (!hash) return;
    const t = setTimeout(() => {
      const el = scrollEl.querySelector<HTMLElement>(`#${cssEsc(hash)}`);
      el?.scrollIntoView({ behavior: "smooth", block: "start" });
    }, 100);
    return () => clearTimeout(t);
  }, [q.data, scrollEl]);

  const onBack = () => {
    if (window.history.length > 1) window.history.back();
    else navigate({ to: "/" });
  };

  const addComment = useMutation({
    mutationFn: (text: string) => api.addComment(id, text),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["issue", id] }),
  });
  const addLabel = useMutation({
    mutationFn: (label: string) => api.addLabel(id, label),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["issue", id] }),
  });
  const removeLabel = useMutation({
    mutationFn: (label: string) => api.removeLabel(id, label),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["issue", id] }),
  });

  if (q.isLoading) return <div style={{ color: "var(--color-ink-tertiary)" }}>loading…</div>;
  if (q.error)     return <div className="text-red-600">{(q.error as Error).message}</div>;
  if (!q.data)     return null;

  const { issue, labels, dependencies, comments, blocked_by, children } = q.data;

  return (
    <div className="flex h-full -m-6">
      {/* TOC on the LEFT (hidden below xl), matches upstream order */}
      <div className="shrink-0 hidden xl:block w-56 p-6 pr-0">
        <TableOfContents container={scrollEl} />
      </div>

      <div ref={setScrollEl} className="flex-1 overflow-y-auto p-6 space-y-5 min-w-0">
        {/* breadcrumbs */}
        <div
          className="flex items-center gap-2 text-sm"
          style={{ color: "var(--color-ink-tertiary)" }}
        >
          <button
            onClick={onBack}
            className="flex items-center gap-1 transition-colors hover:opacity-100"
            style={{ color: "var(--color-ink-tertiary)" }}
          >
            ← Back
          </button>
          <span>/</span>
          <CopyId id={issue.id} />
        </div>

        {/* parent breadcrumb */}
        {issue.parent_id && (
          <Link
            to="/issue/$id"
            params={{ id: issue.parent_id }}
            className="block text-xs"
            style={{ color: "var(--color-ink-tertiary)" }}
          >
            ↑ {issue.parent_id}
            {issue.parent_title && (
              <span className="ml-2" style={{ opacity: 0.7 }}>
                {issue.parent_title}
              </span>
            )}
          </Link>
        )}

        {/* title */}
        <h1
          className="text-2xl font-bold"
          style={{ color: "var(--color-ink-primary)" }}
        >
          {issue.title}
        </h1>

        <Section title="Description" body={issue.description}
                 attachmentBaseUrl={me.data?.file_attachment_base_url} />
        <Section title="Acceptance Criteria" body={issue.acceptance_criteria}
                 attachmentBaseUrl={me.data?.file_attachment_base_url} />
        <Section title="Notes" body={issue.notes}
                 attachmentBaseUrl={me.data?.file_attachment_base_url} />
        <Section title="Design" body={issue.design}
                 attachmentBaseUrl={me.data?.file_attachment_base_url} />

        {/* dependencies (full list — different from sidebar's blocked-by) */}
        {dependencies.length > 0 && (
          <Card label="Dependencies">
            <ul className="space-y-1 text-sm">
              {dependencies.map((d) => {
                const inbound = d.depends_on_id === id;
                const otherId = inbound ? d.issue_id : d.depends_on_id;
                return (
                  <li key={`${d.issue_id}->${d.depends_on_id}-${d.type}`} className="font-mono">
                    {inbound ? "← " : "→ "}
                    <span style={{ color: "var(--color-ink-tertiary)" }}>{d.type}</span>{" "}
                    <Link
                      to="/issue/$id"
                      params={{ id: otherId }}
                      style={{ color: "var(--color-accent)" }}
                    >
                      {otherId}
                    </Link>
                  </li>
                );
              })}
            </ul>
          </Card>
        )}

        {/* comments */}
        <Comments
          issue={issue}
          comments={comments}
          attachmentBaseUrl={me.data?.file_attachment_base_url}
          onAdd={(text) => addComment.mutate(text)}
          submitting={addComment.isPending}
        />
      </div>

      {/* right metadata sidebar */}
      <MetadataSidebar
        issue={issue}
        labels={labels}
        blocked_by={blocked_by}
        children_={children}
        onAddLabel={(l) => addLabel.mutate(l)}
        onRemoveLabel={(l) => removeLabel.mutate(l)}
      />
    </div>
  );
}

function Section({
  title, body, attachmentBaseUrl,
}: {
  title: string; body: string;
  attachmentBaseUrl?: string;
}) {
  const { prefix } = useParams({ strict: false }) as { prefix?: string };
  if (!body) return null;
  return (
    <div>
      <h2
        id={slugify(title)}
        className="text-[11px] uppercase font-semibold tracking-wider mb-2"
        style={{ color: "var(--color-ink-tertiary)" }}
      >
        {title}
      </h2>
      <Markdown content={body} attachmentBaseUrl={attachmentBaseUrl} prefix={prefix} />
    </div>
  );
}

function Card({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div
      className="rounded-lg p-4"
      style={{
        background: "var(--color-bg-elevated)",
        border: "1px solid var(--color-border-subtle)",
        boxShadow: "var(--shadow-card)",
      }}
    >
      <h3
        className="text-[11px] uppercase font-semibold tracking-wider mb-3"
        style={{ color: "var(--color-ink-tertiary)" }}
      >
        {label}
      </h3>
      {children}
    </div>
  );
}

function Comments({
  issue, comments, attachmentBaseUrl, onAdd, submitting,
}: {
  issue: Issue; comments: Comment[];
  attachmentBaseUrl?: string;
  onAdd: (text: string) => void; submitting: boolean;
}) {
  void issue;
  const { prefix } = useParams({ strict: false }) as { prefix?: string };
  const [text, setText] = useState("");
  const submit = () => {
    const t = text.trim();
    if (!t) return;
    onAdd(t);
    setText("");
  };
  return (
    <Card label={`Comments${comments.length ? ` (${comments.length})` : ""}`}>
      {comments.length > 0 && (
        <ul className="space-y-4 mb-4">
          {comments.map((c) => (
            <li key={c.id} className="flex gap-3">
              <span
                className="flex-shrink-0 w-7 h-7 rounded-full flex items-center justify-center text-[10px] font-bold"
                style={{ background: getAvatarColor(c.author), color: "white" }}
                title={c.author}
              >
                {getInitials(c.author)}
              </span>
              <div className="flex-1 min-w-0">
                <div className="flex items-baseline gap-2">
                  <span className="text-sm font-semibold" style={{ color: "var(--color-ink-primary)" }}>
                    {c.author || "anon"}
                  </span>
                  <span className="text-xs" style={{ color: "var(--color-ink-tertiary)" }}>
                    {c.created_at ? new Date(c.created_at).toLocaleString() : ""}
                  </span>
                </div>
                <Markdown content={c.text} attachmentBaseUrl={attachmentBaseUrl} prefix={prefix} />
              </div>
            </li>
          ))}
        </ul>
      )}
      <textarea
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder="Add a comment… (Ctrl+Enter to submit)"
        rows={3}
        className="w-full p-2 text-sm rounded-md resize-y outline-none"
        style={{
          border: "1px solid var(--color-border-default)",
          background: "var(--color-bg-base)",
          color: "var(--color-ink-primary)",
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) submit();
        }}
      />
      <div className="mt-2 flex justify-end">
        <button
          onClick={submit}
          disabled={submitting || !text.trim()}
          className="px-3 py-1.5 text-sm rounded-md font-medium"
          style={{
            background: "var(--color-bg-hover)",
            color: "var(--color-ink-primary)",
            border: "1px solid var(--color-border-default)",
            opacity: submitting || !text.trim() ? 0.6 : 1,
          }}
        >
          {submitting ? "Posting…" : "Add Comment"}
        </button>
      </div>
    </Card>
  );
}

function MetadataSidebar({
  issue, labels, blocked_by, children_, onAddLabel, onRemoveLabel,
}: {
  issue: Issue;
  labels: string[];
  blocked_by: { id: string; title: string }[];
  children_: { id: string; title: string; status: string; priority: number; issue_type: string }[];
  onAddLabel: (l: string) => void;
  onRemoveLabel: (l: string) => void;
}) {
  const [newLabel, setNewLabel] = useState("");
  const submitLabel = () => {
    const v = newLabel.trim();
    if (!v) return;
    onAddLabel(v);
    setNewLabel("");
  };
  return (
    <aside
      className="shrink-0 p-4 space-y-3 overflow-y-auto"
      style={{
        width: 280,
        borderLeft: "1px solid var(--color-border-subtle)",
        background: "var(--color-bg-base)",
      }}
    >
      <Meta label="Status">
        <StatusBadge status={issue.status} />
      </Meta>
      {issue.status === "closed" && issue.close_reason && (
        <Meta label="Close Reason">
          <p className="text-sm" style={{ color: "var(--color-ink-secondary)" }}>
            {issue.close_reason}
          </p>
        </Meta>
      )}
      <Meta label="Priority">
        <PriorityBadge priority={issue.priority} />
      </Meta>
      <Meta label="Type">
        <TypeBadge type={issue.issue_type} />
      </Meta>
      <Meta label="Assignee">
        {issue.assignee ? (
          <span className="flex items-center gap-2">
            <span
              className="w-6 h-6 rounded-full flex items-center justify-center text-[10px] font-bold"
              style={{ background: getAvatarColor(issue.assignee), color: "white" }}
            >
              {getInitials(issue.assignee)}
            </span>
            <span className="text-sm" style={{ color: "var(--color-ink-primary)" }}>
              {issue.assignee}
            </span>
          </span>
        ) : (
          <span className="text-sm" style={{ color: "var(--color-ink-tertiary)" }}>Unassigned</span>
        )}
      </Meta>
      {issue.due_at && (
        <Meta label="Due">
          <span className="text-sm" style={{ color: "var(--color-ink-secondary)" }}>
            {new Date(issue.due_at).toLocaleDateString()}
          </span>
        </Meta>
      )}
      <Meta label="Updated">
        <span className="text-sm" style={{ color: "var(--color-ink-secondary)" }}>
          {issue.updated_at ? new Date(issue.updated_at).toLocaleDateString() : "—"}
        </span>
      </Meta>

      <Meta label="Labels">
        <div className="flex flex-wrap gap-1">
          {labels.map((l) => (
            <span
              key={l}
              className="px-2 py-0.5 text-xs rounded inline-flex items-center gap-1"
              style={{ background: "var(--color-bg-hover)", color: "var(--color-ink-secondary)" }}
            >
              {l}
              <button
                onClick={() => onRemoveLabel(l)}
                className="hover:opacity-70 leading-none"
                title="remove"
                style={{ color: "var(--color-ink-tertiary)" }}
              >×</button>
            </span>
          ))}
          {labels.length === 0 && (
            <span className="text-xs" style={{ color: "var(--color-ink-tertiary)" }}>
              No labels
            </span>
          )}
        </div>
        <input
          value={newLabel}
          onChange={(e) => setNewLabel(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter") submitLabel(); }}
          placeholder="Add label…"
          className="mt-2 w-full px-2 py-1 text-xs rounded outline-none"
          style={{
            border: "1px solid var(--color-border-default)",
            background: "var(--color-bg-base)",
            color: "var(--color-ink-primary)",
          }}
        />
      </Meta>

      {issue.parent_id && (
        <Meta label="Parent">
          <Link
            to="/issue/$id"
            params={{ id: issue.parent_id }}
            className="block rounded px-1 py-1 -mx-1"
          >
            <span className="font-mono text-xs" style={{ color: "var(--color-ink-tertiary)" }}>
              {issue.parent_id}
            </span>
            {issue.parent_title && (
              <p className="text-xs leading-snug pl-0.5 line-clamp-2" style={{ color: "var(--color-ink-secondary)" }}>
                {issue.parent_title}
              </p>
            )}
          </Link>
        </Meta>
      )}

      {children_ && children_.length > 0 && (
        <Meta label={`Children (${issue.closed_children}/${issue.total_children})`}>
          <ul className="space-y-1">
            {children_.map((c) => (
              <li key={c.id}>
                <Link
                  to="/issue/$id"
                  params={{ id: c.id }}
                  className="block rounded px-1 py-1 -mx-1"
                  style={{ opacity: c.status === "closed" ? 0.6 : 1 }}
                >
                  <div className="flex items-center gap-1.5 mb-0.5">
                    <StatusBadge status={c.status} />
                    <span
                      className="font-mono text-[11px] truncate"
                      style={{ color: "var(--color-ink-tertiary)" }}
                    >
                      {c.id}
                    </span>
                  </div>
                  {c.title && (
                    <p
                      className="text-xs leading-snug pl-0.5 line-clamp-2"
                      style={{ color: "var(--color-ink-secondary)" }}
                    >
                      {c.title}
                    </p>
                  )}
                </Link>
              </li>
            ))}
          </ul>
        </Meta>
      )}

      {blocked_by && blocked_by.length > 0 && (
        <Meta label="Blocked By">
          <div className="space-y-1">
            {blocked_by.map((b) => (
              <Link
                key={b.id}
                to="/issue/$id"
                params={{ id: b.id }}
                className="block text-xs font-mono"
                style={{ color: "var(--color-accent)" }}
                title={b.title}
              >
                {b.id}
              </Link>
            ))}
          </div>
        </Meta>
      )}
    </aside>
  );
}

function Meta({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div
      className="px-3 py-2.5 rounded-md"
      style={{ border: "1px solid var(--color-border-subtle)" }}
    >
      <h3
        className="text-[11px] uppercase font-semibold tracking-wider mb-1.5"
        style={{ color: "var(--color-ink-tertiary)" }}
      >
        {label}
      </h3>
      {children}
    </div>
  );
}

function slugify(s: string): string {
  return s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
}
function cssEsc(s: string): string {
  if (typeof CSS !== "undefined" && (CSS as any).escape) return (CSS as any).escape(s);
  return s.replace(/([^a-zA-Z0-9_-])/g, "\\$1");
}

export const Route = createFileRoute("/p/$prefix/issue/$id")({ component: IssueDetail });

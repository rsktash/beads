import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { PriorityBadge, StatusBadge, TypeBadge } from "../components/badges";
import { useMemo } from "react";
import { marked } from "marked";
import DOMPurify from "dompurify";

function IssueDetail() {
  const { id } = Route.useParams();
  const q = useQuery({
    queryKey: ["issue", id],
    queryFn: () => api.getIssue(id),
    refetchInterval: 5000,
  });

  if (q.isLoading) return <div className="text-stone-500">loading…</div>;
  if (q.error) return <div className="text-red-600">{(q.error as Error).message}</div>;
  if (!q.data) return null;
  const { issue, labels, dependencies, comments } = q.data;

  return (
    <div className="max-w-4xl space-y-6">
      <div className="space-y-2">
        <div className="flex items-center gap-2 text-xs">
          <span className="font-mono" style={{ color: "var(--color-ink-tertiary)" }}>{issue.id}</span>
          <PriorityBadge priority={issue.priority} />
          <StatusBadge status={issue.status} />
          <TypeBadge type={issue.issue_type} />
          {issue.assignee && (
            <span style={{ color: "var(--color-ink-tertiary)" }}>@{issue.assignee}</span>
          )}
        </div>
        <h1 className="text-2xl font-semibold" style={{ color: "var(--color-ink-primary)" }}>
          {issue.title}
        </h1>
        {labels.length > 0 && (
          <div className="flex gap-1.5 text-xs">
            {labels.map((l) => (
              <span key={l} className="bg-stone-100 text-stone-700 rounded px-2 py-0.5">{l}</span>
            ))}
          </div>
        )}
      </div>

      <Section title="description" body={issue.description} />
      <Section title="design" body={issue.design} />
      <Section title="acceptance criteria" body={issue.acceptance_criteria} />
      <Section title="notes" body={issue.notes} />

      {dependencies.length > 0 && (
        <div>
          <h2 className="text-sm font-semibold text-stone-700 mb-2">dependencies</h2>
          <ul className="space-y-1 text-sm">
            {dependencies.map((d) => {
              const inbound = d.depends_on_id === id;
              const otherId = inbound ? d.issue_id : d.depends_on_id;
              return (
                <li key={`${d.issue_id}->${d.depends_on_id}-${d.type}`} className="font-mono text-stone-700">
                  {inbound ? "← " : "→ "} <span className="text-stone-500">{d.type}</span>{" "}
                  <Link to="/issue/$id" params={{ id: otherId }} className="text-blue-700 hover:underline">
                    {otherId}
                  </Link>
                </li>
              );
            })}
          </ul>
        </div>
      )}

      {comments.length > 0 && (
        <div>
          <h2 className="text-sm font-semibold text-stone-700 mb-2">comments</h2>
          <ul className="space-y-3">
            {comments.map((c) => (
              <li key={c.id} className="border-l-2 border-stone-200 pl-3">
                <div className="text-xs text-stone-500">
                  {c.author} · {c.created_at ? new Date(c.created_at).toLocaleString() : ""}
                </div>
                <div className="text-sm text-stone-800 whitespace-pre-wrap">{c.text}</div>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function Section({ title, body }: { title: string; body: string }) {
  const html = useMemo(() => {
    if (!body) return "";
    const md = marked.parse(body, { async: false }) as string;
    return DOMPurify.sanitize(md);
  }, [body]);
  if (!body) return null;
  return (
    <div>
      <h2 className="text-sm font-semibold text-stone-700 mb-2">{title}</h2>
      <div
        className="prose prose-sm max-w-none prose-stone"
        dangerouslySetInnerHTML={{ __html: html }}
      />
    </div>
  );
}

export const Route = createFileRoute("/issue/$id")({ component: IssueDetail });

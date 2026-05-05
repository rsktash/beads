import { useEffect, useMemo, useRef, useState } from "react";
import { useParams } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useNavigate } from "../lib/router";
import { PriorityBadge, StatusBadge, TypeBadge } from "./badges";

// Cmd+K / Ctrl+K opens a fuzzy search palette scoped to the active project.
// Disabled outside of /p/$prefix routes.
export function SearchDialog() {
  // Read prefix only to gate query enablement and keys; navigation goes
  // through the project-scoped useNavigate shim and doesn't need it.
  const params = useParams({ strict: false }) as { prefix?: string };
  const prefix = params.prefix || "";
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIdx, setActiveIdx] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const navigate = useNavigate();

  // global hotkey
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((o) => !o);
      } else if (e.key === "Escape") {
        setOpen(false);
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  useEffect(() => {
    if (open) {
      setQuery("");
      setActiveIdx(0);
      setTimeout(() => inputRef.current?.focus(), 0);
    }
  }, [open]);

  const all = useQuery({
    queryKey: ["issues", prefix, "search-all"],
    queryFn: () => api.listIssues({}),
    enabled: open && !!prefix,
  });

  const matches = useMemo(() => {
    const arr = all.data?.issues ?? [];
    const q = query.trim().toLowerCase();
    if (!q) return arr.slice(0, 25);
    const scored = arr
      .map((i) => ({ i, score: scoreMatch(q, i.id, i.title, i.assignee || "") }))
      .filter((x) => x.score > 0)
      .sort((a, b) => b.score - a.score)
      .slice(0, 25);
    return scored.map((x) => x.i);
  }, [all.data, query]);

  if (!open) return null;

  const close = () => setOpen(false);

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActiveIdx((i) => Math.min(i + 1, matches.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActiveIdx((i) => Math.max(i - 1, 0));
    } else if (e.key === "Enter") {
      const m = matches[activeIdx];
      if (m) {
        navigate({ to: "/issue/$id", params: { id: m.id } });
        close();
      }
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center pt-24"
      style={{ background: "rgba(0, 43, 54, 0.35)" }}
      onClick={close}
    >
      <div
        className="w-full max-w-2xl rounded-lg overflow-hidden"
        style={{
          background: "var(--color-bg-elevated)",
          border: "1px solid var(--color-border-subtle)",
          boxShadow: "var(--shadow-md)",
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <input
          ref={inputRef}
          value={query}
          onChange={(e) => { setQuery(e.target.value); setActiveIdx(0); }}
          onKeyDown={onKey}
          placeholder="Search issues by id, title, or assignee…"
          className="w-full px-4 py-3 text-sm outline-none"
          style={{
            background: "transparent",
            color: "var(--color-ink-primary)",
            borderBottom: "1px solid var(--color-border-subtle)",
          }}
        />
        <div className="max-h-[60vh] overflow-y-auto">
          {all.isLoading && (
            <div className="px-4 py-6 text-sm" style={{ color: "var(--color-ink-tertiary)" }}>
              loading…
            </div>
          )}
          {!all.isLoading && matches.length === 0 && (
            <div className="px-4 py-6 text-sm" style={{ color: "var(--color-ink-tertiary)" }}>
              No matches.
            </div>
          )}
          {matches.map((m, idx) => (
            <button
              key={m.id}
              onClick={() => {
                navigate({ to: "/issue/$id", params: { id: m.id } });
                close();
              }}
              onMouseEnter={() => setActiveIdx(idx)}
              className="w-full text-left px-4 py-2 flex items-center gap-3 text-sm"
              style={{
                background: idx === activeIdx ? "var(--color-bg-hover)" : "transparent",
                color: "var(--color-ink-primary)",
              }}
            >
              <span className="font-mono text-xs" style={{ color: "var(--color-ink-tertiary)" }}>
                {m.id}
              </span>
              <StatusBadge status={m.status} />
              <PriorityBadge priority={m.priority} />
              <TypeBadge type={m.issue_type} />
              <span className="flex-1 truncate">{m.title}</span>
            </button>
          ))}
        </div>
        <div
          className="px-4 py-2 text-[11px] flex items-center gap-3"
          style={{
            background: "var(--color-bg-surface)",
            color: "var(--color-ink-tertiary)",
          }}
        >
          <span>↑↓ navigate</span>
          <span>↵ open</span>
          <span>esc close</span>
          <span className="ml-auto">⌘K to toggle</span>
        </div>
      </div>
    </div>
  );
}

function scoreMatch(q: string, id: string, title: string, assignee: string): number {
  const idL = id.toLowerCase();
  const tl = title.toLowerCase();
  const al = assignee.toLowerCase();
  if (idL === q) return 1000;
  if (idL.startsWith(q)) return 500 + q.length * 5;
  if (tl.includes(q)) return 100 + (tl.startsWith(q) ? 50 : 0);
  if (idL.includes(q)) return 80;
  if (al.includes(q)) return 40;
  return 0;
}

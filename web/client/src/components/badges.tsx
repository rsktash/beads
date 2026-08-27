import type { Issue, Status } from "../lib/types";

// Visual style mirrors upstream beads-ui — small monospace pills, subdued
// background tint matched to a foreground colour from the @theme tokens
// (Solarized Light). color-mix gives consistent translucent bgs without
// hand-rolling rgba per token.

export function StatusBadge({ status }: { status: Status | string }) {
  const tone = statusColor(status);
  return (
    <span
      className="text-[11px] rounded px-1.5 py-0.5 font-mono"
      style={{
        background: `color-mix(in srgb, ${tone} 14%, transparent)`,
        color: tone,
      }}
    >
      {labelStatus(status)}
    </span>
  );
}

export function PriorityBadge({ priority }: { priority: number }) {
  const c = priorityColor(priority);
  return (
    <span
      className="text-[11px] rounded px-1.5 py-0.5 font-mono"
      style={{
        background: `color-mix(in srgb, ${c} 14%, transparent)`,
        color: c,
      }}
    >
      P{priority}
    </span>
  );
}

export function TypeBadge({ type }: { type: Issue["issue_type"] }) {
  const c = typeColor(type);
  return (
    <span
      className="text-[11px] rounded px-1.5 py-0.5 font-mono"
      style={{
        background: `color-mix(in srgb, ${c} 14%, transparent)`,
        color: c,
      }}
    >
      {type}
    </span>
  );
}

function statusColor(s: string) {
  switch (s) {
    case "open":         return "var(--color-status-open)";
    case "in_progress":  return "var(--color-status-in-progress)";
    case "blocked":      return "var(--color-status-blocked)";
    case "closed":       return "var(--color-status-closed)";
    case "pinned":       return "var(--color-status-pinned)";
    default:             return "var(--color-ink-tertiary)";
  }
}

function priorityColor(p: number) {
  switch (p) {
    case 0:  return "var(--color-priority-0)";
    case 1:  return "var(--color-priority-1)";
    case 2:  return "var(--color-priority-2)";
    case 3:  return "var(--color-priority-3)";
    default: return "var(--color-priority-4)";
  }
}

function typeColor(t: string) {
  switch (t) {
    case "task":    return "var(--color-type-task)";
    case "bug":     return "var(--color-type-bug)";
    case "feature": return "var(--color-type-feature)";
    case "epic":    return "var(--color-type-epic)";
    case "message": return "var(--color-type-message)";
    default:        return "var(--color-type-chore)";
  }
}

function labelStatus(s: string): string {
  if (s === "in_progress") return "in progress";
  return s;
}

// TYPE_BORDER_COLORS is the strong opaque variant used as the IssueCard
// left-edge accent (3px solid border) — same hues as upstream beads-ui.
export const TYPE_BORDER_COLORS: Record<string, string> = {
  epic:    "#7C3AED",
  feature: "#6366F1",
  bug:     "#EF4444",
  task:    "#16A34A",
  chore:   "#78716C",
  message: "#0EA5E9",
  wisp:    "#F59E0B",
  molecule: "#10B981",
  role:    "#A855F7",
  event:   "#06B6D4",
};

export function typeBorderColor(t: string): string {
  return TYPE_BORDER_COLORS[t] ?? "#78716C";
}

export function OriginChip({
  originKind,
  originIssueId,
}: {
  originKind: string;
  originIssueId: string | null;
}) {
  if (originKind === "self" || !originKind) return null;
  const isProject = originKind === "project";
  const tone = isProject ? "var(--color-accent)" : "var(--color-type-epic)";
  const label = isProject ? "project" : (originIssueId ?? originKind);
  return (
    <span
      className="text-[11px] rounded px-1.5 py-0.5 font-mono"
      style={{
        background: `color-mix(in srgb, ${tone} 14%, transparent)`,
        color: tone,
      }}
      title={isProject ? "project scope" : originIssueId ?? undefined}
    >
      {label}
    </span>
  );
}

export function OpenQuestionBadge({ count }: { count: number }) {
  const n = Number(count ?? 0);
  if (!n || n <= 0) return null;
  return (
    <span
      className="text-[11px] rounded px-1.5 py-0.5 font-mono inline-flex items-center gap-1"
      style={{
        background: `color-mix(in srgb, var(--color-status-blocked) 14%, transparent)`,
        color: "var(--color-status-blocked)",
      }}
      title={`${n} open question${n === 1 ? "" : "s"}`}
    >
      <QuestionIcon />
      {n}
    </span>
  );
}

const QuestionIcon = () => (
  <svg width="10" height="10" viewBox="0 0 16 16" fill="none" aria-hidden>
    <circle cx="8" cy="8" r="7" stroke="currentColor" strokeWidth="1.5" />
    <path d="M6.2 6.2c0-1.5 1.4-2.5 2.8-2.3 1 .15 1.9 1 2 2.1.1 1-.4 1.6-1 2.1l-.7.6c-.4.3-.7.7-.7 1.3" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
    <circle cx="8" cy="12.2" r="1" fill="currentColor" />
  </svg>
);

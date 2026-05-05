import type { Issue, Status } from "../lib/types";

// Color tokens come from @theme in src/index.css (Solarized Light) so badges
// match upstream beads-ui exactly. We render plain colored chips with the
// label inline; no emoji / icons.

export function StatusBadge({ status }: { status: Status | string }) {
  const color = statusColor(status);
  return (
    <span
      className="text-xs rounded-sm px-1.5 py-0.5 font-mono"
      style={{ background: color.bg, color: color.fg }}
    >
      {status}
    </span>
  );
}

export function PriorityBadge({ priority }: { priority: number }) {
  const c = priorityColor(priority);
  return (
    <span
      className="text-xs rounded-sm px-1.5 py-0.5 font-mono"
      style={{ background: c, color: "#fdf6e3" }}
    >
      p{priority}
    </span>
  );
}

export function TypeBadge({ type }: { type: Issue["issue_type"] }) {
  const c = typeColor(type);
  return (
    <span
      className="text-xs rounded-sm px-1.5 py-0.5 font-mono"
      style={{ background: alpha(c, 0.14), color: c }}
    >
      {type}
    </span>
  );
}

function statusColor(s: string) {
  const tone = (() => {
    switch (s) {
      case "open":         return "var(--color-status-open)";
      case "in_progress":  return "var(--color-status-in-progress)";
      case "blocked":      return "var(--color-status-blocked)";
      case "closed":       return "var(--color-status-closed)";
      case "pinned":       return "var(--color-status-pinned)";
      default:             return "var(--color-ink-tertiary)";
    }
  })();
  return { bg: `color-mix(in srgb, ${tone} 14%, transparent)`, fg: tone };
}

function priorityColor(p: number) {
  switch (p) {
    case 0: return "var(--color-priority-0)";
    case 1: return "var(--color-priority-1)";
    case 2: return "var(--color-priority-2)";
    case 3: return "var(--color-priority-3)";
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

// alpha is a CSS-side helper for fallback browsers that don't support
// color-mix(); kept here in case we need a JS-derived rgba later.
function alpha(_color: string, _a: number) {
  return `color-mix(in srgb, ${_color} ${Math.round(_a * 100)}%, transparent)`;
}

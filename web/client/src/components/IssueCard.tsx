import { Link } from "../lib/router";
import type { Issue } from "../lib/types";
import { PriorityBadge, TypeBadge, typeBorderColor } from "./badges";
import { CopyId } from "./CopyId";
import { getAvatarColor, getInitials } from "../lib/avatar";

// IssueCard mirrors upstream beads-ui:
//   • coloured 3px left border keyed to issue_type
//   • parent breadcrumb + monospace id at top
//   • title (line-clamp 2)
//   • optional "blocked by N" indicator
//   • bottom meta row: type + priority + children counter + comment counter
//     + assignee avatar (colour deterministic from name)
export function IssueCard({
  issue,
  dimmed = false,
}: {
  issue: Issue;
  dimmed?: boolean;
}) {
  const borderColor = typeBorderColor(issue.issue_type);
  return (
    <Link
      to="/issue/$id"
      params={{ id: issue.id }}
      className={`issue-card group block w-full text-left relative ${dimmed ? "issue-card--dimmed" : ""}`}
      style={{ borderLeft: `3px solid ${borderColor}` }}
    >
      <div className="px-3.5 py-3">
        {/* id row */}
        <div className="flex items-center gap-1 mb-1">
          {issue.parent_id && (
            <span
              className="text-[10px] truncate"
              style={{ color: "var(--color-ink-tertiary)", opacity: 0.7 }}
              title={issue.parent_title || issue.parent_id}
            >
              {issue.parent_id} ›
            </span>
          )}
          <CopyId id={issue.id} className="text-xs" />
        </div>

        {/* title */}
        <p
          className="text-sm font-medium line-clamp-2 mb-2.5"
          style={{ color: "var(--color-ink-primary)" }}
        >
          {issue.title}
        </p>

        {/* blocked-by indicator */}
        {issue.blocked_by_count > 0 && (
          <div className="flex items-center gap-1 mb-2">
            <BlockedIcon />
            <span
              className="text-[10px] px-1.5 py-0.5 rounded font-mono"
              style={{
                background: "color-mix(in srgb, var(--color-status-blocked) 12%, transparent)",
                color: "var(--color-status-blocked)",
              }}
            >
              blocked by {issue.blocked_by_count}
            </span>
          </div>
        )}

        {/* meta row */}
        <div className="flex items-center gap-1.5">
          <TypeBadge type={issue.issue_type} />
          <PriorityBadge priority={issue.priority} />
          {issue.total_children > 0 && (
            <MetaPill
              icon={<CheckBoxIcon />}
              label={`${issue.closed_children}/${issue.total_children}`}
              title={`${issue.closed_children} of ${issue.total_children} children closed`}
            />
          )}
          {issue.comment_count > 0 && (
            <MetaPill
              icon={<CommentIcon />}
              label={String(issue.comment_count)}
              title={`${issue.comment_count} comment${issue.comment_count === 1 ? "" : "s"}`}
            />
          )}
          <div className="flex-1" />
          {issue.assignee && (
            <span
              className="flex items-center justify-center rounded-full text-[10px] font-bold shrink-0"
              style={{
                width: 24,
                height: 24,
                background: getAvatarColor(issue.assignee),
                color: "var(--color-ink-inverse)",
              }}
              title={issue.assignee}
            >
              {getInitials(issue.assignee)}
            </span>
          )}
        </div>
      </div>
    </Link>
  );
}

function MetaPill({ icon, label, title }: { icon: React.ReactNode; label: string; title: string }) {
  return (
    <span
      className="text-[10px] px-1.5 py-0.5 rounded inline-flex items-center gap-1 font-mono"
      style={{
        background: "rgba(0,0,0,0.04)",
        color: "var(--color-ink-tertiary)",
      }}
      title={title}
    >
      {icon}
      {label}
    </span>
  );
}

const BlockedIcon = () => (
  <svg width="12" height="12" viewBox="0 0 16 16" fill="none"
       style={{ color: "var(--color-status-blocked)", flexShrink: 0 }}>
    <circle cx="8" cy="8" r="7" stroke="currentColor" strokeWidth="1.5" />
    <line x1="4" y1="8" x2="12" y2="8" stroke="currentColor" strokeWidth="1.5" />
  </svg>
);

const CheckBoxIcon = () => (
  <svg width="10" height="10" viewBox="0 0 16 16" fill="none">
    <rect x="2" y="2" width="12" height="12" rx="2" stroke="currentColor" strokeWidth="1.5" />
    <path d="M5 8.5L7 10.5L11 6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
  </svg>
);

const CommentIcon = () => (
  <svg width="10" height="10" viewBox="0 0 16 16" fill="none">
    <path d="M2.5 4a1.5 1.5 0 0 1 1.5-1.5h8A1.5 1.5 0 0 1 13.5 4v5a1.5 1.5 0 0 1-1.5 1.5H6.5L4 13v-2.5h-.5A1.5 1.5 0 0 1 2 9V4Z"
          stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" />
  </svg>
);

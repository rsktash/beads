// Deterministic avatar colours + initials, matching upstream beads-ui's feel.
// Same name always maps to the same colour so visual identity sticks per
// assignee across views.

const PALETTE = [
  "#0EA5E9", // sky
  "#10B981", // emerald
  "#F59E0B", // amber
  "#EF4444", // red
  "#A855F7", // purple
  "#06B6D4", // cyan
  "#22C55E", // green
  "#F97316", // orange
  "#6366F1", // indigo
  "#EC4899", // pink
  "#84CC16", // lime
  "#14B8A6", // teal
];

export function getAvatarColor(name: string): string {
  if (!name) return "#9CA3AF";
  let hash = 0;
  for (let i = 0; i < name.length; i++) {
    hash = (hash * 31 + name.charCodeAt(i)) >>> 0;
  }
  return PALETTE[hash % PALETTE.length];
}

export function getInitials(name: string): string {
  if (!name) return "?";
  const parts = name.trim().split(/[\s._-]+/).filter(Boolean);
  if (parts.length === 0) return name.slice(0, 1).toUpperCase();
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

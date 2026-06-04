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
  // Agent assignee convention: "Human / Model Family X.Y" with an optional
  // "(subagent)" suffix, e.g. "Rustam / Claude Opus 4.8 (subagent)" → "RO",
  // "Rustam / Claude Sonnet 4.6" → "RS". Plain names fall through below.
  const cleaned = name.replace(/\s*\([^)]*\)\s*$/, "").trim();
  const [human, model] = cleaned.split(/\s*\/\s*/, 2);
  if (human && model) {
    const family = model.replace(/^claude\s+/i, "").match(/[a-z]/i);
    if (family) return (human[0] + family[0]).toUpperCase();
  }
  const parts = cleaned.split(/[\s._-]+/).filter(Boolean);
  if (parts.length === 0) return name.trim().slice(0, 1).toUpperCase() || "?";
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

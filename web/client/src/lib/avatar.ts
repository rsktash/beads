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
  // FNV-1a with a murmur3-style finalizer. The old `hash * 31 + char` mod 12
  // had alternating character weights (31² ≡ 1 mod 12), so similar full names
  // — e.g. "X / Opus 4.8" vs "X / Opus 4.8 (subagent)" — collided into the
  // same palette bucket. The avalanche step makes every character count.
  let h = 0x811c9dc5;
  for (let i = 0; i < name.length; i++) {
    h ^= name.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  h ^= h >>> 16;
  h = Math.imul(h, 0x85ebca6b);
  h ^= h >>> 13;
  h = Math.imul(h, 0xc2b2ae35);
  h ^= h >>> 16;
  return PALETTE[(h >>> 0) % PALETTE.length];
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

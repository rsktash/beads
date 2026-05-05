// Mirrors beads.Issue / beads.Dependency / beads.Comment in the Go side
// (beads.go). Keep field names identical (snake_case) so the JSON shape is
// identical end-to-end.

export type Status = "open" | "in_progress" | "blocked" | "closed" | "pinned";

export type IssueType =
  | "task" | "bug" | "epic" | "feature"
  | "message" | "wisp" | "molecule" | "role" | "event";

export type DependencyType =
  | "blocks" | "related" | "duplicates" | "supersedes"
  | "replies-to" | "parent-child" | "discovered-by";

export interface Issue {
  id: string;
  content_hash: string;
  title: string;
  description: string;
  design: string;
  acceptance_criteria: string;
  notes: string;
  status: Status | string; // also accepts custom statuses
  priority: number;        // 0..4
  issue_type: IssueType | string;
  assignee: string;
  estimated_minutes: number;
  created_at: string | null;
  created_by: string;
  owner: string;
  updated_at: string | null;
  started_at: string | null;
  closed_at: string | null;
  closed_by_session: string;
  external_ref: string;
  spec_id: string;
  metadata: string;
  source_repo: string;
  source_system: string;
  close_reason: string;
  sender: string;
  ephemeral: boolean;
  pinned: boolean;
  is_template: boolean;
  wisp_type: string;
  mol_type: string;
  role_type: string;
  event_kind: string;
  actor: string;
  target: string;
  payload: string;
  due_at: string | null;
  defer_until: string | null;

  // server-enriched (always present in /api/issues responses):
  parent_id: string;
  parent_title: string;
  total_children: number;
  closed_children: number;
  blocked_by_count: number;
  comment_count: number;
}

export interface BlockedByEntry {
  id: string;
  title: string;
}

export interface Dependency {
  issue_id: string;
  depends_on_id: string;
  type: DependencyType | string;
  created_at: string | null;
  created_by: string;
  metadata: string;
  thread_id: string;
}

export interface Comment {
  id: string;
  issue_id: string;
  author: string;
  text: string;
  created_at: string | null;
}

export interface Me {
  project: { prefix: string; id_mode: "hash" | "counter" | string };
  user: { username: string; role: string };
  driver: "sqlite" | "postgres";
  auth_enabled: boolean;
  auth_fingerprint: string;
  bead_dir: string;
  version: string;
  file_attachment_base_url: string;
}

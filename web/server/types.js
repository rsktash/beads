// Row-shape helpers shared by the route handlers. Matches the public types
// in the Go side (beads.Issue, beads.Dependency, beads.Comment).

export function rowToIssue(r) {
  if (!r) return null;
  return {
    id: r.id,
    content_hash: r.content_hash || '',
    title: r.title,
    description: r.description || '',
    design: r.design || '',
    acceptance_criteria: r.acceptance_criteria || '',
    notes: r.notes || '',
    status: r.status,
    priority: Number(r.priority),
    issue_type: r.issue_type,
    assignee: r.assignee || '',
    estimated_minutes: Number(r.estimated_minutes || 0),
    created_at: toISO(r.created_at),
    created_by: r.created_by || '',
    owner: r.owner || '',
    updated_at: toISO(r.updated_at),
    started_at: toISO(r.started_at),
    closed_at: toISO(r.closed_at),
    closed_by_session: r.closed_by_session || '',
    external_ref: r.external_ref || '',
    spec_id: r.spec_id || '',
    metadata: r.metadata || '{}',
    source_repo: r.source_repo || '',
    source_system: r.source_system || '',
    close_reason: r.close_reason || '',
    sender: r.sender || '',
    ephemeral: !!Number(r.ephemeral),
    pinned: !!Number(r.pinned),
    is_template: !!Number(r.is_template),
    wisp_type: r.wisp_type || '',
    mol_type: r.mol_type || '',
    role_type: r.role_type || '',
    event_kind: r.event_kind || '',
    actor: r.actor || '',
    target: r.target || '',
    payload: r.payload || '',
    due_at: toISO(r.due_at),
    defer_until: toISO(r.defer_until),
    // Enriched fields populated by ENRICHED SELECT (queries.js). Optional —
    // detail/list endpoints set them; raw inserts won't have them.
    parent_id: r.parent_id || '',
    parent_title: r.parent_title || '',
    total_children: r.total_children != null ? Number(r.total_children) : 0,
    closed_children: r.closed_children != null ? Number(r.closed_children) : 0,
    blocked_by_count: r.blocked_by_count != null ? Number(r.blocked_by_count) : 0,
    blocked_by_id: r.blocked_by_id || '',
    blocked_by_title: r.blocked_by_title || '',
    comment_count: r.comment_count != null ? Number(r.comment_count) : 0,
  };
}

export function rowToDependency(r) {
  return {
    issue_id: r.issue_id,
    depends_on_id: r.depends_on_id,
    type: r.type,
    created_at: toISO(r.created_at),
    created_by: r.created_by || '',
    metadata: r.metadata || '{}',
    thread_id: r.thread_id || '',
  };
}

export function rowToComment(r) {
  return {
    id: r.id,
    issue_id: r.issue_id,
    author: r.author,
    text: r.text,
    created_at: toISO(r.created_at),
  };
}

function toISO(v) {
  if (!v) return null;
  if (v instanceof Date) return v.toISOString();
  // sqlite returns 'YYYY-MM-DD HH:MM:SS' or ISO already; postgres returns Date
  return new Date(v).toISOString();
}

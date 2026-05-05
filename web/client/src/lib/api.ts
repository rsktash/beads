// Multi-project API client. The active project is held inside the client
// itself; route layouts set it once via `api.use(prefix)` and every method
// after that hits /api/p/<prefix>/* without callers thinking about it.
//
// Global methods (me, login, logout, listProjects) sit alongside as plain
// functions — they don't need a project context.

import type { Comment, Dependency, Issue, Me } from "./types";

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = "ApiError";
  }
}

const TOKEN_KEY = "bd-web:token";
export function readToken(): string {
  return localStorage.getItem(TOKEN_KEY) || "";
}
export function writeToken(t: string) {
  if (t) localStorage.setItem(TOKEN_KEY, t);
  else localStorage.removeItem(TOKEN_KEY);
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { "content-type": "application/json" };
  const token = readToken();
  if (token) headers.authorization = `Bearer ${token}`;
  const res = await fetch(path, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) {
    let msg: string;
    try { msg = (await res.json()).error ?? `${res.status}`; }
    catch { msg = `${res.status}`; }
    if (res.status === 401) writeToken("");
    throw new ApiError(res.status, msg);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

class ProjectApi {
  private prefix = "";

  /** Layout routes call this on render to pin the active project. */
  use(prefix: string) {
    this.prefix = prefix;
  }

  /** Currently-active project. "" when not under /p/$prefix/. */
  current() {
    return this.prefix;
  }

  private path(suffix: string) {
    if (!this.prefix) throw new ApiError(0, "no active project — navigate via /projects");
    return `/api/p/${encodeURIComponent(this.prefix)}${suffix}`;
  }

  projectInfo = () =>
    call<{ project: { prefix: string; id_mode: string } }>("GET", this.path(`/me`));

  listIssues = (params: Record<string, string> = {}) => {
    const q = new URLSearchParams(params).toString();
    return call<{ issues: Issue[] }>("GET", this.path(`/issues${q ? "?" + q : ""}`));
  };
  ready = () => call<{ issues: Issue[] }>("GET", this.path(`/issues/ready`));

  getIssue = (id: string) =>
    call<{
      issue: Issue;
      labels: string[];
      dependencies: Dependency[];
      comments: Comment[];
      blocked_by: { id: string; title: string }[];
      children: { id: string; title: string; status: string; priority: number; issue_type: string }[];
    }>("GET", this.path(`/issues/${encodeURIComponent(id)}`));

  addComment = (issueId: string, text: string) =>
    call<{ comment: Comment }>("POST", this.path(`/issues/${encodeURIComponent(issueId)}/comments`), { text });
  deleteComment = (issueId: string, commentId: string) =>
    call<{ ok: true }>("DELETE", this.path(`/issues/${encodeURIComponent(issueId)}/comments/${encodeURIComponent(commentId)}`));
  addLabel = (issueId: string, label: string) =>
    call<{ ok: true; label: string }>("POST", this.path(`/issues/${encodeURIComponent(issueId)}/labels`), { label });
  removeLabel = (issueId: string, label: string) =>
    call<{ ok: true }>("DELETE", this.path(`/issues/${encodeURIComponent(issueId)}/labels/${encodeURIComponent(label)}`));
}

// `api` carries both the global methods and the project-scoped ones.
export const api = Object.assign(new ProjectApi(), {
  me: (): Promise<Me> => call("GET", "/api/me"),
  login: (username: string, password: string) =>
    call<{ token: string; user: { username: string; role: string } }>(
      "POST", "/api/auth/login", { username, password },
    ),
  logout: () => call<{ ok: true }>("POST", "/api/auth/logout"),
  listProjects: () => call<{ projects: { prefix: string }[] }>("GET", "/api/projects"),
});

// Tiny fetch-based API client. We keep it framework-agnostic so it's easy to
// drive from outside React (for tests / scripts).

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

async function call<T>(
  method: string, path: string, body?: unknown,
): Promise<T> {
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
    // A 401 mid-session means the token was wiped/expired. Drop the stale
    // token so the Login screen shows on next render instead of a hard
    // unauthorized message everywhere.
    if (res.status === 401) writeToken("");
    throw new ApiError(res.status, msg);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  me: (): Promise<Me> => call("GET", "/api/me"),
  login: (username: string, password: string) =>
    call<{ token: string; user: { username: string; role: string } }>(
      "POST", "/api/auth/login", { username, password },
    ),
  logout: () => call<{ ok: true }>("POST", "/api/auth/logout"),
  listIssues: (params: Record<string, string> = {}) => {
    const q = new URLSearchParams(params).toString();
    return call<{ issues: Issue[] }>("GET", `/api/issues${q ? "?" + q : ""}`);
  },
  ready: () => call<{ issues: Issue[] }>("GET", "/api/issues/ready"),
  getIssue: (id: string) =>
    call<{ issue: Issue; labels: string[]; dependencies: Dependency[]; comments: Comment[] }>(
      "GET", `/api/issues/${encodeURIComponent(id)}`,
    ),
  listProjects: () => call<{ projects: { prefix: string }[] }>("GET", "/api/projects"),
};

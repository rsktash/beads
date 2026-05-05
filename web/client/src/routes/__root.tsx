import { Outlet, createRootRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { ApiError, api } from "../lib/api";
import { Header } from "../components/Header";
import { Login } from "../components/Login";

function RootComponent() {
  const me = useQuery({ queryKey: ["me"], queryFn: api.me, retry: false });

  if (me.isLoading) {
    return (
      <div
        className="min-h-screen flex items-center justify-center"
        style={{ color: "var(--color-ink-tertiary)" }}
      >connecting…</div>
    );
  }

  // 401 means auth is enabled and we don't have (or have a stale) token.
  const status = me.error instanceof ApiError ? me.error.status : 0;
  if (status === 401) {
    // Hard reload after login so every cached 401 from other queries refetches
    // cleanly with the new token. Cheaper than threading invalidations.
    return <Login onAuthed={() => window.location.reload()} />;
  }

  if (me.error) {
    return (
      <div className="min-h-screen flex flex-col items-center justify-center gap-2">
        <div className="text-red-600 text-sm font-mono">
          /api/me failed: {(me.error as Error).message}
        </div>
        <button
          onClick={() => me.refetch()}
          className="text-xs underline"
          style={{ color: "var(--color-ink-tertiary)" }}
        >retry</button>
      </div>
    );
  }

  return (
    <div className="min-h-screen flex flex-col">
      <Header />
      <main className="flex-1 mx-auto max-w-screen-2xl w-full px-4 py-6">
        <Outlet />
      </main>
    </div>
  );
}

export const Route = createRootRoute({ component: RootComponent });

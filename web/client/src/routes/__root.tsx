import { Outlet, createRootRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { ApiError, api } from "../lib/api";
import { Layout } from "../components/Layout";
import { Login } from "../components/Login";
import { SearchDialog } from "../components/SearchDialog";

function RootComponent() {
  const me = useQuery({ queryKey: ["me"], queryFn: api.me, retry: false });

  if (me.isLoading) {
    return (
      <div
        className="min-h-screen flex items-center justify-center"
        style={{ background: "var(--color-bg-base)", color: "var(--color-ink-tertiary)" }}
      >
        connecting…
      </div>
    );
  }

  const status = me.error instanceof ApiError ? me.error.status : 0;
  if (status === 401) {
    return <Login onAuthed={() => window.location.reload()} />;
  }

  if (me.error) {
    return (
      <div
        className="min-h-screen flex flex-col items-center justify-center gap-2"
        style={{ background: "var(--color-bg-base)" }}
      >
        <div className="text-red-600 text-sm font-mono">
          /api/me failed: {(me.error as Error).message}
        </div>
        <button
          onClick={() => me.refetch()}
          className="text-xs underline"
          style={{ color: "var(--color-ink-tertiary)" }}
        >
          retry
        </button>
      </div>
    );
  }

  return (
    <Layout>
      <SearchDialog />
      <Outlet />
    </Layout>
  );
}

export const Route = createRootRoute({ component: RootComponent });

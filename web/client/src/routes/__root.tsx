import { Outlet, createRootRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { api, readToken } from "../lib/api";
import { Header } from "../components/Header";
import { Login } from "../components/Login";

function RootComponent() {
  const [bumpKey, setBumpKey] = useState(0);
  const me = useQuery({ queryKey: ["me", bumpKey], queryFn: api.me, retry: false });

  // 401 from /api/me means auth is enabled and we're not signed in.
  const need401 = me.error && /^401/.test(String((me.error as Error).message)) === false
    ? false
    : !!me.error;

  if (me.isLoading) {
    return <div className="min-h-screen flex items-center justify-center text-stone-500">connecting…</div>;
  }
  if (need401 && !readToken()) {
    return <Login onAuthed={() => setBumpKey((k) => k + 1)} />;
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

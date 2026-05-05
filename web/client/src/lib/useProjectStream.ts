// useProjectStream — opens a Server-Sent Events connection to
// /api/p/<prefix>/stream and invalidates the project's React Query caches
// whenever the server emits a `tick`. This replaces 5-second polling with
// push-based updates while keeping React Query as the source of truth.
//
// EventSource can't set custom headers, so the bearer token rides as a query
// param. The server's authMiddleware accepts both Authorization: Bearer and
// ?token=. If the stream dies (network blip, proxy timeout) we let
// EventSource auto-reconnect; React Query's normal refetchOnWindowFocus +
// the per-query refetchInterval acts as a fallback.

import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { readToken } from "./api";

export function useProjectStream(prefix: string) {
  const qc = useQueryClient();
  useEffect(() => {
    if (!prefix) return;
    const token = readToken();
    const qs = token ? `?token=${encodeURIComponent(token)}` : "";
    const url = `/api/p/${encodeURIComponent(prefix)}/stream${qs}`;
    const es = new EventSource(url);
    const onTick = () => {
      // Coarse invalidation: list/board cache is keyed ["issues", prefix, ...]
      // and the detail page is keyed ["issue", id]. React Query refetches only
      // what's currently mounted — fan-out to both is safe.
      qc.invalidateQueries({ queryKey: ["issues", prefix] });
      qc.invalidateQueries({ queryKey: ["issue"] });
    };
    es.addEventListener("tick", onTick);
    es.addEventListener("hello", () => {});
    es.onerror = () => { /* EventSource will retry on its own */ };
    return () => {
      es.removeEventListener("tick", onTick);
      es.close();
    };
  }, [prefix, qc]);
}

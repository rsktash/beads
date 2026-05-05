import { Outlet, createFileRoute } from "@tanstack/react-router";
import { useEffect } from "react";
import { api } from "../lib/api";
import { useProjectStream } from "../lib/useProjectStream";

// Layout route for /p/$prefix/*. Pins the api singleton to this project so
// every nested page can call api.listIssues() etc. without threading prefix.
function ProjectLayout() {
  const { prefix } = Route.useParams();
  api.use(prefix); // synchronous, idempotent — safe during render
  useProjectStream(prefix);

  useEffect(() => {
    const prev = document.title;
    document.title = `${prefix} · bd`;
    return () => { document.title = prev; };
  }, [prefix]);

  return <Outlet />;
}

export const Route = createFileRoute("/p/$prefix")({ component: ProjectLayout });

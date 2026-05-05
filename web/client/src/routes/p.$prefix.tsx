import { Outlet, createFileRoute } from "@tanstack/react-router";
import { api } from "../lib/api";

// Layout route for /p/$prefix/*. Pins the api singleton to this project so
// every nested page can call api.listIssues() etc. without threading prefix.
function ProjectLayout() {
  const { prefix } = Route.useParams();
  api.use(prefix); // synchronous, idempotent — safe during render
  return <Outlet />;
}

export const Route = createFileRoute("/p/$prefix")({ component: ProjectLayout });

import { createFileRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";

// "/projects" — only meaningful on a shared postgres. Lists every schema in
// the database that contains a `config` table (i.e. a beads project).
function ProjectsComponent() {
  const me = useQuery({ queryKey: ["me"], queryFn: api.me });
  const q = useQuery({
    queryKey: ["projects"],
    queryFn: api.listProjects,
    enabled: me.data?.driver === "postgres",
  });

  if (me.data?.driver !== "postgres") {
    return (
      <div className="text-stone-600">
        Multi-project listing is only available on postgres backends.
        This workspace is using <span className="font-mono">{me.data?.driver}</span>.
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <h1 className="text-lg font-semibold text-stone-900">projects</h1>
      <ul className="bg-white border border-stone-200 rounded-lg divide-y divide-stone-200">
        {q.data?.projects.map((p) => (
          <li key={p.prefix} className="px-4 py-2 flex items-center">
            <span className="font-mono text-sm text-stone-900">{p.prefix}</span>
            <span className="ml-auto text-xs text-stone-500">
              switch by setting <span className="font-mono">search_path={p.prefix}</span> in .bd/config
            </span>
          </li>
        ))}
        {q.data?.projects.length === 0 && (
          <li className="px-4 py-2 text-stone-500">no projects found</li>
        )}
      </ul>
    </div>
  );
}

export const Route = createFileRoute("/projects")({ component: ProjectsComponent });

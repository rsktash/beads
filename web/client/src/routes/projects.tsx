import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";

function ProjectsComponent() {
  const q = useQuery({ queryKey: ["projects"], queryFn: api.listProjects });

  if (q.isLoading) return <div style={{ color: "var(--color-ink-tertiary)" }}>loading…</div>;
  if (q.error)     return <div className="text-red-600">{(q.error as Error).message}</div>;

  const projects = q.data?.projects ?? [];

  return (
    <div className="space-y-4 max-w-xl">
      <div>
        <h1 className="text-xl font-bold" style={{ color: "var(--color-ink-primary)" }}>
          Projects
        </h1>
        <p className="text-sm mt-0.5" style={{ color: "var(--color-ink-tertiary)" }}>
          {projects.length === 0
            ? "No projects yet — run `bd init --prefix <name>` to create one."
            : `Pick a project to enter.`}
        </p>
      </div>
      <ul
        className="rounded-lg divide-y"
        style={{
          background: "var(--color-bg-elevated)",
          border: "1px solid var(--color-border-subtle)",
          // @ts-expect-error CSS var for divide colour
          "--tw-divide-opacity": 1,
        }}
      >
        {projects.map((p) => (
          <li key={p.prefix}>
            <Link
              to="/p/$prefix"
              params={{ prefix: p.prefix }}
              className="block px-4 py-3 hover:bg-stone-50 transition-colors"
              style={{ color: "var(--color-ink-primary)" }}
            >
              <span className="font-mono text-sm">{p.prefix}</span>
            </Link>
          </li>
        ))}
      </ul>
    </div>
  );
}

export const Route = createFileRoute("/projects")({ component: ProjectsComponent });

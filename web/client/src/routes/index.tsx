import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useEffect } from "react";
import { api } from "../lib/api";

// Landing page: redirect to /projects (single source of truth for the
// project picker). When there's exactly one project we silently jump into
// it so single-project deployments feel the same as before.
function IndexComponent() {
  const navigate = useNavigate();
  const me = useQuery({ queryKey: ["me"], queryFn: api.me });

  useEffect(() => {
    if (!me.data) return;
    if (me.data.projects.length === 1) {
      navigate({ to: "/p/$prefix", params: { prefix: me.data.projects[0].prefix }, replace: true });
    } else {
      navigate({ to: "/projects", replace: true });
    }
  }, [me.data, navigate]);

  return (
    <div className="text-sm" style={{ color: "var(--color-ink-tertiary)" }}>
      loading projects…
    </div>
  );
}

export const Route = createFileRoute("/")({ component: IndexComponent });

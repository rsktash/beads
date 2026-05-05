import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "../lib/api";
import { PriorityBadge, StatusBadge, TypeBadge } from "../components/badges";

// "/list" — flat table view with simple filter controls.
function ListComponent() {
  const [status, setStatus] = useState("");
  const [type, setType] = useState("");
  const [assignee, setAssignee] = useState("");
  const params: Record<string, string> = {};
  if (status) params.status = status;
  if (type) params.type = type;
  if (assignee) params.assignee = assignee;

  const q = useQuery({
    queryKey: ["issues", params],
    queryFn: () => api.listIssues(params),
    refetchInterval: 5000,
  });

  return (
    <div className="space-y-4">
      <div className="flex gap-3 text-sm">
        <Filter label="status" value={status} onChange={setStatus} options={[
          "", "open", "in_progress", "blocked", "closed", "pinned",
        ]} />
        <Filter label="type" value={type} onChange={setType} options={[
          "", "task", "bug", "epic", "feature", "message", "wisp", "molecule", "role", "event",
        ]} />
        <input
          placeholder="assignee"
          value={assignee}
          onChange={(e) => setAssignee(e.target.value)}
          className="border border-stone-300 rounded px-2 py-1 text-sm"
        />
      </div>

      <div className="bg-white border border-stone-200 rounded-lg overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-stone-50 border-b border-stone-200 text-stone-500 text-xs uppercase">
            <tr>
              <th className="text-left px-3 py-2 w-32">id</th>
              <th className="text-left px-3 py-2 w-12">p</th>
              <th className="text-left px-3 py-2 w-32">status</th>
              <th className="text-left px-3 py-2 w-28">type</th>
              <th className="text-left px-3 py-2 w-32">assignee</th>
              <th className="text-left px-3 py-2">title</th>
            </tr>
          </thead>
          <tbody>
            {q.data?.issues.map((i) => (
              <tr key={i.id} className="border-t border-stone-100 hover:bg-stone-50">
                <td className="px-3 py-1.5 font-mono text-stone-500">
                  <Link to="/issue/$id" params={{ id: i.id }}>{i.id}</Link>
                </td>
                <td className="px-3 py-1.5"><PriorityBadge priority={i.priority} /></td>
                <td className="px-3 py-1.5"><StatusBadge status={i.status} /></td>
                <td className="px-3 py-1.5"><TypeBadge type={i.issue_type} /></td>
                <td className="px-3 py-1.5 text-stone-600">{i.assignee || "—"}</td>
                <td className="px-3 py-1.5 text-stone-900">{i.title}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function Filter({ label, value, onChange, options }: {
  label: string; value: string; onChange: (v: string) => void; options: string[];
}) {
  return (
    <label className="flex items-center gap-1">
      <span className="text-xs text-stone-500">{label}</span>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="border border-stone-300 rounded px-2 py-1 text-sm bg-white"
      >
        {options.map((o) => (
          <option key={o} value={o}>{o || "any"}</option>
        ))}
      </select>
    </label>
  );
}

export const Route = createFileRoute("/list")({ component: ListComponent });

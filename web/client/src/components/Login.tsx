import { useState } from "react";
import { api, writeToken } from "../lib/api";

export function Login({ onAuthed }: { onAuthed: () => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string>("");

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    try {
      const r = await api.login(username, password);
      writeToken(r.token);
      onAuthed();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-stone-50">
      <form
        onSubmit={submit}
        className="w-72 bg-white border border-stone-200 rounded-lg p-6 space-y-4 shadow-sm"
      >
        <h1 className="text-lg font-semibold text-stone-900">bd-web sign in</h1>
        <div>
          <label className="block text-xs text-stone-600 mb-1">username</label>
          <input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            className="w-full border border-stone-300 rounded px-2 py-1 text-sm"
            autoFocus
          />
        </div>
        <div>
          <label className="block text-xs text-stone-600 mb-1">password</label>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="w-full border border-stone-300 rounded px-2 py-1 text-sm"
          />
        </div>
        {error && <p className="text-xs text-red-600">{error}</p>}
        <button
          type="submit"
          className="w-full bg-stone-900 text-white rounded text-sm py-1.5 hover:bg-stone-700"
        >sign in</button>
      </form>
    </div>
  );
}

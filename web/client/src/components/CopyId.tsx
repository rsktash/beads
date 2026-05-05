import { useState } from "react";

// Click-to-copy id chip. Shows a transient "copied" indicator on success.
export function CopyId({ id, className = "" }: { id: string; className?: string }) {
  const [copied, setCopied] = useState(false);

  const onClick = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(id);
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    } catch {}
  };

  return (
    <button
      type="button"
      onClick={onClick}
      className={`inline-flex items-center gap-1 font-mono hover:opacity-100 transition-opacity ${className}`}
      style={{ color: "var(--color-ink-tertiary)" }}
      title={copied ? "copied!" : "click to copy"}
    >
      <span>{id}</span>
      {copied ? (
        <CheckIcon />
      ) : (
        <CopyIcon />
      )}
    </button>
  );
}

const CopyIcon = () => (
  <svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor"
       strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" style={{ opacity: 0.5 }}>
    <rect x="5" y="5" width="9" height="9" rx="1.5" />
    <path d="M11 5V3a1 1 0 0 0-1-1H3a1 1 0 0 0-1 1v7a1 1 0 0 0 1 1h2" />
  </svg>
);

const CheckIcon = () => (
  <svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor"
       strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"
       style={{ color: "var(--color-status-closed)" }}>
    <path d="M3 8.5l3.5 3.5L13 5" />
  </svg>
);

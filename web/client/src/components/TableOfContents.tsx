import { useEffect, useState } from "react";

// Lightweight TOC: scans the document for h1–h3 inside the given container,
// renders a sticky outline. Click jumps; passive scrollspy highlights the
// active heading. No fancy intersection-observer math — just nearest-above.

export function TableOfContents({ container }: { container: HTMLElement | null }) {
  const [items, setItems] = useState<Array<{ id: string; level: number; text: string }>>([]);
  const [active, setActive] = useState<string>("");

  useEffect(() => {
    if (!container) return;
    const collect = () => {
      const out: typeof items = [];
      container.querySelectorAll<HTMLElement>("h1[id], h2[id], h3[id]").forEach((el) => {
        out.push({
          id: el.id,
          level: Number(el.tagName.slice(1)),
          text: el.textContent || el.id,
        });
      });
      setItems(out);
    };
    collect();
    const obs = new MutationObserver(collect);
    obs.observe(container, { childList: true, subtree: true });
    return () => obs.disconnect();
  }, [container]);

  useEffect(() => {
    if (!container || items.length === 0) return;
    const onScroll = () => {
      const top = container.scrollTop + 16;
      let cur = items[0]?.id;
      for (const it of items) {
        const el = container.querySelector<HTMLElement>(`#${cssEsc(it.id)}`);
        if (!el) continue;
        if (el.offsetTop <= top) cur = it.id;
        else break;
      }
      if (cur) setActive(cur);
    };
    onScroll();
    container.addEventListener("scroll", onScroll, { passive: true });
    return () => container.removeEventListener("scroll", onScroll);
  }, [container, items]);

  if (items.length < 2) return null;

  const onClick = (id: string) => (e: React.MouseEvent) => {
    e.preventDefault();
    const el = container?.querySelector<HTMLElement>(`#${cssEsc(id)}`);
    if (el) el.scrollIntoView({ behavior: "smooth", block: "start" });
  };

  return (
    <nav
      className="sticky top-0 w-56 text-xs space-y-0.5 pt-1"
      style={{ color: "var(--color-ink-tertiary)" }}
    >
      <div
        className="text-[10px] uppercase font-semibold tracking-wider mb-2 px-2"
        style={{ color: "var(--color-ink-tertiary)" }}
      >
        On this page
      </div>
      {items.map((it) => (
        <a
          key={it.id}
          href={`#${it.id}`}
          onClick={onClick(it.id)}
          className="block px-2 py-1 rounded transition-colors truncate"
          style={{
            paddingLeft: 8 + (it.level - 1) * 10,
            color: active === it.id ? "var(--color-accent)" : "var(--color-ink-tertiary)",
            fontWeight: active === it.id ? 500 : 400,
          }}
          title={it.text}
        >
          {it.text}
        </a>
      ))}
    </nav>
  );
}

function cssEsc(id: string): string {
  // CSS.escape on modern browsers; fall back to a manual escape for safety.
  if (typeof CSS !== "undefined" && (CSS as any).escape) return (CSS as any).escape(id);
  return id.replace(/([^a-zA-Z0-9_-])/g, "\\$1");
}

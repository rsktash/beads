// Project-scoped router shim.
//
// Inside /p/$prefix/* routes, callers write logical URLs like
// `<Link to="/issue/$id" params={{ id }}>` and this shim prepends the active
// project segment, sourcing `prefix` from the current URL via useParams.
// useNavigate() works the same way.
//
// For outside-project navigation (/projects, /, project switcher), import
// directly from @tanstack/react-router instead.

import {
  Link as TSLink,
  useNavigate as useTSNavigate,
  useParams,
} from "@tanstack/react-router";
import {
  forwardRef,
  type AnchorHTMLAttributes,
  type CSSProperties,
} from "react";

function useProjectPrefix(): string {
  const p = useParams({ strict: false }) as { prefix?: string };
  return p.prefix ?? "";
}

type Params = Record<string, string>;

type LocalLinkProps = AnchorHTMLAttributes<HTMLAnchorElement> & {
  to: string;
  params?: Params;
  activeProps?: { className?: string; style?: CSSProperties };
};

export const Link = forwardRef<HTMLAnchorElement, LocalLinkProps>(function Link(
  { to, params, ...rest }, ref,
) {
  const prefix = useProjectPrefix();
  const target = to === "" || to === "/" ? "/p/$prefix" : `/p/$prefix${to}`;
  const tsProps = rest as Record<string, unknown>;
  return (
    <TSLink
      ref={ref}
      to={target as never}
      params={{ prefix, ...(params ?? {}) } as never}
      {...tsProps}
    />
  );
});

type NavigateOpts = {
  to: string;
  params?: Params;
  replace?: boolean;
  search?: unknown;
};

export function useNavigate() {
  const navigate = useTSNavigate();
  const prefix = useProjectPrefix();
  return (opts: NavigateOpts) => {
    const target = opts.to === "" || opts.to === "/" ? "/p/$prefix" : `/p/$prefix${opts.to}`;
    return navigate({
      to: target as never,
      params: { prefix, ...(opts.params ?? {}) } as never,
      replace: opts.replace,
      search: opts.search as never,
    });
  };
}

export { useParams };

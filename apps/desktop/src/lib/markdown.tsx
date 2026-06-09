import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

import { cn } from "./utils";

/**
 * Minimal Markdown renderer that handles the things agents actually emit:
 * fenced code blocks, inline code, lists, tables (via GFM), bold/italic,
 * blockquotes, headings.
 *
 * No syntax highlighting yet -- on purpose. Adding shiki/highlight.js bloats
 * the bundle by ~500 KB for a UX gain we haven't earned yet. V0.3.
 */
export function Markdown({
  children,
  className,
}: {
  children: string;
  className?: string;
}) {
  return (
    <div className={cn("prose-logos", className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          // We render in a dark panel so default markdown styles need overrides.
          code({ className, children, ...props }) {
            const isBlock = String(className ?? "").includes("language-");
            if (isBlock) {
              return (
                <pre className="my-2 overflow-x-auto rounded border border-border bg-bg/60 p-3 text-xs">
                  <code className={className} {...props}>
                    {children}
                  </code>
                </pre>
              );
            }
            return (
              <code className="rounded bg-bg/60 px-1 py-0.5 text-[0.85em] font-mono">
                {children}
              </code>
            );
          },
          a({ children, href, ...props }) {
            return (
              <a
                href={href}
                target="_blank"
                rel="noreferrer noopener"
                className="text-accent underline-offset-2 hover:underline"
                {...props}
              >
                {children}
              </a>
            );
          },
          ul: (props) => <ul className="my-2 list-disc pl-6" {...props} />,
          ol: (props) => <ol className="my-2 list-decimal pl-6" {...props} />,
          li: (props) => <li className="my-0.5" {...props} />,
          p: (props) => <p className="my-2 leading-relaxed" {...props} />,
          h1: (props) => (
            <h1 className="mb-2 mt-4 text-lg font-semibold" {...props} />
          ),
          h2: (props) => (
            <h2 className="mb-2 mt-3 text-base font-semibold" {...props} />
          ),
          h3: (props) => (
            <h3 className="mb-1 mt-3 text-sm font-semibold" {...props} />
          ),
          blockquote: (props) => (
            <blockquote
              className="my-2 border-l-2 border-border pl-3 italic opacity-80"
              {...props}
            />
          ),
          table: (props) => (
            <div className="my-2 overflow-x-auto">
              <table className="w-full border-collapse text-xs" {...props} />
            </div>
          ),
          th: (props) => (
            <th
              className="border border-border bg-bg/40 px-2 py-1 text-left font-semibold"
              {...props}
            />
          ),
          td: (props) => (
            <td className="border border-border px-2 py-1 align-top" {...props} />
          ),
          hr: () => <hr className="my-3 border-border" />,
        }}
      >
        {children}
      </ReactMarkdown>
    </div>
  );
}

import { forwardRef, useImperativeHandle, useRef, useState } from "react";

import type { Agent } from "../lib/api";

// MentionAutocompleteTextarea wraps <textarea> with the same @-mention
// detection + insertion logic the V0.8 ReplyComposer originally
// inlined. Same trigger rule as the server-side mention parser
// (internal/mentions/mentions.go) so what the popover suggests is
// exactly what the server will resolve to a worker.
//
// Disambiguation: when two candidates share a case-insensitive name,
// insertion appends `#<short-id-8>` (matches the parser's #<shortid>
// branch); otherwise the bare name is inserted. The inserted text
// always ends with a trailing space so the next token reads as a
// continuation.
//
// Reused by:
//  - components/issue-thread.tsx -- the issue Reply composer
//  - pages/SquadsPage.tsx -- "Leader instructions" in New + Edit modals
//
// Uses forwardRef so callers can attach to the inner <textarea> (e.g.
// to drive auto-grow via scrollHeight). The forwarded ref points at
// the textarea element, not the outer wrapper div.
type Props = Omit<React.TextareaHTMLAttributes<HTMLTextAreaElement>, "onChange" | "value"> & {
  value: string;
  onChange: (next: string) => void;
  candidates: Agent[];
  /**
   * Optional callback fired when the user submits via Cmd/Ctrl+Enter.
   * If omitted, the keystroke is left to default behaviour.
   */
  onSubmit?: () => void;
  /**
   * When false, the popover is disabled regardless of candidates.
   * Default true.
   */
  autocompleteEnabled?: boolean;
};

const SEPARATOR_RE = /[\s,.;:!?(\[{]/;
const NAME_CHAR_RE = /[A-Za-z0-9_.\-#]/;

export const MentionAutocompleteTextarea = forwardRef<HTMLTextAreaElement, Props>(
  function MentionAutocompleteTextarea(
    {
      value,
      onChange,
      candidates,
      onSubmit,
      onKeyDown,
      autocompleteEnabled = true,
      ...textareaProps
    },
    forwardedRef,
  ) {
    const innerRef = useRef<HTMLTextAreaElement>(null);
    // Surface the inner textarea via the forwarded ref. Callers see
    // the live <textarea> element (for scrollHeight / setSelectionRange /
    // focus / etc.) without us having to expose anything richer.
    useImperativeHandle(forwardedRef, () => innerRef.current as HTMLTextAreaElement);

    const [triggerStart, setTriggerStart] = useState<number | null>(null);
    const [query, setQuery] = useState("");
    const [highlight, setHighlight] = useState(0);

    const detectMention = (text: string, caret: number) => {
      for (let i = caret - 1; i >= 0; i--) {
        const ch = text[i];
        if (ch === "@") {
          const prev = text[i - 1];
          if (prev !== undefined && !SEPARATOR_RE.test(prev)) return null;
          const partial = text.slice(i + 1, caret);
          if (/\s/.test(partial)) return null;
          return { start: i, query: partial };
        }
        if (!NAME_CHAR_RE.test(ch)) return null;
      }
      return null;
    };

    const filtered = (() => {
      if (triggerStart === null) return [] as Agent[];
      const q = query.toLowerCase();
      return candidates
        .filter((a) => a.name.toLowerCase().includes(q))
        .slice(0, 6);
    })();

    const closePopover = () => {
      setTriggerStart(null);
      setQuery("");
      setHighlight(0);
    };

    const insertMention = (agent: Agent) => {
      if (triggerStart === null) return;
      const el = innerRef.current;
      if (!el) return;
      const caret = el.selectionStart ?? value.length;
      const before = value.slice(0, triggerStart);
      const after = value.slice(caret);
      const sameName = candidates.filter(
        (c) => c.name.toLowerCase() === agent.name.toLowerCase(),
      );
      const insertion =
        sameName.length > 1
          ? `@${agent.name}#${agent.id.slice(0, 8)} `
          : `@${agent.name} `;
      const next = before + insertion + after;
      onChange(next);
      closePopover();
      requestAnimationFrame(() => {
        const pos = before.length + insertion.length;
        el.setSelectionRange(pos, pos);
        el.focus();
      });
    };

    const handleChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
      const nextValue = e.target.value;
      onChange(nextValue);
      if (!autocompleteEnabled || candidates.length === 0) {
        if (triggerStart !== null) closePopover();
        return;
      }
      const caret = e.target.selectionStart ?? nextValue.length;
      const m = detectMention(nextValue, caret);
      if (m) {
        setTriggerStart(m.start);
        setQuery(m.query);
        setHighlight(0);
      } else if (triggerStart !== null) {
        closePopover();
      }
    };

    const handleKey = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (triggerStart !== null && filtered.length > 0) {
        if (e.key === "ArrowDown") {
          e.preventDefault();
          setHighlight((i) => Math.min(i + 1, filtered.length - 1));
          return;
        }
        if (e.key === "ArrowUp") {
          e.preventDefault();
          setHighlight((i) => Math.max(i - 1, 0));
          return;
        }
        if ((e.key === "Enter" && !e.metaKey && !e.ctrlKey) || e.key === "Tab") {
          e.preventDefault();
          insertMention(filtered[highlight]);
          return;
        }
        if (e.key === "Escape") {
          e.preventDefault();
          closePopover();
          return;
        }
      }
      if (
        onSubmit &&
        (e.metaKey || e.ctrlKey) &&
        e.key === "Enter" &&
        value.trim()
      ) {
        e.preventDefault();
        onSubmit();
        return;
      }
      onKeyDown?.(e);
    };

    return (
      <div className="relative">
        <textarea
          {...textareaProps}
          ref={innerRef}
          value={value}
          onChange={handleChange}
          onKeyDown={handleKey}
          onBlur={(e) => {
            setTimeout(closePopover, 120);
            textareaProps.onBlur?.(e);
          }}
        />
        {triggerStart !== null && filtered.length > 0 ? (
          <ul
            className="absolute bottom-full left-2 z-10 mb-1 max-h-48 w-64 overflow-auto rounded border border-border bg-panel shadow-xl"
            onMouseDown={(e) => e.preventDefault()}
          >
            {filtered.map((a, i) => (
              <li
                key={a.id}
                onMouseEnter={() => setHighlight(i)}
                onMouseDown={() => insertMention(a)}
                className={
                  i === highlight
                    ? "cursor-pointer bg-accent/15 px-3 py-1.5 text-xs"
                    : "cursor-pointer px-3 py-1.5 text-xs hover:bg-bg/60"
                }
              >
                <span className="font-mono">@{a.name}</span>
                <span className="ml-2 opacity-50">{a.id.slice(0, 8)}</span>
              </li>
            ))}
          </ul>
        ) : null}
      </div>
    );
  },
);


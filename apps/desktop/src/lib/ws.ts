// Minimal WebSocket client. Auto-reconnects on close after 2s. Mirrors
// the event-prefix model from server/pkg/protocol/events.go so callers
// can subscribe by exact type ("task:running") OR prefix ("task:").

import { useEffect, useRef } from "react";
import { useRuntimeConfig } from "./runtime";

type Envelope = { type: string; payload: unknown };
type Handler = (env: Envelope) => void;

class WSClient {
  private ws: WebSocket | null = null;
  private handlers = new Set<Handler>();
  private url: string;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private closed = false;

  constructor(httpUrl: string, token: string) {
    this.url = httpUrl.replace(/^http/, "ws") + "/ws?token=" + encodeURIComponent(token);
    this.connect();
  }

  private connect() {
    if (this.closed) return;
    const ws = new WebSocket(this.url);
    this.ws = ws;
    ws.onopen = () => {
      console.info("[logos] ws connected");
    };
    ws.onmessage = (ev) => {
      let env: Envelope;
      try {
        env = JSON.parse(ev.data);
      } catch {
        return;
      }
      for (const h of this.handlers) {
        try {
          h(env);
        } catch (err) {
          console.warn("[logos] ws handler threw", err);
        }
      }
    };
    ws.onclose = () => {
      this.ws = null;
      if (this.closed) return;
      this.reconnectTimer = setTimeout(() => this.connect(), 2000);
    };
    ws.onerror = () => {
      // onclose will fire after this; nothing to do here.
    };
  }

  on(h: Handler): () => void {
    this.handlers.add(h);
    return () => this.handlers.delete(h);
  }

  close() {
    this.closed = true;
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.ws?.close();
  }
}

let singleton: WSClient | null = null;

export function useWS() {
  const { url, token } = useRuntimeConfig();
  if (!singleton) {
    singleton = new WSClient(url, token);
  }
  return singleton;
}

/** Subscribe to events whose type matches the exact string or starts with `prefix:`. */
export function useWSEvent(
  matcher: string | ((type: string) => boolean),
  handler: (type: string, payload: unknown) => void,
) {
  const ws = useWS();
  const handlerRef = useRef(handler);
  handlerRef.current = handler;
  useEffect(() => {
    const match =
      typeof matcher === "string"
        ? matcher.endsWith(":")
          ? (t: string) => t.startsWith(matcher)
          : (t: string) => t === matcher
        : matcher;
    return ws.on(({ type, payload }) => {
      if (match(type)) handlerRef.current(type, payload);
    });
  }, [ws, matcher]);
}

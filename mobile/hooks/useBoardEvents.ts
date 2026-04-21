// useBoardEvents subscribes to /api/board/events via SSE for the
// duration of the component's lifetime. Web only — on native, where the
// EventSource API is not available, the hook is a no-op.
//
// Auth: the JWT is passed as ?token= query param because the browser
// EventSource API does not support custom headers. The backend's
// RequireAuth middleware accepts ?token= as an alternative to the
// Authorization header, so this is a first-class auth path, not a
// workaround.
//
// Resume: EventSource emits each event with `lastEventId` populated
// from the wire `id:` field. We track the most recent id in a ref and
// pass it via the `Last-Event-ID` header on reconnect. (The browser
// itself does this automatically on EventSource auto-reconnects, but
// we manage reconnection manually so we send the header explicitly via
// the URL query string after backoff.)
//
// Backoff: 1s, 2s, 4s, 8s, ... capped at 30s. Reset on a successful
// open.

import { useEffect, useRef } from "react";
import { Platform } from "react-native";

import { API_BASE_URL } from "@/services/board";
import { getAccessToken } from "@/services/api";
import type { BoardEvent } from "@/services/boardTypes";

type Options = {
  enabled?: boolean;
  onEvent: (event: BoardEvent) => void;
  onError?: (error: Error) => void;
};

const MAX_BACKOFF_MS = 30_000;
const BASE_BACKOFF_MS = 1_000;

export function useBoardEvents({ enabled = true, onEvent, onError }: Options) {
  const onEventRef = useRef(onEvent);
  const onErrorRef = useRef(onError);

  useEffect(() => {
    onEventRef.current = onEvent;
  }, [onEvent]);

  useEffect(() => {
    onErrorRef.current = onError;
  }, [onError]);

  useEffect(() => {
    if (!enabled) {
      return;
    }
    if (Platform.OS !== "web") {
      return;
    }
    if (typeof EventSource === "undefined") {
      return;
    }

    let cancelled = false;
    let source: EventSource | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let attempt = 0;
    let lastEventId: string | null = null;

    const scheduleReconnect = () => {
      if (cancelled) {
        return;
      }
      const delay = Math.min(
        MAX_BACKOFF_MS,
        BASE_BACKOFF_MS * 2 ** Math.max(0, attempt - 1),
      );
      reconnectTimer = setTimeout(() => {
        void connect();
      }, delay);
    };

    const connect = async () => {
      if (cancelled) {
        return;
      }
      attempt += 1;

      let token: string | null = null;
      try {
        token = await getAccessToken();
      } catch (err) {
        onErrorRef.current?.(err instanceof Error ? err : new Error(String(err)));
        scheduleReconnect();
        return;
      }
      if (!token) {
        // No token yet — try again after backoff. The auth provider
        // may still be hydrating.
        scheduleReconnect();
        return;
      }

      const params = new URLSearchParams({ token });
      if (lastEventId) {
        params.set("last_event_id", lastEventId);
      }
      const base = API_BASE_URL.replace(/\/+$/, "");
      const url = `${base}/api/board/events?${params.toString()}`;

      try {
        source = new EventSource(url);
      } catch (err) {
        onErrorRef.current?.(err instanceof Error ? err : new Error(String(err)));
        scheduleReconnect();
        return;
      }

      source.onopen = () => {
        attempt = 0;
      };

      source.onmessage = (event: MessageEvent) => {
        if (event.lastEventId) {
          lastEventId = event.lastEventId;
        }
        if (!event.data) {
          return;
        }
        try {
          const parsed = JSON.parse(event.data) as BoardEvent;
          onEventRef.current(parsed);
        } catch (err) {
          onErrorRef.current?.(
            err instanceof Error ? err : new Error("invalid sse payload"),
          );
        }
      };

      source.onerror = () => {
        if (cancelled) {
          return;
        }
        onErrorRef.current?.(new Error("board events stream disconnected"));
        if (source) {
          source.close();
          source = null;
        }
        scheduleReconnect();
      };
    };

    void connect();

    return () => {
      cancelled = true;
      if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
      }
      if (source) {
        source.close();
        source = null;
      }
    };
  }, [enabled]);
}

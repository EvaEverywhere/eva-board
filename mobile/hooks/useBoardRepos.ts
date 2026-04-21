// useBoardRepos loads + caches the user's connected GitHub repos and
// resolves which one is currently selected.
//
// Selection precedence on first load (matches the candidate loop in
// the resolve effect below):
//   1. initialRepoId arg (e.g. ?repo URL query param) — URL wins so
//      shared links land on the intended board.
//   2. The current in-memory selectedId (preserves a prior in-session
//      pick across re-renders / refetches).
//   3. The repo last selected in this browser (localStorage).
//   4. The repo with is_default=true.
//   5. The first repo in the list.
//   6. null when the user has zero repos.
//
// The hook returns the repos list, the current selection, a setter,
// loading + error state, and a refetch handle. Persistence is per
// platform: web uses localStorage (synchronous), native uses
// expo-secure-store (asynchronous, loaded once on mount). The URL
// remains the source of truth for share-ability; storage is just a
// convenience fallback so the user lands on their last-used repo
// across app launches.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Platform } from "react-native";
import * as SecureStore from "expo-secure-store";

import { listBoardRepos } from "@/services/board";
import type { BoardRepo } from "@/services/boardTypes";

const STORAGE_KEY = "eva_board_selected_repo";

function readStoredSelectionWeb(): string | null {
  if (Platform.OS !== "web") return null;
  try {
    return globalThis.localStorage?.getItem(STORAGE_KEY) ?? null;
  } catch {
    return null;
  }
}

async function readStoredSelectionNative(): Promise<string | null> {
  if (Platform.OS === "web") return null;
  try {
    return await SecureStore.getItemAsync(STORAGE_KEY);
  } catch {
    return null;
  }
}

function writeStoredSelection(id: string | null) {
  if (Platform.OS === "web") {
    try {
      if (id) {
        globalThis.localStorage?.setItem(STORAGE_KEY, id);
      } else {
        globalThis.localStorage?.removeItem(STORAGE_KEY);
      }
    } catch {
      // best-effort persistence; ignore quota / disabled storage
    }
    return;
  }
  void (async () => {
    try {
      if (id) {
        await SecureStore.setItemAsync(STORAGE_KEY, id);
      } else {
        await SecureStore.deleteItemAsync(STORAGE_KEY);
      }
    } catch {
      // best-effort persistence
    }
  })();
}

export type UseBoardReposResult = {
  repos: BoardRepo[];
  selected: BoardRepo | null;
  selectRepo: (id: string) => void;
  isLoading: boolean;
  error: string | null;
  refetch: () => Promise<void>;
};

export function useBoardRepos(initialRepoId?: string): UseBoardReposResult {
  const [repos, setRepos] = useState<BoardRepo[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(initialRepoId ?? null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const initialAppliedRef = useRef(false);
  // Native SecureStore fetch is async, so we cache the loaded value
  // (or `null` once the fetch resolves with no value) and treat
  // `undefined` as "still loading". Web stays synchronous via
  // readStoredSelectionWeb() in the resolution effect below.
  const [nativeStored, setNativeStored] = useState<string | null | undefined>(
    Platform.OS === "web" ? null : undefined,
  );

  useEffect(() => {
    if (Platform.OS === "web") return;
    let cancelled = false;
    void readStoredSelectionNative().then((value) => {
      if (!cancelled) setNativeStored(value);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  const refetch = useCallback(async () => {
    setError(null);
    setIsLoading(true);
    try {
      const list = await listBoardRepos();
      setRepos(list);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load repos");
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    void refetch();
  }, [refetch]);

  // Resolve initial selection once we have repos. Re-run if the
  // initialRepoId changes (user navigated with a different ?repo).
  useEffect(() => {
    if (!repos.length) {
      setSelectedId(null);
      return;
    }
    // Wait for native SecureStore to resolve before picking a default,
    // otherwise we'd race against the user's last-used repo and end up
    // showing the first/default repo on every cold start.
    if (Platform.OS !== "web" && nativeStored === undefined) {
      return;
    }
    const stored =
      Platform.OS === "web" ? readStoredSelectionWeb() : nativeStored ?? null;
    const candidates = [initialRepoId, selectedId, stored];
    for (const id of candidates) {
      if (id && repos.some((r) => r.id === id)) {
        setSelectedId(id);
        initialAppliedRef.current = true;
        return;
      }
    }
    const def = repos.find((r) => r.is_default) ?? repos[0];
    if (def) {
      setSelectedId(def.id);
      initialAppliedRef.current = true;
    }
    // We intentionally exclude `selectedId` from deps to avoid a loop
    // where selecting a repo causes a re-resolve.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [repos, initialRepoId, nativeStored]);

  const selectRepo = useCallback((id: string) => {
    setSelectedId(id);
    writeStoredSelection(id);
  }, []);

  const selected = useMemo(
    () => repos.find((r) => r.id === selectedId) ?? null,
    [repos, selectedId],
  );

  return { repos, selected, selectRepo, isLoading, error, refetch };
}

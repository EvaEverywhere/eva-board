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
// loading + error state, and a refetch handle. Persistence is web-only
// (no-op on native); the URL is the source of truth for share-ability,
// localStorage is just the convenience fallback.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Platform } from "react-native";

import { listBoardRepos } from "@/services/board";
import type { BoardRepo } from "@/services/boardTypes";

const STORAGE_KEY = "eva_board_selected_repo";

function readStoredSelection(): string | null {
  if (Platform.OS !== "web") return null;
  try {
    return globalThis.localStorage?.getItem(STORAGE_KEY) ?? null;
  } catch {
    return null;
  }
}

function writeStoredSelection(id: string | null) {
  if (Platform.OS !== "web") return;
  try {
    if (id) {
      globalThis.localStorage?.setItem(STORAGE_KEY, id);
    } else {
      globalThis.localStorage?.removeItem(STORAGE_KEY);
    }
  } catch {
    // best-effort persistence; ignore quota / disabled storage
  }
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
    const stored = readStoredSelection();
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
  }, [repos, initialRepoId]);

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

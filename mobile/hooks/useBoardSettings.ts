import { useCallback, useEffect, useState } from "react";

import { getBoardSettings } from "@/services/board";
import type { BoardSettings } from "@/services/boardTypes";

export type UseBoardSettingsResult = {
  settings: BoardSettings | null;
  isLoading: boolean;
  isConfigured: boolean;
  error: string | null;
  refresh: () => Promise<void>;
};

// Centralised loader for /api/board/settings so screens (settings,
// board, onboarding banner) share one source of truth. `isConfigured`
// requires a saved PAT plus an owner+repo selection — i.e. enough state
// for the agent loop to actually do work.
export function useBoardSettings(): UseBoardSettingsResult {
  const [settings, setSettings] = useState<BoardSettings | null>(null);
  const [isLoading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const result = await getBoardSettings();
      setSettings(result);
    } catch (err) {
      const message = err instanceof Error ? err.message : "Failed to load settings";
      setError(message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const isConfigured = Boolean(
    settings?.has_github_token && settings?.github_owner && settings?.github_repo,
  );

  return { settings, isLoading, isConfigured, error, refresh };
}

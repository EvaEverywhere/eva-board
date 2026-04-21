// Typed client for /api/board/* routes.
//
// Mirrors the existing pattern in services/api.ts: every function is
// async, uses request<T>() for auth + error mapping, and returns a
// typed response. List-style routes that wrap their payload in an
// envelope ({cards}, {repos}, ...) are unwrapped here so callers see
// the bare array.

import { request } from "@/services/api";
import type {
  AddRepoRequest,
  AgentActionStatus,
  BoardCard,
  BoardColumn,
  BoardRepo,
  BoardSettings,
  CleanupAction,
  CreateCardRequest,
  CurateResult,
  ListCardsOptions,
  MoveCardRequest,
  Repo,
  TriageProposal,
  UpdateCardRequest,
  UpsertSettingsRequest,
} from "@/services/boardTypes";

// Re-export the API base URL under the conventional name used by the
// board hooks (SSE, etc.). The mobile app's source of truth lives in
// @/config — this is a typed alias so consumers don't have to know.
export { API_URL as API_BASE_URL } from "@/config";

// ---------- Cards ----------

export async function listCards(opts?: ListCardsOptions): Promise<BoardCard[]> {
  // Backend reads the repo from ?repo_id and the column filter from ?column,
  // both optional. When repo_id is omitted, the backend falls back to the
  // user's default repo; absence of repos returns 400 which the caller
  // surfaces as an empty-state.
  const params = new URLSearchParams();
  if (opts?.column) params.set("column", opts.column);
  if (opts?.repoId) params.set("repo_id", opts.repoId);
  const query = params.toString() ? `?${params.toString()}` : "";
  const res = await request<{ cards: BoardCard[] }>(`/api/board/cards${query}`);
  return res.cards ?? [];
}

export async function createCard(
  req: CreateCardRequest,
  repoId?: string,
): Promise<BoardCard> {
  const query = repoId ? `?repo_id=${encodeURIComponent(repoId)}` : "";
  return request<BoardCard>(`/api/board/cards${query}`, {
    method: "POST",
    body: JSON.stringify(req),
  });
}

export async function getCard(id: string): Promise<BoardCard> {
  return request<BoardCard>(`/api/board/cards/${encodeURIComponent(id)}`);
}

export async function updateCard(
  id: string,
  req: UpdateCardRequest,
): Promise<BoardCard> {
  return request<BoardCard>(`/api/board/cards/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: JSON.stringify(req),
  });
}

export async function deleteCard(id: string): Promise<void> {
  await request<void>(`/api/board/cards/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export async function moveCard(
  id: string,
  req: MoveCardRequest,
): Promise<BoardCard> {
  return request<BoardCard>(`/api/board/cards/${encodeURIComponent(id)}/move`, {
    method: "POST",
    body: JSON.stringify(req),
  });
}

// ---------- Agent control ----------

export async function startAgent(cardId: string): Promise<void> {
  await request<AgentActionStatus>(
    `/api/board/cards/${encodeURIComponent(cardId)}/agent/start`,
    { method: "POST" },
  );
}

export async function stopAgent(cardId: string): Promise<void> {
  await request<AgentActionStatus>(
    `/api/board/cards/${encodeURIComponent(cardId)}/agent/stop`,
    { method: "POST" },
  );
}

export async function sendAgentFeedback(
  cardId: string,
  feedback: string,
): Promise<void> {
  await request<AgentActionStatus>(
    `/api/board/cards/${encodeURIComponent(cardId)}/agent/feedback`,
    {
      method: "POST",
      body: JSON.stringify({ feedback }),
    },
  );
}

// ---------- Diff ----------

export type CardDiff = {
  diff: string;
  branch: string | null;
  base: string;
  error?: string;
};

export async function getCardDiff(cardId: string): Promise<CardDiff> {
  return request<CardDiff>(
    `/api/board/cards/${encodeURIComponent(cardId)}/diff`,
  );
}

// ---------- Settings ----------

export async function getBoardSettings(): Promise<BoardSettings> {
  return request<BoardSettings>("/api/board/settings");
}

export async function updateBoardSettings(
  req: UpsertSettingsRequest,
): Promise<BoardSettings> {
  return request<BoardSettings>("/api/board/settings", {
    method: "PUT",
    body: JSON.stringify(req),
  });
}

export async function listRepos(): Promise<Repo[]> {
  const res = await request<{ repos: Repo[] }>("/api/board/settings/repos");
  return res.repos ?? [];
}

// ---------- Connected repos (board_repos) ----------

export async function listBoardRepos(): Promise<BoardRepo[]> {
  const res = await request<{ repos: BoardRepo[] }>("/api/board/repos");
  return res.repos ?? [];
}

export async function addBoardRepo(req: AddRepoRequest): Promise<BoardRepo> {
  return request<BoardRepo>("/api/board/repos", {
    method: "POST",
    body: JSON.stringify(req),
  });
}

export async function removeBoardRepo(id: string): Promise<void> {
  await request<void>(`/api/board/repos/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export async function setDefaultBoardRepo(id: string): Promise<BoardRepo> {
  return request<BoardRepo>(
    `/api/board/repos/${encodeURIComponent(id)}/default`,
    { method: "POST" },
  );
}

// ---------- Curate (triage + spring clean) ----------

export async function runTriage(): Promise<{ proposals: TriageProposal[] }> {
  return request<{ proposals: TriageProposal[] }>("/api/board/triage", {
    method: "POST",
  });
}

export async function applyTriage(
  proposals: TriageProposal[],
): Promise<void> {
  await request<{ applied: number }>("/api/board/triage/apply", {
    method: "POST",
    body: JSON.stringify({ proposals }),
  });
}

export async function runSpringClean(): Promise<{ actions: CleanupAction[] }> {
  return request<{ actions: CleanupAction[] }>("/api/board/springclean", {
    method: "POST",
  });
}

export async function applySpringClean(
  actions: CleanupAction[],
): Promise<void> {
  await request<{ applied: number }>("/api/board/springclean/apply", {
    method: "POST",
    body: JSON.stringify({ actions }),
  });
}

export async function runCurate(): Promise<CurateResult> {
  return request<CurateResult>("/api/board/curate", { method: "POST" });
}

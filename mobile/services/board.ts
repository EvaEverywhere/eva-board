// Typed client for /api/board/* routes.
//
// Mirrors the existing pattern in services/api.ts: every function is
// async, uses request<T>() for auth + error mapping, and returns a
// typed response. List-style routes that wrap their payload in an
// envelope ({cards}, {repos}, ...) are unwrapped here so callers see
// the bare array.

import { request } from "@/services/api";
import type {
  AgentActionStatus,
  BoardCard,
  BoardColumn,
  BoardSettings,
  CleanupAction,
  CreateCardRequest,
  CurateResult,
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

export async function listCards(column?: BoardColumn): Promise<BoardCard[]> {
  const query = column ? `?column=${encodeURIComponent(column)}` : "";
  const res = await request<{ cards: BoardCard[] }>(`/api/board/cards${query}`);
  return res.cards ?? [];
}

export async function createCard(req: CreateCardRequest): Promise<BoardCard> {
  return request<BoardCard>("/api/board/cards", {
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

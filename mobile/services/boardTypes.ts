// TypeScript mirrors of the Go types in backend/internal/board.
//
// Field names match the Go `json:"..."` tags. Pointer fields with
// `omitempty` are typed as optional + nullable so the client tolerates
// both an absent field and an explicit null.

export type BoardColumn = "backlog" | "develop" | "review" | "pr" | "done";

export const BOARD_COLUMNS: readonly BoardColumn[] = [
  "backlog",
  "develop",
  "review",
  "pr",
  "done",
] as const;

export const BOARD_COLUMN_LABELS: Record<BoardColumn, string> = {
  backlog: "Backlog",
  develop: "Develop",
  review: "Review",
  pr: "PR",
  done: "Done",
};

export type AgentStatus =
  | "idle"
  | "running"
  | "verifying"
  | "reviewing"
  | "failed"
  | "succeeded";

export type BoardCard = {
  id: string;
  user_id: string;
  title: string;
  description: string;
  column: BoardColumn;
  position: number;
  agent_status: AgentStatus;
  worktree_branch?: string | null;
  pr_number?: number | null;
  pr_url?: string | null;
  review_status?: string | null;
  metadata: Record<string, unknown>;
  created_at: string;
  updated_at: string;
};

export type CreateCardRequest = {
  title: string;
  description: string;
};

export type UpdateCardRequest = {
  title?: string;
  description?: string;
  metadata?: Record<string, unknown>;
};

export type MoveCardRequest = {
  to_column: BoardColumn;
  to_position: number;
};

export type AgentActionStatus = {
  status: string;
};

export type BoardSettings = {
  user_id: string;
  github_owner: string;
  github_repo: string;
  repo_path: string;
  codegen_agent: string;
  codegen_command: string | null;
  codegen_args: string[];
  max_verify_iterations: number;
  max_review_cycles: number;
  has_github_token: boolean;
  updated_at: string;
};

export type UpsertSettingsRequest = {
  github_token?: string | null;
  github_owner?: string;
  github_repo?: string;
  repo_path?: string;
  codegen_agent?: string;
  codegen_command?: string | null;
  codegen_args?: string[];
  max_verify_iterations?: number;
  max_review_cycles?: number;
};

export type Repo = {
  owner: string;
  name: string;
  default_branch: string;
  private: boolean;
};

export type TriageProposalType = "create" | "close" | "rewrite";

export type TriageProposal = {
  type: TriageProposalType;
  card_id?: string;
  title?: string;
  description?: string;
  acceptance_criteria?: string[];
  reason?: string;
};

export type CleanupActionType =
  | "delete_branch"
  | "remove_worktree"
  | "close_issue";

export type CleanupAction = {
  type: CleanupActionType;
  target: string;
  reason: string;
};

export type CurateResult = {
  triage_proposals: TriageProposal[];
  cleanup_actions: CleanupAction[];
  errors?: Record<string, string>;
};

export type BoardEventType =
  | "agent_started"
  | "agent_progress"
  | "agent_finished"
  | "verification_started"
  | "verification_result"
  | "review_started"
  | "review_result"
  | "pr_created"
  | "card_moved"
  | "error";

export type BoardEvent = {
  id: string;
  type: BoardEventType;
  user_id: string;
  card_id: string;
  data?: Record<string, unknown>;
  timestamp: string;
};

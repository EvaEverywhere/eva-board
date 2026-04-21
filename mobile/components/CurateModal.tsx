import { useCallback, useMemo, useState } from "react";
import {
  ActivityIndicator,
  Modal,
  Pressable,
  ScrollView,
  View,
} from "react-native";
import { CheckCircle2, Sparkles, X } from "lucide-react-native";

import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card, CardContent } from "@/components/ui/Card";
import { EmptyState } from "@/components/ui/EmptyState";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import {
  applySpringClean,
  applyTriage,
  runCurate,
} from "@/services/board";
import type {
  BoardCard,
  CleanupAction,
  CleanupActionType,
  CurateResult,
  TriageProposal,
  TriageProposalType,
} from "@/services/boardTypes";

type Props = {
  visible: boolean;
  onClose: () => void;
  onApplied: () => void;
  cards: BoardCard[];
};

type ApplyState = "idle" | "applying" | "success" | "error";

const TRIAGE_BADGE: Record<
  TriageProposalType,
  { label: string; variant: "default" | "secondary" | "destructive" | "outline" }
> = {
  create: { label: "CREATE", variant: "default" },
  close: { label: "CLOSE", variant: "destructive" },
  rewrite: { label: "REWRITE", variant: "secondary" },
};

const CLEANUP_BADGE: Record<
  CleanupActionType,
  { label: string; variant: "default" | "secondary" | "destructive" | "outline" }
> = {
  delete_branch: { label: "DELETE BRANCH", variant: "destructive" },
  remove_worktree: { label: "REMOVE WORKTREE", variant: "destructive" },
  close_issue: { label: "CLOSE ISSUE", variant: "secondary" },
};

function proposalKey(p: TriageProposal, idx: number): string {
  return `${p.type}-${p.card_id ?? "new"}-${idx}`;
}

function actionKey(a: CleanupAction, idx: number): string {
  return `${a.type}-${a.target}-${idx}`;
}

export function CurateModal({ visible, onClose, onApplied, cards }: Props) {
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<CurateResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [approvedProposals, setApprovedProposals] = useState<Set<string>>(
    new Set(),
  );
  const [approvedActions, setApprovedActions] = useState<Set<string>>(
    new Set(),
  );
  const [applyState, setApplyState] = useState<ApplyState>("idle");
  const [applySummary, setApplySummary] = useState<string | null>(null);

  const cardsById = useMemo(() => {
    const map = new Map<string, BoardCard>();
    for (const c of cards) map.set(c.id, c);
    return map;
  }, [cards]);

  const reset = useCallback(() => {
    setLoading(false);
    setResult(null);
    setError(null);
    setApprovedProposals(new Set());
    setApprovedActions(new Set());
    setApplyState("idle");
    setApplySummary(null);
  }, []);

  const handleClose = useCallback(() => {
    reset();
    onClose();
  }, [onClose, reset]);

  const handleRun = useCallback(async () => {
    setLoading(true);
    setError(null);
    setResult(null);
    setApprovedProposals(new Set());
    setApprovedActions(new Set());
    setApplyState("idle");
    setApplySummary(null);
    try {
      const res = await runCurate();
      setResult(res);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to run curate");
    } finally {
      setLoading(false);
    }
  }, []);

  const toggleProposal = useCallback((key: string, approved: boolean) => {
    setApprovedProposals((prev) => {
      const next = new Set(prev);
      if (approved) next.add(key);
      else next.delete(key);
      return next;
    });
  }, []);

  const toggleAction = useCallback((key: string, approved: boolean) => {
    setApprovedActions((prev) => {
      const next = new Set(prev);
      if (approved) next.add(key);
      else next.delete(key);
      return next;
    });
  }, []);

  const handleApply = useCallback(async () => {
    if (!result) return;
    const proposals = (result.triage_proposals ?? []).filter((p, idx) =>
      approvedProposals.has(proposalKey(p, idx)),
    );
    const actions = (result.cleanup_actions ?? []).filter((a, idx) =>
      approvedActions.has(actionKey(a, idx)),
    );
    if (proposals.length === 0 && actions.length === 0) {
      setError("Approve at least one proposal or action before applying.");
      return;
    }
    setApplyState("applying");
    setError(null);
    try {
      if (proposals.length > 0) await applyTriage(proposals);
      if (actions.length > 0) await applySpringClean(actions);
      setApplyState("success");
      setApplySummary(
        `Applied ${proposals.length} triage proposal${proposals.length === 1 ? "" : "s"} and ${actions.length} cleanup action${actions.length === 1 ? "" : "s"}.`,
      );
      onApplied();
    } catch (err) {
      setApplyState("error");
      setError(err instanceof Error ? err.message : "Failed to apply changes");
    }
  }, [result, approvedProposals, approvedActions, onApplied]);

  const proposals = result?.triage_proposals ?? [];
  const actions = result?.cleanup_actions ?? [];
  const isEmpty =
    result !== null && proposals.length === 0 && actions.length === 0;
  const approvedCount = approvedProposals.size + approvedActions.size;

  return (
    <Modal
      visible={visible}
      transparent
      animationType="fade"
      onRequestClose={handleClose}
    >
      <View
        className="flex-1 items-center justify-center p-4"
        style={{ backgroundColor: "rgba(6,7,10,0.7)" }}
      >
        <View
          className="w-full rounded-2xl border border-border bg-card"
          style={{ maxWidth: 720, maxHeight: "90%" }}
        >
          <View className="flex-row items-center justify-between border-b border-border px-5 py-4">
            <View className="flex-row items-center gap-2">
              <Sparkles size={18} color="#00D4AA" />
              <Text variant="h4">Curate</Text>
            </View>
            <Pressable onPress={handleClose} accessibilityLabel="Close">
              <X size={18} color="#9AA4B2" />
            </Pressable>
          </View>

          <ScrollView
            className="px-5 py-4"
            contentContainerStyle={{ gap: 16 }}
          >
            {!result && !loading ? (
              <View className="items-center gap-3 py-8">
                <Text variant="small" className="text-center text-muted">
                  Run curate to scan your backlog for proposals and cleanup
                  actions. You'll review and approve each one before anything
                  is applied.
                </Text>
                <Button onPress={handleRun} icon={<Sparkles size={16} color="#06070A" />}>
                  Run Curate
                </Button>
              </View>
            ) : null}

            {loading ? (
              <View className="items-center justify-center gap-3 py-8">
                <ActivityIndicator color="#00D4AA" />
                <Text variant="small" className="text-muted">
                  Curating your backlog…
                </Text>
              </View>
            ) : null}

            {isEmpty ? (
              <EmptyState
                icon={<CheckCircle2 size={32} color="#00D4AA" />}
                title="All clean"
                description="Your backlog is in good shape — nothing to clean up."
              />
            ) : null}

            {result && proposals.length > 0 ? (
              <View className="gap-3">
                <Text variant="large">Triage proposals</Text>
                {proposals.map((p, idx) => {
                  const key = proposalKey(p, idx);
                  const approved = approvedProposals.has(key);
                  const original = p.card_id ? cardsById.get(p.card_id) : null;
                  const badge = TRIAGE_BADGE[p.type] ?? {
                    label: p.type.toUpperCase(),
                    variant: "outline" as const,
                  };
                  return (
                    <Card key={key}>
                      <CardContent>
                        <View className="flex-row items-center gap-2">
                          <Badge variant={badge.variant}>{badge.label}</Badge>
                          <Text variant="small" className="flex-1 font-semibold" numberOfLines={2}>
                            {p.title ?? original?.title ?? "(untitled)"}
                          </Text>
                        </View>
                        {p.type !== "create" && original ? (
                          <Text variant="small" className="text-muted">
                            Original card: {original.title}
                          </Text>
                        ) : null}
                        {p.description ? (
                          <Text variant="small" numberOfLines={4}>
                            {p.description}
                          </Text>
                        ) : null}
                        {p.acceptance_criteria && p.acceptance_criteria.length > 0 ? (
                          <View className="gap-1">
                            <Text variant="small" className="text-muted">
                              Acceptance criteria
                            </Text>
                            {p.acceptance_criteria.map((ac, i) => (
                              <Text key={i} variant="small">
                                • {ac}
                              </Text>
                            ))}
                          </View>
                        ) : null}
                        {p.reason ? (
                          <Text variant="small" className="italic text-muted">
                            {p.reason}
                          </Text>
                        ) : null}
                        <ApproveToggle
                          approved={approved}
                          onChange={(v) => toggleProposal(key, v)}
                        />
                      </CardContent>
                    </Card>
                  );
                })}
              </View>
            ) : null}

            {result && actions.length > 0 ? (
              <View className="gap-3">
                <Text variant="large">Cleanup actions</Text>
                {actions.map((a, idx) => {
                  const key = actionKey(a, idx);
                  const approved = approvedActions.has(key);
                  const badge = CLEANUP_BADGE[a.type] ?? {
                    label: a.type.toUpperCase(),
                    variant: "outline" as const,
                  };
                  return (
                    <Card key={key}>
                      <CardContent>
                        <View className="flex-row items-center gap-2">
                          <Badge variant={badge.variant}>{badge.label}</Badge>
                          <Text variant="small" className="flex-1 font-semibold" numberOfLines={2}>
                            {a.target}
                          </Text>
                        </View>
                        {a.reason ? (
                          <Text variant="small" className="italic text-muted">
                            {a.reason}
                          </Text>
                        ) : null}
                        <ApproveToggle
                          approved={approved}
                          onChange={(v) => toggleAction(key, v)}
                        />
                      </CardContent>
                    </Card>
                  );
                })}
              </View>
            ) : null}

            {error ? (
              <Text variant="small" className="text-destructive">
                {error}
              </Text>
            ) : null}

            {applyState === "success" && applySummary ? (
              <View className="rounded-xl border border-primary/30 bg-primary/10 p-3">
                <Text variant="small" className="text-primary">
                  {applySummary}
                </Text>
              </View>
            ) : null}
          </ScrollView>

          <View className="flex-row items-center justify-between border-t border-border px-5 py-3">
            <Text variant="small" className="text-muted">
              {result && !isEmpty
                ? `${approvedCount} approved`
                : ""}
            </Text>
            <View className="flex-row gap-2">
              <Button variant="outline" size="sm" onPress={handleClose}>
                {applyState === "success" ? "Close" : "Cancel"}
              </Button>
              {result && !isEmpty && applyState !== "success" ? (
                <Button
                  size="sm"
                  loading={applyState === "applying"}
                  onPress={handleApply}
                >
                  Apply approved
                </Button>
              ) : null}
            </View>
          </View>
        </View>
      </View>
    </Modal>
  );
}

function ApproveToggle({
  approved,
  onChange,
}: {
  approved: boolean;
  onChange: (approved: boolean) => void;
}) {
  return (
    <View className="mt-2 flex-row gap-2">
      <Pressable
        onPress={() => onChange(true)}
        className={cn(
          "flex-1 items-center justify-center rounded-lg border px-3 py-2",
          approved
            ? "border-primary bg-primary/15"
            : "border-border bg-transparent",
        )}
        accessibilityLabel="Approve"
      >
        <Text
          variant="small"
          className={cn("font-semibold", approved ? "text-primary" : "text-muted")}
        >
          Approve
        </Text>
      </Pressable>
      <Pressable
        onPress={() => onChange(false)}
        className={cn(
          "flex-1 items-center justify-center rounded-lg border px-3 py-2",
          !approved
            ? "border-destructive bg-destructive/15"
            : "border-border bg-transparent",
        )}
        accessibilityLabel="Reject"
      >
        <Text
          variant="small"
          className={cn(
            "font-semibold",
            !approved ? "text-destructive" : "text-muted",
          )}
        >
          Reject
        </Text>
      </Pressable>
    </View>
  );
}

// Card detail screen: where the autonomous loop becomes visible.
//
// Sections (top -> bottom):
//   - Header: editable title, column badge, agent status (with spinner
//     when running)
//   - Description: markdown editor with live preview; acceptance
//     criteria checkboxes are parsed from `- [ ]` lines and rendered
//     as a separate checklist
//   - Agent log: streaming output from the agent, populated by the
//     SSE stream filtered to this card
//   - Verification results: pass/fail per acceptance criterion, read
//     from card metadata (`verification_results`) or the latest
//     `verification_result` event
//   - Review feedback: LLM verdict + comments from card metadata
//     (`review`) or the latest `review_result` event
//   - Diff viewer: git diff for the agent's worktree branch
//   - PR link: opens the GitHub PR in a browser
//   - Actions: start/stop agent, send feedback, move column, delete
//
// Routing: lives at `/board-card/[id]` (rather than `/board/[id]`)
// because the existing tab layout has `board.tsx` as a leaf tab and
// nesting a directory under the same name fights with the Tabs router.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ActivityIndicator,
  Alert,
  Linking,
  Platform,
  Pressable,
  ScrollView,
  TextInput,
  View,
} from "react-native";
import { router, useLocalSearchParams } from "expo-router";
// react-native-markdown-display ships its own .d.ts but the import path
// is the package root. We type it here instead of fighting an external
// dependency's loose ASTNode types.
// eslint-disable-next-line @typescript-eslint/no-var-requires
import Markdown from "react-native-markdown-display";

import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/Card";
import { Separator } from "@/components/ui/Separator";
import { Text } from "@/components/ui/Text";
import { useBoardEvents } from "@/hooks/useBoardEvents";
import { parseAcceptanceCriteria } from "@/lib/acceptanceCriteria";
import {
  deleteCard,
  getCard,
  getCardDiff,
  moveCard,
  sendAgentFeedback,
  startAgent,
  stopAgent,
  updateCard,
  type CardDiff,
} from "@/services/board";
import type {
  AgentStatus,
  BoardCard,
  BoardColumn,
  BoardEvent,
} from "@/services/boardTypes";

const COLUMNS: BoardColumn[] = ["backlog", "develop", "review", "pr", "done"];

const AGENT_STATUS_VARIANT: Record<AgentStatus, "default" | "secondary" | "destructive" | "outline"> = {
  idle: "outline",
  running: "default",
  verifying: "default",
  reviewing: "default",
  failed: "destructive",
  succeeded: "secondary",
};

type AgentLogEntry = {
  id: string;
  type: BoardEvent["type"];
  text: string;
  timestamp: string;
};

type VerificationResult = {
  criterion: string;
  passed: boolean;
  notes?: string;
};

type ReviewSummary = {
  verdict?: string;
  comments?: string;
  raw?: unknown;
};

function asString(value: unknown): string | undefined {
  return typeof value === "string" ? value : undefined;
}

function readVerification(metadata: Record<string, unknown> | undefined): VerificationResult[] | null {
  if (!metadata) return null;
  const raw = metadata.verification_results ?? metadata.verification;
  if (!Array.isArray(raw)) return null;
  const out: VerificationResult[] = [];
  for (const item of raw) {
    if (!item || typeof item !== "object") continue;
    const r = item as Record<string, unknown>;
    const criterion = asString(r.criterion) ?? asString(r.text) ?? asString(r.name);
    if (!criterion) continue;
    const passed = Boolean(r.passed ?? r.pass ?? r.ok);
    out.push({ criterion, passed, notes: asString(r.notes) ?? asString(r.reason) });
  }
  return out.length > 0 ? out : null;
}

function readReview(
  metadata: Record<string, unknown> | undefined,
  reviewStatus: string | null | undefined,
): ReviewSummary | null {
  if (!metadata && !reviewStatus) return null;
  const raw = metadata?.review ?? metadata?.review_result;
  if (raw && typeof raw === "object") {
    const r = raw as Record<string, unknown>;
    return {
      verdict: asString(r.verdict) ?? asString(r.decision) ?? reviewStatus ?? undefined,
      comments: asString(r.comments) ?? asString(r.summary) ?? asString(r.feedback),
      raw,
    };
  }
  if (reviewStatus) {
    return { verdict: reviewStatus };
  }
  return null;
}

function eventToLogText(event: BoardEvent): string {
  const data = event.data ?? {};
  const message =
    asString(data.message) ?? asString(data.summary) ?? asString(data.status);
  if (message) return `[${event.type}] ${message}`;
  return `[${event.type}]`;
}

export default function CardDetailScreen() {
  const params = useLocalSearchParams<{ id: string }>();
  const cardId = typeof params.id === "string" ? params.id : "";

  const [card, setCard] = useState<BoardCard | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const [titleDraft, setTitleDraft] = useState("");
  const [titleEditing, setTitleEditing] = useState(false);
  const [savingTitle, setSavingTitle] = useState(false);

  const [descDraft, setDescDraft] = useState("");
  const [descEditing, setDescEditing] = useState(false);
  const [savingDesc, setSavingDesc] = useState(false);

  const [feedback, setFeedback] = useState("");
  const [sendingFeedback, setSendingFeedback] = useState(false);
  const [agentBusy, setAgentBusy] = useState(false);
  const [movingTo, setMovingTo] = useState<BoardColumn | null>(null);
  const [deleting, setDeleting] = useState(false);

  const [diff, setDiff] = useState<CardDiff | null>(null);
  const [diffLoading, setDiffLoading] = useState(false);
  const [diffError, setDiffError] = useState<string | null>(null);

  const [logs, setLogs] = useState<AgentLogEntry[]>([]);
  const [liveVerification, setLiveVerification] = useState<VerificationResult[] | null>(null);
  const [liveReview, setLiveReview] = useState<ReviewSummary | null>(null);

  const refresh = useCallback(async () => {
    if (!cardId) return;
    try {
      const fresh = await getCard(cardId);
      setCard(fresh);
      setTitleDraft(fresh.title);
      setDescDraft(fresh.description);
      setLoadError(null);
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : "Failed to load card");
    } finally {
      setLoading(false);
    }
  }, [cardId]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const loadDiff = useCallback(async () => {
    if (!cardId) return;
    setDiffLoading(true);
    setDiffError(null);
    try {
      const result = await getCardDiff(cardId);
      setDiff(result);
    } catch (err) {
      setDiffError(err instanceof Error ? err.message : "Failed to load diff");
    } finally {
      setDiffLoading(false);
    }
  }, [cardId]);

  useEffect(() => {
    void loadDiff();
  }, [loadDiff]);

  // Subscribe to SSE events scoped to this card. Web only — on native
  // the hook is a no-op and the screen falls back to the polled card
  // state.
  const handleEvent = useCallback(
    (event: BoardEvent) => {
      if (event.card_id !== cardId) return;
      setLogs((prev) =>
        [
          ...prev,
          {
            id: event.id,
            type: event.type,
            text: eventToLogText(event),
            timestamp: event.timestamp,
          },
        ].slice(-200),
      );

      if (event.type === "verification_result") {
        const data = event.data ?? {};
        const items = readVerification({ verification_results: data.results });
        if (items) setLiveVerification(items);
      }
      if (event.type === "review_result") {
        const data = event.data ?? {};
        setLiveReview({
          verdict: asString(data.verdict) ?? asString(data.decision),
          comments: asString(data.comments) ?? asString(data.summary),
          raw: data,
        });
      }
      if (
        event.type === "agent_finished" ||
        event.type === "verification_result" ||
        event.type === "review_result" ||
        event.type === "pr_created" ||
        event.type === "card_moved"
      ) {
        void refresh();
        if (event.type === "agent_finished" || event.type === "pr_created") {
          void loadDiff();
        }
      }
    },
    [cardId, refresh, loadDiff],
  );

  useBoardEvents({ enabled: Boolean(cardId), onEvent: handleEvent });

  const acceptanceCriteria = useMemo(
    () => parseAcceptanceCriteria(card?.description ?? ""),
    [card?.description],
  );

  const verification = liveVerification ?? readVerification(card?.metadata);
  const review = liveReview ?? readReview(card?.metadata, card?.review_status);

  const handleSaveTitle = async () => {
    if (!card) return;
    const next = titleDraft.trim();
    if (!next || next === card.title) {
      setTitleEditing(false);
      setTitleDraft(card.title);
      return;
    }
    setSavingTitle(true);
    try {
      const updated = await updateCard(card.id, { title: next });
      setCard(updated);
      setTitleEditing(false);
    } catch (err) {
      Alert.alert("Save failed", err instanceof Error ? err.message : "Could not update title");
    } finally {
      setSavingTitle(false);
    }
  };

  const handleSaveDescription = async () => {
    if (!card) return;
    if (descDraft === card.description) {
      setDescEditing(false);
      return;
    }
    setSavingDesc(true);
    try {
      const updated = await updateCard(card.id, { description: descDraft });
      setCard(updated);
      setDescEditing(false);
    } catch (err) {
      Alert.alert("Save failed", err instanceof Error ? err.message : "Could not update description");
    } finally {
      setSavingDesc(false);
    }
  };

  const handleStartAgent = async () => {
    if (!card) return;
    setAgentBusy(true);
    try {
      await startAgent(card.id);
      await refresh();
    } catch (err) {
      Alert.alert("Start failed", err instanceof Error ? err.message : "Could not start agent");
    } finally {
      setAgentBusy(false);
    }
  };

  const handleStopAgent = async () => {
    if (!card) return;
    setAgentBusy(true);
    try {
      await stopAgent(card.id);
      await refresh();
    } catch (err) {
      Alert.alert("Stop failed", err instanceof Error ? err.message : "Could not stop agent");
    } finally {
      setAgentBusy(false);
    }
  };

  const handleSendFeedback = async () => {
    if (!card) return;
    const text = feedback.trim();
    if (!text) return;
    setSendingFeedback(true);
    try {
      await sendAgentFeedback(card.id, text);
      setFeedback("");
    } catch (err) {
      Alert.alert("Send failed", err instanceof Error ? err.message : "Could not send feedback");
    } finally {
      setSendingFeedback(false);
    }
  };

  const handleMove = async (target: BoardColumn) => {
    if (!card || target === card.column) return;
    setMovingTo(target);
    try {
      const updated = await moveCard(card.id, { to_column: target, to_position: 0 });
      setCard(updated);
    } catch (err) {
      Alert.alert("Move failed", err instanceof Error ? err.message : "Could not move card");
    } finally {
      setMovingTo(null);
    }
  };

  const confirmDelete = () => {
    if (!card) return;
    const doDelete = async () => {
      setDeleting(true);
      try {
        await deleteCard(card.id);
        router.back();
      } catch (err) {
        Alert.alert("Delete failed", err instanceof Error ? err.message : "Could not delete card");
        setDeleting(false);
      }
    };
    if (Platform.OS === "web") {
      // Alert.alert with buttons is a no-op on web; fall back to the
      // browser's native confirm dialog so destructive actions still
      // require explicit consent.
      // eslint-disable-next-line no-alert
      if (typeof window !== "undefined" && window.confirm("Delete this card? This cannot be undone.")) {
        void doDelete();
      }
      return;
    }
    Alert.alert("Delete card", "This cannot be undone.", [
      { text: "Cancel", style: "cancel" },
      { text: "Delete", style: "destructive", onPress: () => void doDelete() },
    ]);
  };

  const openPR = () => {
    if (!card?.pr_url) return;
    void Linking.openURL(card.pr_url);
  };

  if (loading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  if (loadError || !card) {
    return (
      <View className="flex-1 items-center justify-center bg-background gap-3 p-6">
        <Text variant="h4">Card not available</Text>
        <Text variant="muted">{loadError ?? "Unknown error"}</Text>
        <Button variant="outline" onPress={() => router.back()}>Back</Button>
      </View>
    );
  }

  const agentRunning =
    card.agent_status === "running" ||
    card.agent_status === "verifying" ||
    card.agent_status === "reviewing";

  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerStyle={{ padding: 20, paddingBottom: 160, gap: 16 }}
    >
      <View className="flex-row items-center justify-between">
        <Pressable onPress={() => router.back()} className="active:opacity-70">
          <Text variant="small" className="text-primary">{"< Back"}</Text>
        </Pressable>
        <Button variant="ghost" size="sm" onPress={() => void refresh()}>
          Refresh
        </Button>
      </View>

      <Card>
        <CardHeader>
          <View className="flex-row items-center gap-2">
            <Badge variant="outline">{card.column}</Badge>
            <Badge variant={AGENT_STATUS_VARIANT[card.agent_status]}>
              <View className="flex-row items-center gap-1">
                {agentRunning ? <ActivityIndicator size="small" /> : null}
                <Text variant="small">{card.agent_status}</Text>
              </View>
            </Badge>
          </View>
        </CardHeader>
        <CardContent>
          {titleEditing ? (
            <View className="gap-2">
              <TextInput
                value={titleDraft}
                onChangeText={setTitleDraft}
                autoFocus
                multiline
                className="rounded-lg border border-border bg-card px-3 py-2 text-foreground text-2xl font-bold"
                placeholderTextColor="#9AA4B2"
              />
              <View className="flex-row gap-2">
                <Button size="sm" onPress={handleSaveTitle} loading={savingTitle}>
                  Save
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  onPress={() => {
                    setTitleEditing(false);
                    setTitleDraft(card.title);
                  }}
                >
                  Cancel
                </Button>
              </View>
            </View>
          ) : (
            <Pressable onPress={() => setTitleEditing(true)} className="active:opacity-70">
              <Text variant="h2">{card.title || "Untitled"}</Text>
              <Text variant="muted" className="mt-1">Tap to edit</Text>
            </Pressable>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Description</CardTitle>
        </CardHeader>
        <CardContent className="gap-3">
          {descEditing ? (
            <View className="gap-2">
              <TextInput
                value={descDraft}
                onChangeText={setDescDraft}
                multiline
                numberOfLines={10}
                className="min-h-[160px] rounded-lg border border-border bg-card px-3 py-2 text-foreground"
                placeholder="Describe the work. Use `- [ ] criterion` lines for acceptance criteria."
                placeholderTextColor="#9AA4B2"
                textAlignVertical="top"
              />
              <View className="flex-row gap-2">
                <Button size="sm" onPress={handleSaveDescription} loading={savingDesc}>
                  Save
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  onPress={() => {
                    setDescEditing(false);
                    setDescDraft(card.description);
                  }}
                >
                  Cancel
                </Button>
              </View>
            </View>
          ) : (
            <Pressable onPress={() => setDescEditing(true)} className="active:opacity-70">
              {card.description ? (
                <Markdown style={markdownStyles}>{card.description}</Markdown>
              ) : (
                <Text variant="muted">No description. Tap to add.</Text>
              )}
              <Text variant="muted" className="mt-2">Tap to edit</Text>
            </Pressable>
          )}

          {acceptanceCriteria.length > 0 ? (
            <View className="mt-2 gap-2">
              <Text variant="large">Acceptance criteria</Text>
              {acceptanceCriteria.map((item, idx) => (
                <View key={`${item.line}-${idx}`} className="flex-row items-start gap-2">
                  <View
                    className={`mt-1 h-4 w-4 items-center justify-center rounded border ${
                      item.checked ? "border-primary bg-primary" : "border-border"
                    }`}
                  >
                    {item.checked ? (
                      <Text variant="small" className="text-background">{"✓"}</Text>
                    ) : null}
                  </View>
                  <Text variant="small" className="flex-1">{item.text}</Text>
                </View>
              ))}
            </View>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Agent log</CardTitle>
        </CardHeader>
        <CardContent>
          {Platform.OS !== "web" ? (
            <Text variant="muted">
              Live agent log is web-only in this build.
            </Text>
          ) : logs.length === 0 ? (
            <Text variant="muted">
              {agentRunning
                ? "Waiting for agent output..."
                : "No agent activity yet."}
            </Text>
          ) : (
            <View className="gap-1">
              {logs.map((entry) => (
                <View key={entry.id} className="flex-row gap-2">
                  <Text variant="muted" className="text-xs">
                    {new Date(entry.timestamp).toLocaleTimeString()}
                  </Text>
                  <Text variant="small" className="flex-1">{entry.text}</Text>
                </View>
              ))}
            </View>
          )}
        </CardContent>
      </Card>

      {verification && verification.length > 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>Verification</CardTitle>
          </CardHeader>
          <CardContent className="gap-2">
            {verification.map((item, idx) => (
              <View key={idx} className="gap-1">
                <View className="flex-row items-center gap-2">
                  <Badge variant={item.passed ? "secondary" : "destructive"}>
                    {item.passed ? "PASS" : "FAIL"}
                  </Badge>
                  <Text variant="small" className="flex-1">{item.criterion}</Text>
                </View>
                {item.notes ? (
                  <Text variant="muted" className="ml-2">{item.notes}</Text>
                ) : null}
              </View>
            ))}
          </CardContent>
        </Card>
      ) : null}

      {review ? (
        <Card>
          <CardHeader>
            <CardTitle>Review</CardTitle>
          </CardHeader>
          <CardContent className="gap-2">
            {review.verdict ? (
              <Badge
                variant={
                  /approve|pass|ok/i.test(review.verdict)
                    ? "secondary"
                    : /reject|fail/i.test(review.verdict)
                      ? "destructive"
                      : "default"
                }
              >
                {review.verdict}
              </Badge>
            ) : null}
            {review.comments ? (
              <Markdown style={markdownStyles}>{review.comments}</Markdown>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <View className="flex-row items-center justify-between">
            <CardTitle>Diff</CardTitle>
            <Button size="sm" variant="ghost" onPress={() => void loadDiff()}>
              Reload
            </Button>
          </View>
        </CardHeader>
        <CardContent>
          {diffLoading ? (
            <ActivityIndicator />
          ) : diffError ? (
            <Text variant="muted" className="text-destructive">{diffError}</Text>
          ) : !diff || !diff.diff ? (
            <Text variant="muted">
              {diff?.error
                ? `No diff yet (${diff.error})`
                : diff?.branch
                  ? "Branch exists but contains no changes."
                  : "Agent has not produced any changes yet."}
            </Text>
          ) : (
            <DiffView diff={diff.diff} />
          )}
        </CardContent>
      </Card>

      {card.pr_number && card.pr_url ? (
        <Button onPress={openPR}>{`View PR #${card.pr_number} on GitHub`}</Button>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>Agent control</CardTitle>
        </CardHeader>
        <CardContent className="gap-3">
          <View className="flex-row gap-2">
            <Button
              onPress={handleStartAgent}
              loading={agentBusy && !agentRunning}
              disabled={agentRunning}
            >
              Start agent
            </Button>
            <Button
              variant="outline"
              onPress={handleStopAgent}
              loading={agentBusy && agentRunning}
              disabled={!agentRunning}
            >
              Stop agent
            </Button>
          </View>

          <Separator />

          <View className="gap-2">
            <Text variant="small">Send feedback to agent</Text>
            <TextInput
              value={feedback}
              onChangeText={setFeedback}
              multiline
              numberOfLines={3}
              placeholder="Tell the agent what to do differently..."
              placeholderTextColor="#9AA4B2"
              className="min-h-[72px] rounded-lg border border-border bg-card px-3 py-2 text-foreground"
              textAlignVertical="top"
            />
            <Button
              onPress={handleSendFeedback}
              loading={sendingFeedback}
              disabled={!feedback.trim()}
            >
              Send feedback
            </Button>
          </View>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Move to column</CardTitle>
        </CardHeader>
        <CardContent>
          <View className="flex-row flex-wrap gap-2">
            {COLUMNS.map((col) => (
              <Button
                key={col}
                size="sm"
                variant={col === card.column ? "default" : "outline"}
                onPress={() => void handleMove(col)}
                loading={movingTo === col}
                disabled={col === card.column}
              >
                {col}
              </Button>
            ))}
          </View>
        </CardContent>
      </Card>

      <Button variant="destructive" onPress={confirmDelete} loading={deleting}>
        Delete card
      </Button>
    </ScrollView>
  );
}

// DiffView renders a unified diff with simple per-line coloring. We
// purposely avoid a heavy diff library — the agent diffs we display
// are already in `git diff` format and a line-by-line color pass is
// readable enough for v1.
function DiffView({ diff }: { diff: string }) {
  const lines = diff.split(/\r?\n/);
  return (
    <View className="rounded-lg border border-border bg-card p-2">
      <ScrollView horizontal showsHorizontalScrollIndicator>
        <View>
          {lines.map((line, idx) => {
            const colour = lineClass(line);
            return (
              <Text
                key={idx}
                className={`font-mono text-xs ${colour}`}
                style={{ fontFamily: Platform.select({ web: "ui-monospace, SFMono-Regular, Menlo, monospace", default: "Courier" }) }}
              >
                {line || " "}
              </Text>
            );
          })}
        </View>
      </ScrollView>
    </View>
  );
}

function lineClass(line: string): string {
  if (line.startsWith("+++") || line.startsWith("---")) return "text-foreground/80";
  if (line.startsWith("@@")) return "text-primary";
  if (line.startsWith("+")) return "text-emerald-400";
  if (line.startsWith("-")) return "text-rose-400";
  return "text-foreground/80";
}

// react-native-markdown-display takes a style object keyed by node
// type. We only override the colors we need so dark-mode text stays
// legible on the card background.
const markdownStyles = {
  body: { color: "#E5E7EB" },
  heading1: { color: "#F9FAFB" },
  heading2: { color: "#F9FAFB" },
  heading3: { color: "#F9FAFB" },
  code_inline: {
    backgroundColor: "rgba(255,255,255,0.08)",
    color: "#F9FAFB",
    paddingHorizontal: 4,
    borderRadius: 4,
  },
  fence: {
    backgroundColor: "rgba(255,255,255,0.05)",
    color: "#F9FAFB",
    padding: 8,
    borderRadius: 6,
  },
  link: { color: "#60A5FA" },
  bullet_list: { color: "#E5E7EB" },
  ordered_list: { color: "#E5E7EB" },
};

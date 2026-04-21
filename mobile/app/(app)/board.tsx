import { useCallback, useEffect, useMemo, useState } from "react";
import {
  ActivityIndicator,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  View,
} from "react-native";
import { useLocalSearchParams, useRouter } from "expo-router";
import { ExternalLink, Plus, Sparkles, Trash2, X } from "lucide-react-native";

import { CurateModal } from "@/components/CurateModal";
import { RepoSwitcher } from "@/components/RepoSwitcher";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { useBoardEvents } from "@/hooks/useBoardEvents";
import { useBoardRepos } from "@/hooks/useBoardRepos";
import { createCard, draftCard, listCards, moveCard } from "@/services/board";
import {
  BOARD_COLUMN_LABELS,
  BOARD_COLUMNS,
  type AgentStatus,
  type BoardCard,
  type BoardColumn,
} from "@/services/boardTypes";

// @hello-pangea/dnd is web-only. Loading it on native would crash because
// it depends on the DOM. We require it conditionally at module init so
// the import is never evaluated on iOS/Android.
type DndModule = typeof import("@hello-pangea/dnd");
const dnd: DndModule | null =
  Platform.OS === "web" ? (require("@hello-pangea/dnd") as DndModule) : null;

const STATUS_COLORS: Record<AgentStatus, { bg: string; fg: string; label: string }> = {
  idle: { bg: "#1F2630", fg: "#9AA4B2", label: "Idle" },
  running: { bg: "#1E3A8A", fg: "#93C5FD", label: "Running" },
  verifying: { bg: "#78350F", fg: "#FCD34D", label: "Verifying" },
  reviewing: { bg: "#78350F", fg: "#FCD34D", label: "Reviewing" },
  failed: { bg: "#7F1D1D", fg: "#FCA5A5", label: "Failed" },
  succeeded: { bg: "#14532D", fg: "#86EFAC", label: "Succeeded" },
};

function groupByColumn(cards: BoardCard[]): Record<BoardColumn, BoardCard[]> {
  const grouped: Record<BoardColumn, BoardCard[]> = {
    backlog: [],
    develop: [],
    review: [],
    pr: [],
    done: [],
  };
  for (const card of cards) {
    grouped[card.column]?.push(card);
  }
  for (const col of BOARD_COLUMNS) {
    grouped[col].sort((a, b) => a.position - b.position);
  }
  return grouped;
}

export default function BoardScreen() {
  const router = useRouter();
  const params = useLocalSearchParams<{ repo?: string }>();
  const initialRepoId = typeof params.repo === "string" ? params.repo : undefined;
  const repos = useBoardRepos(initialRepoId);
  const selectedRepo = repos.selected;
  const [cards, setCards] = useState<BoardCard[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showNewCard, setShowNewCard] = useState(false);
  const [showCurate, setShowCurate] = useState(false);

  const refresh = useCallback(async () => {
    if (!selectedRepo) {
      // No repo selected — either repos are still loading or user has none.
      setCards([]);
      setLoading(repos.isLoading);
      return;
    }
    try {
      const next = await listCards({ repoId: selectedRepo.id });
      setCards(next);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load board");
    } finally {
      setLoading(false);
    }
  }, [selectedRepo, repos.isLoading]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // Reflect selection changes in the URL so links to the board are
  // shareable and survive a reload via the ?repo param.
  useEffect(() => {
    if (selectedRepo && initialRepoId !== selectedRepo.id) {
      router.setParams({ repo: selectedRepo.id });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedRepo?.id]);

  useBoardEvents({
    onEvent: (event) => {
      // For any board event, reload the canonical state from the API.
      // This is simple and correct; we can optimise per-event later.
      if (
        event.type === "card_moved" ||
        event.type === "agent_started" ||
        event.type === "agent_finished" ||
        event.type === "agent_progress" ||
        event.type === "verification_started" ||
        event.type === "verification_result" ||
        event.type === "review_started" ||
        event.type === "review_result" ||
        event.type === "pr_created"
      ) {
        void refresh();
      }
    },
  });

  const grouped = useMemo(() => groupByColumn(cards), [cards]);

  const openCard = useCallback(
    (id: string) => {
      router.push(`/board-card/${id}` as never);
    },
    [router],
  );

  // Native long-press → bottom sheet column picker. On web we let
  // @hello-pangea/dnd own the move UX and skip wiring the sheet at
  // all (long-press would conflict with click + drag detection).
  const [moveTarget, setMoveTarget] = useState<BoardCard | null>(null);
  const handleLongPress = useCallback((card: BoardCard) => {
    if (Platform.OS === "web") return;
    setMoveTarget(card);
  }, []);

  const handleMove = useCallback(
    async (card: BoardCard, toColumn: BoardColumn) => {
      setMoveTarget(null);
      if (card.column === toColumn) return;
      const previous = cards;
      setCards((prev) =>
        prev.map((c) =>
          c.id === card.id ? { ...c, column: toColumn, position: 0 } : c,
        ),
      );
      try {
        await moveCard(card.id, { to_column: toColumn, to_position: 0 });
        await refresh();
      } catch (err) {
        setCards(previous);
        setError(err instanceof Error ? err.message : "Failed to move card");
      }
    },
    [cards, refresh],
  );

  const handleCreateCard = useCallback(
    async (title: string, description: string) => {
      if (!selectedRepo) return;
      const created = await createCard({ title, description }, selectedRepo.id);
      setCards((prev) => [...prev, created]);
    },
    [selectedRepo],
  );

  const handleDragEnd = useCallback(
    async (result: { source: { droppableId: string; index: number }; destination?: { droppableId: string; index: number } | null; draggableId: string }) => {
      const { source, destination, draggableId } = result;
      if (!destination) return;
      if (
        source.droppableId === destination.droppableId &&
        source.index === destination.index
      ) {
        return;
      }
      const toColumn = destination.droppableId as BoardColumn;
      const toPosition = destination.index;

      const previous = cards;
      // Optimistic: rebuild card list with the card moved.
      const moving = cards.find((c) => c.id === draggableId);
      if (!moving) return;

      const next = cards
        .filter((c) => c.id !== draggableId)
        .map((c) => ({ ...c }));
      const inDest = next
        .filter((c) => c.column === toColumn)
        .sort((a, b) => a.position - b.position);
      inDest.splice(toPosition, 0, { ...moving, column: toColumn });
      // Reassign positions for the destination column.
      const destIds = new Set(inDest.map((c) => c.id));
      const reflowed: BoardCard[] = next
        .filter((c) => !destIds.has(c.id) && c.column !== toColumn)
        .concat(
          inDest.map((c, idx) => ({ ...c, column: toColumn, position: idx })),
        );
      setCards(reflowed);

      try {
        await moveCard(draggableId, {
          to_column: toColumn,
          to_position: toPosition,
        });
      } catch (err) {
        setCards(previous);
        setError(err instanceof Error ? err.message : "Failed to move card");
      }
    },
    [cards],
  );

  if (loading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator color="#00D4AA" />
      </View>
    );
  }

  // Empty state when the user has no connected repos. Disable card
  // creation until they add one in Settings → Manage repos.
  if (!repos.isLoading && repos.repos.length === 0) {
    return (
      <View className="flex-1 items-center justify-center bg-background gap-4 p-8">
        <Text variant="h3">No repos connected</Text>
        <Text variant="muted" className="text-center max-w-md">
          Eva Board scopes cards to a GitHub repository. Connect one to get started.
        </Text>
        <Button onPress={() => router.push("/repos" as never)} icon={<Plus size={16} color="#06070A" />}>
          Add a repo
        </Button>
      </View>
    );
  }

  return (
    <View className="flex-1 bg-background">
      <View className="flex-row items-center justify-between border-b border-border px-5 py-4">
        <View className="flex-row items-center gap-3">
          <Text variant="h3">Board</Text>
          <RepoSwitcher
            repos={repos.repos}
            selected={selectedRepo}
            onSelect={repos.selectRepo}
            isLoading={repos.isLoading}
          />
          {error ? (
            <Text variant="small" className="text-destructive">
              {error}
            </Text>
          ) : null}
        </View>
        <View className="flex-row items-center gap-2">
          <Button
            size="sm"
            variant="outline"
            onPress={() => setShowCurate(true)}
            icon={<Sparkles size={16} color="#F8FAFC" />}
            disabled={!selectedRepo}
          >
            Curate
          </Button>
          <Button
            size="sm"
            onPress={() => setShowNewCard(true)}
            icon={<Plus size={16} color="#06070A" />}
            disabled={!selectedRepo}
          >
            New card
          </Button>
        </View>
      </View>

      {dnd ? (
        <dnd.DragDropContext onDragEnd={handleDragEnd}>
          <ColumnsRow
            grouped={grouped}
            onOpen={openCard}
            onLongPress={handleLongPress}
            onNewCard={() => setShowNewCard(true)}
            dnd={dnd}
          />
        </dnd.DragDropContext>
      ) : (
        <ColumnsRow
          grouped={grouped}
          onOpen={openCard}
          onLongPress={handleLongPress}
          onNewCard={() => setShowNewCard(true)}
          dnd={null}
        />
      )}

      <MoveCardSheet
        card={moveTarget}
        onClose={() => setMoveTarget(null)}
        onMove={handleMove}
      />

      <NewCardModal
        visible={showNewCard}
        onClose={() => setShowNewCard(false)}
        onCreate={handleCreateCard}
        repoId={selectedRepo?.id}
      />

      <CurateModal
        visible={showCurate}
        onClose={() => setShowCurate(false)}
        onApplied={() => {
          void refresh();
        }}
        cards={cards}
        repoId={selectedRepo?.id}
      />
    </View>
  );
}

type ColumnsRowProps = {
  grouped: Record<BoardColumn, BoardCard[]>;
  onOpen: (id: string) => void;
  onLongPress: (card: BoardCard) => void;
  onNewCard: () => void;
  dnd: DndModule | null;
};

function ColumnsRow({ grouped, onOpen, onLongPress, onNewCard, dnd }: ColumnsRowProps) {
  return (
    <ScrollView
      horizontal
      className="flex-1"
      contentContainerStyle={{ padding: 16, gap: 12 }}
      showsHorizontalScrollIndicator={false}
    >
      {BOARD_COLUMNS.map((col) => (
        <Column
          key={col}
          column={col}
          cards={grouped[col]}
          onOpen={onOpen}
          onLongPress={onLongPress}
          onNewCard={col === "backlog" ? onNewCard : undefined}
          dnd={dnd}
        />
      ))}
    </ScrollView>
  );
}

type ColumnProps = {
  column: BoardColumn;
  cards: BoardCard[];
  onOpen: (id: string) => void;
  onLongPress: (card: BoardCard) => void;
  onNewCard?: () => void;
  dnd: DndModule | null;
};

function Column({ column, cards, onOpen, onLongPress, onNewCard, dnd }: ColumnProps) {
  const header = (
    <View className="mb-3 flex-row items-center justify-between">
      <View className="flex-row items-center gap-2">
        <Text variant="large">{BOARD_COLUMN_LABELS[column]}</Text>
        <Text variant="small" className="text-muted">
          {cards.length}
        </Text>
      </View>
      {onNewCard ? (
        <Pressable
          onPress={onNewCard}
          className="h-7 w-7 items-center justify-center rounded-md bg-secondary"
          accessibilityLabel="Add card"
        >
          <Plus size={14} color="#F8FAFC" />
        </Pressable>
      ) : null}
    </View>
  );

  const empty =
    column === "backlog" && cards.length === 0 ? (
      <View className="items-center justify-center rounded-xl border border-dashed border-border p-6">
        <Text variant="small" className="text-center text-muted">
          Add your first card to get started
        </Text>
      </View>
    ) : null;

  if (dnd) {
    const { Droppable, Draggable } = dnd;
    return (
      <View
        className="rounded-2xl border border-border bg-card/40 p-3"
        style={{ width: 300, minHeight: 200 }}
      >
        {header}
        <Droppable droppableId={column}>
          {(provided) => (
            <View
              // The dnd lib expects a DOM ref; on react-native-web View
              // forwards refs as the underlying div.
              ref={provided.innerRef as unknown as React.Ref<View>}
              {...(provided.droppableProps as object)}
              style={{ gap: 8, minHeight: 100 }}
            >
              {cards.map((card, idx) => (
                <Draggable key={card.id} draggableId={card.id} index={idx}>
                  {(dragProvided) => (
                    <View
                      ref={dragProvided.innerRef as unknown as React.Ref<View>}
                      {...(dragProvided.draggableProps as object)}
                      {...(dragProvided.dragHandleProps as object)}
                    >
                      <BoardCardView
                        card={card}
                        onOpen={onOpen}
                        onLongPress={onLongPress}
                      />
                    </View>
                  )}
                </Draggable>
              ))}
              {provided.placeholder as unknown as React.ReactNode}
              {empty}
            </View>
          )}
        </Droppable>
      </View>
    );
  }

  return (
    <View
      className="rounded-2xl border border-border bg-card/40 p-3"
      style={{ width: 300, minHeight: 200 }}
    >
      {header}
      <View style={{ gap: 8 }}>
        {cards.map((card) => (
          <BoardCardView
            key={card.id}
            card={card}
            onOpen={onOpen}
            onLongPress={onLongPress}
          />
        ))}
        {empty}
      </View>
    </View>
  );
}

function BoardCardView({
  card,
  onOpen,
  onLongPress,
}: {
  card: BoardCard;
  onOpen: (id: string) => void;
  onLongPress: (card: BoardCard) => void;
}) {
  const status = STATUS_COLORS[card.agent_status] ?? STATUS_COLORS.idle;
  return (
    <Pressable
      onPress={() => onOpen(card.id)}
      onLongPress={() => onLongPress(card)}
      delayLongPress={350}
      className="rounded-xl border border-border bg-card p-3 active:opacity-80"
    >
      <Text variant="small" numberOfLines={2} className="font-semibold">
        {card.title}
      </Text>
      <View className="mt-3 flex-row items-center justify-between">
        <View
          style={{
            backgroundColor: status.bg,
            paddingHorizontal: 8,
            paddingVertical: 2,
            borderRadius: 999,
            flexDirection: "row",
            alignItems: "center",
            gap: 6,
          }}
        >
          {card.agent_status === "running" ? (
            <ActivityIndicator size="small" color={status.fg} />
          ) : null}
          <Text variant="small" style={{ color: status.fg }}>
            {status.label}
          </Text>
        </View>
        {card.pr_number ? (
          <View className="flex-row items-center gap-1">
            <ExternalLink size={12} color="#9AA4B2" />
            <Text variant="small" className="text-muted">
              #{card.pr_number}
            </Text>
          </View>
        ) : null}
      </View>
    </Pressable>
  );
}

type NewCardModalProps = {
  visible: boolean;
  onClose: () => void;
  onCreate: (title: string, description: string) => Promise<void>;
  repoId?: string;
};

// NewCardModal has two phases.
//   idea: the user types a rough title + description and either hits
//         Create (save raw, existing behavior) or Draft with AI (ask
//         the codegen agent to expand it into a structured draft).
//   edit: shown after Draft returns; user can tweak title, description
//         and each AC row inline, or hit Try again to re-draft with
//         the edited prompt. Save stitches the edited AC back into the
//         description as markdown checkboxes so the existing verify.go
//         pipeline picks them up unchanged.
type NewCardPhase = "idea" | "edit";

// composeDescription stitches the edited description and AC list back
// into a single markdown body. It matches the format the backend
// verify pipeline scans for ("- [ ] <item>"). When there are no AC
// items we return the description untouched so "Create" from the idea
// phase behaves exactly like before.
function composeDescription(description: string, ac: string[]): string {
  const items = ac.map((a) => a.trim()).filter(Boolean);
  const body = description.trim();
  if (items.length === 0) {
    return body;
  }
  const checklist = items.map((a) => `- [ ] ${a}`).join("\n");
  return body
    ? `${body}\n\n## Acceptance criteria\n\n${checklist}`
    : `## Acceptance criteria\n\n${checklist}`;
}

function NewCardModal({ visible, onClose, onCreate, repoId }: NewCardModalProps) {
  const [phase, setPhase] = useState<NewCardPhase>("idea");
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [criteria, setCriteria] = useState<string[]>([]);
  const [reasoning, setReasoning] = useState("");
  const [drafting, setDrafting] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reset = useCallback(() => {
    setPhase("idea");
    setTitle("");
    setDescription("");
    setCriteria([]);
    setReasoning("");
    setError(null);
    setDrafting(false);
    setSubmitting(false);
  }, []);

  const handleClose = useCallback(() => {
    reset();
    onClose();
  }, [onClose, reset]);

  const runDraft = useCallback(async () => {
    if (!title.trim()) {
      setError("Title is required");
      return;
    }
    setDrafting(true);
    setError(null);
    try {
      const draft = await draftCard({
        title: title.trim(),
        description: description.trim(),
        repo_id: repoId,
      });
      setTitle(draft.title);
      setDescription(draft.description);
      setCriteria(draft.acceptance_criteria ?? []);
      setReasoning(draft.reasoning ?? "");
      setPhase("edit");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to draft card");
    } finally {
      setDrafting(false);
    }
  }, [title, description, repoId]);

  const handleCreateRaw = async () => {
    if (!title.trim()) {
      setError("Title is required");
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await onCreate(title.trim(), description.trim());
      reset();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create card");
      setSubmitting(false);
    }
  };

  const handleSaveDraft = async () => {
    if (!title.trim()) {
      setError("Title is required");
      return;
    }
    const composed = composeDescription(description, criteria);
    setSubmitting(true);
    setError(null);
    try {
      await onCreate(title.trim(), composed);
      reset();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create card");
      setSubmitting(false);
    }
  };

  const updateCriterion = (idx: number, value: string) => {
    setCriteria((prev) => prev.map((c, i) => (i === idx ? value : c)));
  };

  const removeCriterion = (idx: number) => {
    setCriteria((prev) => prev.filter((_, i) => i !== idx));
  };

  const addCriterion = () => {
    setCriteria((prev) => [...prev, ""]);
  };

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
          className="w-full rounded-2xl border border-border bg-card p-5"
          style={{ maxWidth: 520, maxHeight: "90%" }}
        >
          <View className="mb-4 flex-row items-center justify-between">
            <Text variant="h4">
              {phase === "idea" ? "New card" : "Edit draft"}
            </Text>
            <Pressable onPress={handleClose} accessibilityLabel="Close">
              <X size={18} color="#9AA4B2" />
            </Pressable>
          </View>
          <ScrollView
            className="w-full"
            contentContainerStyle={{ gap: 12 }}
            keyboardShouldPersistTaps="handled"
          >
            {error ? (
              <Text variant="small" className="text-destructive">
                {error}
              </Text>
            ) : null}
            <Input
              label="Title"
              value={title}
              onChangeText={setTitle}
              placeholder={
                phase === "idea" ? "What needs doing?" : "Card title"
              }
              autoFocus={phase === "idea"}
            />
            <Input
              label="Description"
              value={description}
              onChangeText={setDescription}
              placeholder={
                phase === "idea"
                  ? "Optional details, links..."
                  : "Describe the work"
              }
              multiline
              numberOfLines={phase === "edit" ? 6 : 4}
              style={{
                minHeight: phase === "edit" ? 120 : 96,
                textAlignVertical: "top",
              }}
            />
            {phase === "edit" ? (
              <View className="gap-2">
                {reasoning ? (
                  <Text variant="small" className="italic text-muted">
                    AI note: {reasoning}
                  </Text>
                ) : null}
                <Text variant="small">Acceptance criteria</Text>
                {criteria.map((criterion, idx) => (
                  <View
                    key={`ac-${idx}`}
                    className="flex-row items-start gap-2"
                  >
                    <View className="flex-1">
                      <Input
                        value={criterion}
                        onChangeText={(value) => updateCriterion(idx, value)}
                        placeholder="X does Y when Z"
                      />
                    </View>
                    <Pressable
                      onPress={() => removeCriterion(idx)}
                      className="h-11 w-11 items-center justify-center rounded-lg border border-border"
                      accessibilityLabel={`Remove criterion ${idx + 1}`}
                    >
                      <Trash2 size={16} color="#9AA4B2" />
                    </Pressable>
                  </View>
                ))}
                <Button
                  variant="outline"
                  size="sm"
                  onPress={addCriterion}
                  icon={<Plus size={14} color="#F8FAFC" />}
                >
                  Add criterion
                </Button>
              </View>
            ) : null}
          </ScrollView>
          <View className="mt-4 flex-row flex-wrap justify-end gap-2">
            {phase === "idea" ? (
              <>
                <Button
                  variant="outline"
                  size="sm"
                  onPress={handleClose}
                  disabled={drafting || submitting}
                >
                  Cancel
                </Button>
                <Button
                  variant="secondary"
                  size="sm"
                  loading={drafting}
                  disabled={submitting}
                  onPress={runDraft}
                  icon={
                    drafting ? undefined : (
                      <Sparkles size={14} color="#F8FAFC" />
                    )
                  }
                >
                  {drafting ? "Drafting..." : "Draft with AI"}
                </Button>
                <Button
                  size="sm"
                  loading={submitting}
                  disabled={drafting}
                  onPress={handleCreateRaw}
                >
                  Create
                </Button>
              </>
            ) : (
              <>
                <Button
                  variant="outline"
                  size="sm"
                  loading={drafting}
                  disabled={submitting}
                  onPress={runDraft}
                >
                  {drafting ? "Drafting..." : "Try again"}
                </Button>
                <Button
                  size="sm"
                  loading={submitting}
                  disabled={drafting}
                  onPress={handleSaveDraft}
                >
                  Save card
                </Button>
              </>
            )}
          </View>
        </View>
      </View>
    </Modal>
  );
}

type MoveCardSheetProps = {
  card: BoardCard | null;
  onClose: () => void;
  onMove: (card: BoardCard, toColumn: BoardColumn) => void;
};

// MoveCardSheet is the native replacement for drag-and-drop. We use a
// shared Modal-based picker for both iOS and Android (rather than
// platform-specific ActionSheetIOS) so the styling matches the rest of
// the app and we have one code path to maintain.
function MoveCardSheet({ card, onClose, onMove }: MoveCardSheetProps) {
  const visible = card !== null;
  return (
    <Modal
      visible={visible}
      transparent
      animationType="slide"
      onRequestClose={onClose}
    >
      <Pressable
        className="flex-1 justify-end"
        style={{ backgroundColor: "rgba(6,7,10,0.7)" }}
        onPress={onClose}
      >
        <Pressable
          className="rounded-t-2xl border-t border-border bg-card p-4"
          onPress={(e) => e.stopPropagation()}
        >
          <View className="mb-3 flex-row items-center justify-between">
            <Text variant="h4" numberOfLines={1} className="flex-1 pr-3">
              {card?.title ?? "Move card"}
            </Text>
            <Pressable onPress={onClose} accessibilityLabel="Close">
              <X size={18} color="#9AA4B2" />
            </Pressable>
          </View>
          <View className="gap-2">
            {BOARD_COLUMNS.map((col) => {
              const isCurrent = card?.column === col;
              return (
                <Pressable
                  key={col}
                  disabled={isCurrent}
                  onPress={() => card && onMove(card, col)}
                  className="rounded-xl border border-border bg-secondary p-3 active:opacity-80"
                  style={isCurrent ? { opacity: 0.4 } : undefined}
                  accessibilityRole="button"
                  accessibilityLabel={`Move to ${BOARD_COLUMN_LABELS[col]}`}
                >
                  <Text variant="small" className="font-semibold">
                    {isCurrent
                      ? `${BOARD_COLUMN_LABELS[col]} (current)`
                      : `Move to ${BOARD_COLUMN_LABELS[col]}`}
                  </Text>
                </Pressable>
              );
            })}
            <Pressable
              onPress={onClose}
              className="mt-1 rounded-xl border border-border p-3 active:opacity-80"
              accessibilityRole="button"
              accessibilityLabel="Cancel"
            >
              <Text variant="small" className="text-center font-semibold">
                Cancel
              </Text>
            </Pressable>
          </View>
        </Pressable>
      </Pressable>
    </Modal>
  );
}

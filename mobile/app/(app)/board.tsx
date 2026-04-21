import { useCallback, useEffect, useMemo, useState } from "react";
import {
  ActivityIndicator,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  View,
} from "react-native";
import { useRouter } from "expo-router";
import { ExternalLink, Plus, X } from "lucide-react-native";

import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { useBoardEvents } from "@/hooks/useBoardEvents";
import { createCard, listCards, moveCard } from "@/services/board";
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
  const [cards, setCards] = useState<BoardCard[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showNewCard, setShowNewCard] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const next = await listCards();
      setCards(next);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load board");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

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
      router.push(`/board/${id}` as never);
    },
    [router],
  );

  const handleCreateCard = useCallback(
    async (title: string, description: string) => {
      const created = await createCard({ title, description });
      setCards((prev) => [...prev, created]);
    },
    [],
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

  return (
    <View className="flex-1 bg-background">
      <View className="flex-row items-center justify-between border-b border-border px-5 py-4">
        <View>
          <Text variant="h3">Board</Text>
          {error ? (
            <Text variant="small" className="text-destructive">
              {error}
            </Text>
          ) : null}
        </View>
        <Button
          size="sm"
          onPress={() => setShowNewCard(true)}
          icon={<Plus size={16} color="#06070A" />}
        >
          New card
        </Button>
      </View>

      {dnd ? (
        <dnd.DragDropContext onDragEnd={handleDragEnd}>
          <ColumnsRow grouped={grouped} onOpen={openCard} onNewCard={() => setShowNewCard(true)} dnd={dnd} />
        </dnd.DragDropContext>
      ) : (
        <ColumnsRow grouped={grouped} onOpen={openCard} onNewCard={() => setShowNewCard(true)} dnd={null} />
      )}

      <NewCardModal
        visible={showNewCard}
        onClose={() => setShowNewCard(false)}
        onCreate={handleCreateCard}
      />
    </View>
  );
}

type ColumnsRowProps = {
  grouped: Record<BoardColumn, BoardCard[]>;
  onOpen: (id: string) => void;
  onNewCard: () => void;
  dnd: DndModule | null;
};

function ColumnsRow({ grouped, onOpen, onNewCard, dnd }: ColumnsRowProps) {
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
  onNewCard?: () => void;
  dnd: DndModule | null;
};

function Column({ column, cards, onOpen, onNewCard, dnd }: ColumnProps) {
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
                      <BoardCardView card={card} onOpen={onOpen} />
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
          <BoardCardView key={card.id} card={card} onOpen={onOpen} />
        ))}
        {empty}
      </View>
    </View>
  );
}

function BoardCardView({ card, onOpen }: { card: BoardCard; onOpen: (id: string) => void }) {
  const status = STATUS_COLORS[card.agent_status] ?? STATUS_COLORS.idle;
  return (
    <Pressable
      onPress={() => onOpen(card.id)}
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
};

function NewCardModal({ visible, onClose, onCreate }: NewCardModalProps) {
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reset = () => {
    setTitle("");
    setDescription("");
    setError(null);
    setSubmitting(false);
  };

  const handleSubmit = async () => {
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

  return (
    <Modal
      visible={visible}
      transparent
      animationType="fade"
      onRequestClose={onClose}
    >
      <View
        className="flex-1 items-center justify-center p-4"
        style={{ backgroundColor: "rgba(6,7,10,0.7)" }}
      >
        <View
          className="w-full rounded-2xl border border-border bg-card p-5"
          style={{ maxWidth: 480 }}
        >
          <View className="mb-4 flex-row items-center justify-between">
            <Text variant="h4">New card</Text>
            <Pressable onPress={onClose} accessibilityLabel="Close">
              <X size={18} color="#9AA4B2" />
            </Pressable>
          </View>
          <View className="gap-3">
            <Input
              label="Title"
              value={title}
              onChangeText={setTitle}
              placeholder="What needs doing?"
              autoFocus
            />
            <Input
              label="Description"
              value={description}
              onChangeText={setDescription}
              placeholder="Optional details, acceptance criteria, links..."
              multiline
              numberOfLines={4}
              style={{ minHeight: 96, textAlignVertical: "top" }}
            />
            {error ? (
              <Text variant="small" className="text-destructive">
                {error}
              </Text>
            ) : null}
            <View className="mt-2 flex-row justify-end gap-2">
              <Button variant="outline" size="sm" onPress={onClose}>
                Cancel
              </Button>
              <Button size="sm" loading={submitting} onPress={handleSubmit}>
                Create
              </Button>
            </View>
          </View>
        </View>
      </View>
    </Modal>
  );
}

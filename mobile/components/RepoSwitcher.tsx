// RepoSwitcher — top-bar dropdown for switching between the user's
// connected GitHub repos. Shows the current repo's owner/name with a
// chevron; on press, opens a list of the rest. The footer routes the
// user to the full repos management screen.

import { useState } from "react";
import { Modal, Pressable, View } from "react-native";
import { ChevronDown, Plus } from "lucide-react-native";
import { router } from "expo-router";

import { Badge } from "@/components/ui/Badge";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { BoardRepo } from "@/services/boardTypes";

type Props = {
  repos: BoardRepo[];
  selected: BoardRepo | null;
  onSelect: (id: string) => void;
  isLoading?: boolean;
};

export function RepoSwitcher({ repos, selected, onSelect, isLoading }: Props) {
  const [open, setOpen] = useState(false);

  if (isLoading && !selected) {
    return (
      <View className="flex-row items-center rounded-lg border border-border bg-card px-3 py-2">
        <Text variant="small" className="text-muted">Loading repos…</Text>
      </View>
    );
  }

  if (!repos.length) {
    return (
      <Pressable
        className="flex-row items-center rounded-lg border border-border bg-card px-3 py-2"
        onPress={() => router.push("/repos")}
      >
        <Plus size={14} color="#9AA4B2" />
        <Text variant="small" className="ml-2 text-muted">
          Connect a repo
        </Text>
      </Pressable>
    );
  }

  return (
    <>
      <Pressable
        className="flex-row items-center rounded-lg border border-border bg-card px-3 py-2"
        onPress={() => setOpen(true)}
        accessibilityRole="button"
        accessibilityLabel={
          selected
            ? `Switch board, current repo ${selected.owner}/${selected.name}`
            : "Pick a board repo"
        }
        accessibilityHint="Opens a list of your connected GitHub repos to switch boards"
      >
        <View className="flex-1">
          <Text variant="small" className="font-semibold">
            {selected ? `${selected.owner}/${selected.name}` : "Pick a repo"}
          </Text>
        </View>
        <ChevronDown size={14} color="#9AA4B2" />
      </Pressable>

      <Modal
        visible={open}
        transparent
        animationType="fade"
        onRequestClose={() => setOpen(false)}
      >
        <Pressable className="flex-1 bg-black/40" onPress={() => setOpen(false)}>
          <View className="absolute left-4 top-16 w-72 rounded-xl border border-border bg-card p-2 shadow-2xl">
            <Text variant="small" className="px-2 pb-2 text-muted">
              Switch board
            </Text>
            {repos.map((repo) => {
              const isSelected = selected?.id === repo.id;
              return (
                <Pressable
                  key={repo.id}
                  className={cn(
                    "flex-row items-center gap-2 rounded-lg px-2 py-2",
                    isSelected ? "bg-primary/10" : "active:bg-card-foreground/5",
                  )}
                  onPress={() => {
                    onSelect(repo.id);
                    setOpen(false);
                  }}
                >
                  <View className="flex-1">
                    <Text variant="small" className={cn("font-medium", isSelected && "text-primary")}>
                      {repo.owner}/{repo.name}
                    </Text>
                  </View>
                  {repo.is_default ? (
                    <Badge variant="secondary">default</Badge>
                  ) : null}
                </Pressable>
              );
            })}
            <View className="mt-1 border-t border-border pt-2">
              <Pressable
                className="flex-row items-center gap-2 rounded-lg px-2 py-2 active:bg-card-foreground/5"
                onPress={() => {
                  setOpen(false);
                  router.push("/repos");
                }}
              >
                <Plus size={14} color="#00D4AA" />
                <Text variant="small" className="text-primary">
                  Manage repos
                </Text>
              </Pressable>
            </View>
          </View>
        </Pressable>
      </Modal>
    </>
  );
}

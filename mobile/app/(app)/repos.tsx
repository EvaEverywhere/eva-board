// Repos management screen — list, add, remove, and set-default for the
// user's connected GitHub repositories. Reachable from the board's
// repo switcher footer or the settings screen's "Manage repos" link.

import { useCallback, useEffect, useState } from "react";
import { ActivityIndicator, Modal, Pressable, ScrollView, View } from "react-native";
import { router } from "expo-router";
import { ChevronLeft, Plus, Star, Trash2 } from "lucide-react-native";

import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card, CardContent } from "@/components/ui/Card";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import {
  addBoardRepo,
  listBoardRepos,
  removeBoardRepo,
  setDefaultBoardRepo,
} from "@/services/board";
import type { BoardRepo } from "@/services/boardTypes";

export default function ReposScreen() {
  const [repos, setRepos] = useState<BoardRepo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showAdd, setShowAdd] = useState(false);

  const refresh = useCallback(async () => {
    try {
      setError(null);
      const list = await listBoardRepos();
      setRepos(list);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load repos");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const handleSetDefault = useCallback(
    async (id: string) => {
      try {
        await setDefaultBoardRepo(id);
        await refresh();
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to set default");
      }
    },
    [refresh],
  );

  const handleRemove = useCallback(
    async (id: string) => {
      try {
        await removeBoardRepo(id);
        await refresh();
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to remove repo");
      }
    },
    [refresh],
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
        <View className="flex-row items-center gap-2">
          <Pressable onPress={() => router.back()} className="rounded-lg p-1 active:bg-card">
            <ChevronLeft size={20} color="#9AA4B2" />
          </Pressable>
          <Text variant="h3">Repos</Text>
        </View>
        <Button
          size="sm"
          onPress={() => setShowAdd(true)}
          icon={<Plus size={16} color="#06070A" />}
        >
          Add repo
        </Button>
      </View>

      <ScrollView className="flex-1" contentContainerStyle={{ padding: 16, gap: 12, paddingBottom: 120 }}>
        {error ? (
          <Text variant="small" className="text-destructive">
            {error}
          </Text>
        ) : null}

        {repos.length === 0 ? (
          <Card>
            <CardContent>
              <View className="items-center gap-2 py-6">
                <Text variant="large">No repos connected yet</Text>
                <Text variant="muted" className="text-center">
                  Add a GitHub repository to start running the autonomous loop on its issues.
                </Text>
                <View className="h-2" />
                <Button onPress={() => setShowAdd(true)} icon={<Plus size={16} color="#06070A" />}>
                  Add your first repo
                </Button>
              </View>
            </CardContent>
          </Card>
        ) : (
          repos.map((repo) => (
            <Card key={repo.id}>
              <CardContent>
                <View className="flex-row items-start justify-between gap-3">
                  <View className="flex-1 gap-1">
                    <View className="flex-row items-center gap-2">
                      <Text variant="large" className="font-semibold">
                        {repo.owner}/{repo.name}
                      </Text>
                      {repo.is_default ? <Badge>default</Badge> : null}
                    </View>
                    <Text variant="small" className="text-muted">
                      Branch: {repo.default_branch} · Path: {repo.repo_path}
                    </Text>
                  </View>
                  <View className="flex-row items-center gap-2">
                    {!repo.is_default ? (
                      <Pressable
                        onPress={() => void handleSetDefault(repo.id)}
                        className="flex-row items-center gap-1 rounded-lg border border-border px-3 py-2 active:bg-card"
                      >
                        <Star size={14} color="#FBBF24" />
                        <Text variant="small">Set default</Text>
                      </Pressable>
                    ) : null}
                    <Pressable
                      onPress={() => void handleRemove(repo.id)}
                      className="flex-row items-center gap-1 rounded-lg border border-destructive/40 px-3 py-2 active:bg-destructive/10"
                    >
                      <Trash2 size={14} color="#FCA5A5" />
                      <Text variant="small" className="text-destructive">
                        Remove
                      </Text>
                    </Pressable>
                  </View>
                </View>
              </CardContent>
            </Card>
          ))
        )}
      </ScrollView>

      <AddRepoModal
        visible={showAdd}
        onClose={() => setShowAdd(false)}
        onAdded={async () => {
          setShowAdd(false);
          await refresh();
        }}
      />
    </View>
  );
}

type AddProps = {
  visible: boolean;
  onClose: () => void;
  onAdded: () => Promise<void>;
};

function AddRepoModal({ visible, onClose, onAdded }: AddProps) {
  const [owner, setOwner] = useState("");
  const [name, setName] = useState("");
  const [repoPath, setRepoPath] = useState("");
  const [defaultBranch, setDefaultBranch] = useState("");
  const [setAsDefault, setSetAsDefault] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reset = () => {
    setOwner("");
    setName("");
    setRepoPath("");
    setDefaultBranch("");
    setSetAsDefault(false);
    setError(null);
  };

  const submit = useCallback(async () => {
    setError(null);
    setSubmitting(true);
    try {
      await addBoardRepo({
        owner: owner.trim(),
        name: name.trim(),
        repo_path: repoPath.trim(),
        default_branch: defaultBranch.trim() || undefined,
        set_default: setAsDefault,
      });
      reset();
      await onAdded();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to add repo");
    } finally {
      setSubmitting(false);
    }
  }, [owner, name, repoPath, defaultBranch, setAsDefault, onAdded]);

  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onClose}>
      <Pressable className="flex-1 items-center justify-center bg-black/40 p-6" onPress={onClose}>
        <Pressable
          className="w-full max-w-md rounded-2xl border border-border bg-card p-5"
          onPress={(e) => e.stopPropagation()}
        >
          <Text variant="h4" className="mb-3">
            Add a repository
          </Text>

          <View className="gap-3">
            <View className="gap-1">
              <Text variant="small" className="text-muted">Owner</Text>
              <Input value={owner} onChangeText={setOwner} placeholder="EvaEverywhere" />
            </View>
            <View className="gap-1">
              <Text variant="small" className="text-muted">Name</Text>
              <Input value={name} onChangeText={setName} placeholder="eva-board" />
            </View>
            <View className="gap-1">
              <Text variant="small" className="text-muted">Local repo path</Text>
              <Input
                value={repoPath}
                onChangeText={setRepoPath}
                placeholder="/Users/you/eva-board"
                autoCapitalize="none"
              />
            </View>
            <View className="gap-1">
              <Text variant="small" className="text-muted">
                Default branch (optional)
              </Text>
              <Input value={defaultBranch} onChangeText={setDefaultBranch} placeholder="main" />
            </View>
            <Pressable
              className="flex-row items-center gap-2"
              onPress={() => setSetAsDefault((v) => !v)}
            >
              <View
                className={`h-5 w-5 items-center justify-center rounded border ${
                  setAsDefault ? "bg-primary border-primary" : "border-border"
                }`}
              >
                {setAsDefault ? <Text className="text-xs">✓</Text> : null}
              </View>
              <Text variant="small">Set as default board</Text>
            </Pressable>
          </View>

          {error ? (
            <Text variant="small" className="mt-3 text-destructive">
              {error}
            </Text>
          ) : null}

          <View className="mt-5 flex-row justify-end gap-2">
            <Button variant="outline" onPress={onClose} disabled={submitting}>
              Cancel
            </Button>
            <Button onPress={() => void submit()} disabled={submitting || !owner || !name || !repoPath}>
              {submitting ? "Adding…" : "Add repo"}
            </Button>
          </View>
        </Pressable>
      </Pressable>
    </Modal>
  );
}

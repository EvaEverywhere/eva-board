import { useCallback, useEffect, useState } from "react";
import { Alert, Linking, Pressable, ScrollView, View } from "react-native";

import { Avatar } from "@/components/ui/Avatar";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/Card";
import { Input } from "@/components/ui/Input";
import { Separator } from "@/components/ui/Separator";
import { Text } from "@/components/ui/Text";
import { useBoardSettings } from "@/hooks/useBoardSettings";
import { listRepos, updateBoardSettings } from "@/services/board";
import type { Repo } from "@/services/boardTypes";
import { useAuthSession } from "@/providers/AuthSessionProvider";

const REPO_PATH_ROOT = "~/eva-board-repos";

const AGENT_OPTIONS: Array<{ value: string; label: string; description: string }> = [
  {
    value: "claude_code",
    label: "Claude Code",
    description: "Use the Claude Code CLI for code generation.",
  },
  {
    value: "generic_cli",
    label: "Generic CLI",
    description: "Use whatever command is set via CODEGEN_COMMAND on the server.",
  },
];

function defaultRepoPath(owner: string, repo: string): string {
  return `${REPO_PATH_ROOT}/${owner}-${repo}`;
}

export default function SettingsScreen() {
  const { user, logout } = useAuthSession();
  const { settings, isLoading, error, refresh } = useBoardSettings();

  const handleLogout = async () => {
    try {
      await logout();
    } catch (err) {
      const message = err instanceof Error ? err.message : "Could not log out";
      Alert.alert("Logout failed", message);
    }
  };

  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerStyle={{ padding: 20, paddingBottom: 120 }}
    >
      <View className="gap-4">
        <ProfileCard user={user} />

        <GitHubCard
          hasToken={Boolean(settings?.has_github_token)}
          owner={settings?.github_owner ?? ""}
          repo={settings?.github_repo ?? ""}
          repoPath={settings?.repo_path ?? ""}
          isLoadingSettings={isLoading}
          settingsError={error}
          onChanged={refresh}
        />

        <AgentCard
          codegenAgent={settings?.codegen_agent ?? "claude_code"}
          maxVerifyIterations={settings?.max_verify_iterations ?? 3}
          maxReviewCycles={settings?.max_review_cycles ?? 5}
          isLoadingSettings={isLoading}
          onChanged={refresh}
        />

        <AboutCard />

        <Button variant="destructive" onPress={handleLogout}>
          Sign out
        </Button>
      </View>
    </ScrollView>
  );
}

// ---------- Profile ----------

function ProfileCard({ user }: { user: { name?: string | null; email?: string | null } | null }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Profile</CardTitle>
      </CardHeader>
      <CardContent className="gap-4">
        <View className="flex-row items-center gap-3">
          <Avatar fallback={user?.name ?? "U"} />
          <View className="gap-1">
            <Text variant="large">{user?.name ?? "Unknown user"}</Text>
            <Text variant="small" className="text-muted">
              {user?.email ?? "No email"}
            </Text>
          </View>
        </View>
      </CardContent>
    </Card>
  );
}

// ---------- GitHub ----------

type GitHubCardProps = {
  hasToken: boolean;
  owner: string;
  repo: string;
  repoPath: string;
  isLoadingSettings: boolean;
  settingsError: string | null;
  onChanged: () => Promise<void>;
};

function GitHubCard({
  hasToken,
  owner,
  repo,
  repoPath,
  isLoadingSettings,
  settingsError,
  onChanged,
}: GitHubCardProps) {
  const [token, setToken] = useState("");
  const [showToken, setShowToken] = useState(false);
  const [saving, setSaving] = useState(false);
  const [verifyError, setVerifyError] = useState<string | null>(null);

  const [repos, setRepos] = useState<Repo[] | null>(null);
  const [reposLoading, setReposLoading] = useState(false);
  const [reposError, setReposError] = useState<string | null>(null);
  const [verifiedLogin, setVerifiedLogin] = useState<string | null>(null);

  const [savingRepo, setSavingRepo] = useState(false);
  const [disconnecting, setDisconnecting] = useState(false);

  const loadRepos = useCallback(async () => {
    setReposLoading(true);
    setReposError(null);
    try {
      const list = await listRepos();
      setRepos(list);
      if (list.length > 0) {
        setVerifiedLogin(list[0].owner);
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : "Failed to load repos";
      setReposError(message);
    } finally {
      setReposLoading(false);
    }
  }, []);

  useEffect(() => {
    if (hasToken && repos === null && !reposLoading) {
      void loadRepos();
    }
  }, [hasToken, repos, reposLoading, loadRepos]);

  const handleVerifyAndSave = async () => {
    const trimmed = token.trim();
    if (!trimmed) {
      setVerifyError("Enter a personal access token");
      return;
    }
    setSaving(true);
    setVerifyError(null);
    try {
      await updateBoardSettings({ github_token: trimmed });
      setToken("");
      setShowToken(false);
      await onChanged();
      // Server-side validation hits GitHub /user; if we got here it's valid.
      // Pull repos so the user can pick one immediately.
      await loadRepos();
    } catch (err) {
      const message = err instanceof Error ? err.message : "Failed to save token";
      setVerifyError(message);
    } finally {
      setSaving(false);
    }
  };

  const handlePickRepo = async (picked: Repo) => {
    setSavingRepo(true);
    try {
      await updateBoardSettings({
        github_owner: picked.owner,
        github_repo: picked.name,
        repo_path: defaultRepoPath(picked.owner, picked.name),
      });
      await onChanged();
    } catch (err) {
      const message = err instanceof Error ? err.message : "Failed to save repo";
      Alert.alert("Save failed", message);
    } finally {
      setSavingRepo(false);
    }
  };

  const handleDisconnect = () => {
    Alert.alert(
      "Disconnect GitHub?",
      "This will clear your saved personal access token. Your selected repo will stay until you change it.",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Disconnect",
          style: "destructive",
          onPress: async () => {
            setDisconnecting(true);
            try {
              await updateBoardSettings({ github_token: "" });
              setRepos(null);
              setVerifiedLogin(null);
              await onChanged();
            } catch (err) {
              const message = err instanceof Error ? err.message : "Failed to disconnect";
              Alert.alert("Disconnect failed", message);
            } finally {
              setDisconnecting(false);
            }
          },
        },
      ],
    );
  };

  return (
    <Card>
      <CardHeader>
        <View className="flex-row items-center justify-between">
          <CardTitle>GitHub Connection</CardTitle>
          {hasToken ? (
            <Badge variant="default">Connected</Badge>
          ) : (
            <Badge variant="outline">Not connected</Badge>
          )}
        </View>
      </CardHeader>
      <CardContent className="gap-4">
        {settingsError ? (
          <Text variant="small" className="text-destructive">
            {settingsError}
          </Text>
        ) : null}

        {!hasToken ? (
          <View className="gap-3">
            <Text variant="small" className="text-muted">
              Paste a GitHub personal access token with `repo` scope. Eva uses it to read your
              repos, push branches, and open pull requests.
            </Text>
            <Input
              label="Personal Access Token"
              value={token}
              onChangeText={setToken}
              autoCapitalize="none"
              autoCorrect={false}
              secureTextEntry={!showToken}
              placeholder="ghp_..."
              error={verifyError ?? undefined}
            />
            <View className="flex-row items-center justify-between">
              <Pressable onPress={() => setShowToken((v) => !v)}>
                <Text variant="small" className="text-primary">
                  {showToken ? "Hide token" : "Show token"}
                </Text>
              </Pressable>
              <Button onPress={handleVerifyAndSave} loading={saving} disabled={!token.trim()}>
                Verify & save
              </Button>
            </View>
          </View>
        ) : (
          <View className="gap-4">
            <View className="gap-1">
              <Text variant="small" className="text-muted">Status</Text>
              <Text>
                {verifiedLogin
                  ? `Connected as @${verifiedLogin}`
                  : "Token saved. Verifying..."}
              </Text>
            </View>

            <Separator />

            <View className="gap-2">
              <Text variant="small" className="text-muted">Repository</Text>
              {owner && repo ? (
                <View className="gap-1">
                  <Text>
                    {owner}/{repo}
                  </Text>
                  <Text variant="small" className="text-muted">
                    Worktree path: {repoPath || defaultRepoPath(owner, repo)}
                  </Text>
                </View>
              ) : (
                <Text variant="small" className="text-muted">No repo selected yet.</Text>
              )}
            </View>

            <View className="gap-2">
              <View className="flex-row items-center justify-between">
                <Text variant="small" className="text-muted">
                  {owner && repo ? "Switch repository" : "Pick a repository"}
                </Text>
                <Pressable onPress={loadRepos} disabled={reposLoading}>
                  <Text variant="small" className="text-primary">
                    {reposLoading ? "Loading..." : "Refresh"}
                  </Text>
                </Pressable>
              </View>
              {reposError ? (
                <Text variant="small" className="text-destructive">{reposError}</Text>
              ) : null}
              {repos && repos.length === 0 ? (
                <Text variant="small" className="text-muted">
                  No repositories visible to this token.
                </Text>
              ) : null}
              <View className="gap-1">
                {(repos ?? []).map((r) => {
                  const selected = r.owner === owner && r.name === repo;
                  return (
                    <Pressable
                      key={`${r.owner}/${r.name}`}
                      onPress={() => {
                        if (!selected && !savingRepo) void handlePickRepo(r);
                      }}
                      className={
                        "flex-row items-center justify-between rounded-lg border px-3 py-2 " +
                        (selected ? "border-primary bg-primary/10" : "border-border bg-card")
                      }
                    >
                      <View className="flex-1 pr-2">
                        <Text>
                          {r.owner}/{r.name}
                        </Text>
                        <Text variant="small" className="text-muted">
                          {r.private ? "private" : "public"} · default {r.default_branch}
                        </Text>
                      </View>
                      {selected ? <Badge variant="default">Selected</Badge> : null}
                    </Pressable>
                  );
                })}
              </View>
            </View>

            <Separator />

            <Button
              variant="outline"
              onPress={handleDisconnect}
              loading={disconnecting}
            >
              Disconnect GitHub
            </Button>
          </View>
        )}

        {isLoadingSettings ? (
          <Text variant="small" className="text-muted">Loading settings...</Text>
        ) : null}
      </CardContent>
    </Card>
  );
}

// ---------- Agent ----------

type AgentCardProps = {
  codegenAgent: string;
  maxVerifyIterations: number;
  maxReviewCycles: number;
  isLoadingSettings: boolean;
  onChanged: () => Promise<void>;
};

function AgentCard({
  codegenAgent,
  maxVerifyIterations,
  maxReviewCycles,
  isLoadingSettings,
  onChanged,
}: AgentCardProps) {
  const [agent, setAgent] = useState(codegenAgent);
  const [verifyIters, setVerifyIters] = useState(maxVerifyIterations);
  const [reviewCycles, setReviewCycles] = useState(maxReviewCycles);
  const [saving, setSaving] = useState(false);
  const [savedMsg, setSavedMsg] = useState<string | null>(null);

  useEffect(() => setAgent(codegenAgent), [codegenAgent]);
  useEffect(() => setVerifyIters(maxVerifyIterations), [maxVerifyIterations]);
  useEffect(() => setReviewCycles(maxReviewCycles), [maxReviewCycles]);

  const dirty =
    agent !== codegenAgent ||
    verifyIters !== maxVerifyIterations ||
    reviewCycles !== maxReviewCycles;

  const handleSave = async () => {
    setSaving(true);
    setSavedMsg(null);
    try {
      await updateBoardSettings({
        codegen_agent: agent,
        max_verify_iterations: verifyIters,
        max_review_cycles: reviewCycles,
      });
      await onChanged();
      setSavedMsg("Saved");
    } catch (err) {
      const message = err instanceof Error ? err.message : "Failed to save";
      Alert.alert("Save failed", message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>Agent Configuration</CardTitle>
      </CardHeader>
      <CardContent className="gap-4">
        <View className="gap-2">
          <Text variant="small" className="text-muted">Agent type</Text>
          <View className="gap-1">
            {AGENT_OPTIONS.map((opt) => {
              const selected = opt.value === agent;
              return (
                <Pressable
                  key={opt.value}
                  onPress={() => setAgent(opt.value)}
                  className={
                    "rounded-lg border px-3 py-2 " +
                    (selected ? "border-primary bg-primary/10" : "border-border bg-card")
                  }
                >
                  <View className="flex-row items-center justify-between">
                    <Text>{opt.label}</Text>
                    {selected ? <Badge variant="default">Selected</Badge> : null}
                  </View>
                  <Text variant="small" className="text-muted">
                    {opt.description}
                  </Text>
                </Pressable>
              );
            })}
          </View>
          {agent === "generic_cli" ? (
            <Text variant="small" className="text-muted">
              Set the `CODEGEN_COMMAND` environment variable on the server to point at your
              CLI. (Per-user custom commands aren't wired up yet.)
            </Text>
          ) : null}
        </View>

        <Separator />

        <Stepper
          label="Max verify iterations"
          value={verifyIters}
          min={1}
          max={10}
          onChange={setVerifyIters}
          help="How many times the agent will retry after a failed verification."
        />

        <Stepper
          label="Max review cycles"
          value={reviewCycles}
          min={1}
          max={10}
          onChange={setReviewCycles}
          help="How many code-review rounds before escalating for input."
        />

        <View className="flex-row items-center justify-between">
          {savedMsg ? (
            <Text variant="small" className="text-muted">{savedMsg}</Text>
          ) : (
            <View />
          )}
          <Button onPress={handleSave} loading={saving} disabled={!dirty || isLoadingSettings}>
            Save
          </Button>
        </View>
      </CardContent>
    </Card>
  );
}

function Stepper({
  label,
  value,
  min,
  max,
  onChange,
  help,
}: {
  label: string;
  value: number;
  min: number;
  max: number;
  onChange: (v: number) => void;
  help?: string;
}) {
  const dec = () => onChange(Math.max(min, value - 1));
  const inc = () => onChange(Math.min(max, value + 1));
  return (
    <View className="gap-1">
      <View className="flex-row items-center justify-between">
        <Text variant="small" className="text-muted">{label}</Text>
        <View className="flex-row items-center gap-2">
          <Button variant="outline" size="sm" onPress={dec} disabled={value <= min}>
            −
          </Button>
          <View className="min-w-10 items-center">
            <Text variant="large">{value}</Text>
          </View>
          <Button variant="outline" size="sm" onPress={inc} disabled={value >= max}>
            +
          </Button>
        </View>
      </View>
      {help ? (
        <Text variant="small" className="text-muted">{help}</Text>
      ) : null}
    </View>
  );
}

// ---------- About ----------

const REPO_URL = "https://github.com/teslashibe/eva-board";

function AboutCard() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>About</CardTitle>
      </CardHeader>
      <CardContent className="gap-2">
        <Text>Eva Board v0.1.0</Text>
        <Pressable onPress={() => void Linking.openURL(REPO_URL)}>
          <Text variant="small" className="text-primary">{REPO_URL}</Text>
        </Pressable>
      </CardContent>
    </Card>
  );
}

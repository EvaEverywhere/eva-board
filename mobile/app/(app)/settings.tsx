import { useCallback, useEffect, useState } from "react";
import { Alert, Linking, Pressable, ScrollView, View } from "react-native";
import { router } from "expo-router";

import { Avatar } from "@/components/ui/Avatar";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/Card";
import { Input } from "@/components/ui/Input";
import { Separator } from "@/components/ui/Separator";
import { Text } from "@/components/ui/Text";
import { useBoardRepos } from "@/hooks/useBoardRepos";
import { useBoardSettings } from "@/hooks/useBoardSettings";
import { listRepos, updateBoardSettings } from "@/services/board";
import type { BoardSettings } from "@/services/boardTypes";
import { useAuthSession } from "@/providers/AuthSessionProvider";

// AgentPreset describes a one-click coding-agent configuration the user
// can pick in Settings. `agent` and `command` map directly to the Go
// codegen.NewAgent switch ("claude-code" uses the Claude Code CLI; any
// other value uses the generic CLI wrapper with `command` + `args`).
// `id === "custom"` is the escape hatch that exposes free-form command
// + args inputs.
type AgentPreset = {
  id: string;
  label: string;
  description: string;
  agent: "claude-code" | "generic";
  command: string | null;
  args: string[];
};

const AGENT_PRESETS: AgentPreset[] = [
  {
    id: "claude-code",
    label: "Claude Code",
    description: "Anthropic's Claude Code CLI. Recommended.",
    agent: "claude-code",
    command: null,
    args: [],
  },
  {
    id: "codex",
    label: "Codex CLI",
    description: "OpenAI's Codex CLI.",
    agent: "generic",
    command: "codex",
    args: [],
  },
  {
    id: "aider",
    label: "Aider",
    description: "AI pair programming in your terminal.",
    agent: "generic",
    command: "aider",
    args: ["--yes", "--no-stream"],
  },
  {
    id: "openhands",
    label: "OpenHands",
    description: "Open-source AI software engineer.",
    agent: "generic",
    command: "openhands",
    args: [],
  },
  {
    id: "cline",
    label: "Cline",
    description: "Autonomous coding agent (CLI mode).",
    agent: "generic",
    command: "cline",
    args: [],
  },
  {
    id: "custom",
    label: "Custom...",
    description:
      "Use any CLI command. Reads prompt from stdin and modifies files in the working directory.",
    agent: "generic",
    command: "",
    args: [],
  },
];

const CUSTOM_PRESET_ID = "custom";

function arraysEqual(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i += 1) {
    if (a[i] !== b[i]) return false;
  }
  return true;
}

// detectPreset matches the saved settings against each preset and falls
// back to "custom" so editing fields are exposed for any combination
// the named presets don't cover.
function detectPreset(settings: {
  codegen_agent: string;
  codegen_command: string | null;
  codegen_args: string[];
}): string {
  for (const preset of AGENT_PRESETS) {
    if (preset.id === CUSTOM_PRESET_ID) continue;
    const presetCmd = preset.command ?? null;
    const savedCmd = settings.codegen_command ?? null;
    if (
      preset.agent === settings.codegen_agent &&
      presetCmd === savedCmd &&
      arraysEqual(preset.args, settings.codegen_args ?? [])
    ) {
      return preset.id;
    }
  }
  return CUSTOM_PRESET_ID;
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
          isLoadingSettings={isLoading}
          settingsError={error}
          onChanged={refresh}
        />

        <ReposSummaryCard hasToken={Boolean(settings?.has_github_token)} />

        <AgentCard
          settings={settings}
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
  isLoadingSettings: boolean;
  settingsError: string | null;
  onChanged: () => Promise<void>;
};

function GitHubCard({
  hasToken,
  isLoadingSettings,
  settingsError,
  onChanged,
}: GitHubCardProps) {
  const [token, setToken] = useState("");
  const [showToken, setShowToken] = useState(false);
  const [saving, setSaving] = useState(false);
  const [verifyError, setVerifyError] = useState<string | null>(null);

  const [verifiedLogin, setVerifiedLogin] = useState<string | null>(null);
  const [verifiedFetching, setVerifiedFetching] = useState(false);
  const [disconnecting, setDisconnecting] = useState(false);

  // Resolve the GitHub username from the token by listing one repo and
  // reading its owner. Cheap and avoids adding a /user endpoint.
  const fetchVerifiedLogin = useCallback(async () => {
    setVerifiedFetching(true);
    try {
      const list = await listRepos();
      if (list.length > 0) setVerifiedLogin(list[0].owner);
    } catch {
      // Best-effort; the connection itself was already validated on save.
    } finally {
      setVerifiedFetching(false);
    }
  }, []);

  useEffect(() => {
    if (hasToken && verifiedLogin === null && !verifiedFetching) {
      void fetchVerifiedLogin();
    }
  }, [hasToken, verifiedLogin, verifiedFetching, fetchVerifiedLogin]);

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
      // Resolve the username to display the "Connected as @…" line.
      await fetchVerifiedLogin();
    } catch (err) {
      const message = err instanceof Error ? err.message : "Failed to save token";
      setVerifyError(message);
    } finally {
      setSaving(false);
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

            <View className="gap-1">
              <Text variant="small" className="text-muted">Repositories</Text>
              <Text variant="small">
                Connect and switch between repos in the Repos screen.
              </Text>
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
  settings: BoardSettings | null;
  isLoadingSettings: boolean;
  onChanged: () => Promise<void>;
};

function argsToCSV(args: string[]): string {
  return args.join(", ");
}

function csvToArgs(csv: string): string[] {
  return csv
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}

function AgentCard({ settings, isLoadingSettings, onChanged }: AgentCardProps) {
  const savedAgent = settings?.codegen_agent ?? "claude-code";
  const savedCommand = settings?.codegen_command ?? null;
  const savedArgs = settings?.codegen_args ?? [];
  const savedVerifyIters = settings?.max_verify_iterations ?? 3;
  const savedReviewCycles = settings?.max_review_cycles ?? 5;

  const [presetId, setPresetId] = useState<string>(() =>
    detectPreset({
      codegen_agent: savedAgent,
      codegen_command: savedCommand,
      codegen_args: savedArgs,
    }),
  );
  const [customCommand, setCustomCommand] = useState<string>(savedCommand ?? "");
  const [customArgsCSV, setCustomArgsCSV] = useState<string>(argsToCSV(savedArgs));
  const [verifyIters, setVerifyIters] = useState(savedVerifyIters);
  const [reviewCycles, setReviewCycles] = useState(savedReviewCycles);
  const [saving, setSaving] = useState(false);
  const [savedMsg, setSavedMsg] = useState<string | null>(null);

  // Re-sync local state whenever the parent reloads settings.
  useEffect(() => {
    setPresetId(
      detectPreset({
        codegen_agent: savedAgent,
        codegen_command: savedCommand,
        codegen_args: savedArgs,
      }),
    );
    setCustomCommand(savedCommand ?? "");
    setCustomArgsCSV(argsToCSV(savedArgs));
  }, [savedAgent, savedCommand, savedArgs]);
  useEffect(() => setVerifyIters(savedVerifyIters), [savedVerifyIters]);
  useEffect(() => setReviewCycles(savedReviewCycles), [savedReviewCycles]);

  // Resolve what we'd send on save based on the current form state.
  const resolved = (() => {
    const preset = AGENT_PRESETS.find((p) => p.id === presetId) ?? AGENT_PRESETS[0];
    if (preset.id === CUSTOM_PRESET_ID) {
      return {
        agent: preset.agent,
        command: customCommand.trim(),
        args: csvToArgs(customArgsCSV),
      };
    }
    return {
      agent: preset.agent,
      command: preset.command ?? "",
      args: preset.args,
    };
  })();

  const dirty =
    resolved.agent !== savedAgent ||
    resolved.command !== (savedCommand ?? "") ||
    !arraysEqual(resolved.args, savedArgs) ||
    verifyIters !== savedVerifyIters ||
    reviewCycles !== savedReviewCycles;

  const customInvalid =
    presetId === CUSTOM_PRESET_ID && resolved.command.length === 0;

  const handleSave = async () => {
    setSaving(true);
    setSavedMsg(null);
    try {
      await updateBoardSettings({
        codegen_agent: resolved.agent,
        codegen_command: resolved.command === "" ? null : resolved.command,
        codegen_args: resolved.args,
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
          <Text variant="small" className="text-muted">Coding agent</Text>
          <View className="gap-1">
            {AGENT_PRESETS.map((preset) => {
              const selected = preset.id === presetId;
              return (
                <Pressable
                  key={preset.id}
                  onPress={() => setPresetId(preset.id)}
                  className={
                    "rounded-lg border px-3 py-2 " +
                    (selected ? "border-primary bg-primary/10" : "border-border bg-card")
                  }
                >
                  <View className="flex-row items-center justify-between">
                    <Text>{preset.label}</Text>
                    {selected ? <Badge variant="default">Selected</Badge> : null}
                  </View>
                  <Text variant="small" className="text-muted">
                    {preset.description}
                  </Text>
                </Pressable>
              );
            })}
          </View>

          {presetId === CUSTOM_PRESET_ID ? (
            <View className="gap-2 pt-2">
              <Input
                label="Custom command"
                value={customCommand}
                onChangeText={setCustomCommand}
                autoCapitalize="none"
                autoCorrect={false}
                placeholder="/usr/local/bin/my-agent"
                error={customInvalid ? "Command is required" : undefined}
              />
              <Input
                label="Args (comma-separated)"
                value={customArgsCSV}
                onChangeText={setCustomArgsCSV}
                autoCapitalize="none"
                autoCorrect={false}
                placeholder="--yes, --model, gpt-4o"
              />
              <Text variant="small" className="text-muted">
                The command runs in the worktree directory and reads the prompt from stdin.
              </Text>
            </View>
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
          <Button
            onPress={handleSave}
            loading={saving}
            disabled={!dirty || isLoadingSettings || customInvalid}
          >
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

// ---------- Repos summary ----------

function ReposSummaryCard({ hasToken }: { hasToken: boolean }) {
  const { repos, isLoading } = useBoardRepos();
  const defaultRepo = repos.find((r) => r.is_default) ?? repos[0] ?? null;

  return (
    <Card>
      <CardHeader>
        <CardTitle>Repositories</CardTitle>
      </CardHeader>
      <CardContent className="gap-3">
        {!hasToken ? (
          <Text variant="small" className="text-muted">
            Connect GitHub above before adding repositories.
          </Text>
        ) : isLoading ? (
          <Text variant="small" className="text-muted">
            Loading…
          </Text>
        ) : repos.length === 0 ? (
          <Text variant="small" className="text-muted">
            No repositories connected yet.
          </Text>
        ) : (
          <View className="gap-1">
            <Text variant="small">
              {repos.length} {repos.length === 1 ? "repo" : "repos"} connected
            </Text>
            {defaultRepo ? (
              <Text variant="small" className="text-muted">
                Default: {defaultRepo.owner}/{defaultRepo.name}
              </Text>
            ) : null}
          </View>
        )}
        <Button variant="outline" onPress={() => router.push("/repos" as never)}>
          Manage repos →
        </Button>
      </CardContent>
    </Card>
  );
}

// ---------- About ----------

const REPO_URL = "https://github.com/EvaEverywhere/eva-board";

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

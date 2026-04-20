import { Alert, ScrollView, View } from "react-native";

import { Avatar } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/Card";
import { Separator } from "@/components/ui/Separator";
import { Text } from "@/components/ui/Text";
import { useAuthSession } from "@/providers/AuthSessionProvider";

export default function SettingsScreen() {
  const { user, logout } = useAuthSession();

  const handleLogout = async () => {
    try {
      await logout();
    } catch (error) {
      const message = error instanceof Error ? error.message : "Could not log out";
      Alert.alert("Logout failed", message);
    }
  };

  return (
    <ScrollView className="flex-1 bg-background" contentContainerStyle={{ padding: 20, paddingBottom: 120 }}>
      <View className="gap-4">
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
            <Separator />
            <Text variant="small" className="text-muted">
              This is a minimal template settings screen. Extend it with notifications, integrations, and profile settings.
            </Text>
          </CardContent>
        </Card>

        <Button variant="destructive" onPress={handleLogout}>
          Sign out
        </Button>
      </View>
    </ScrollView>
  );
}

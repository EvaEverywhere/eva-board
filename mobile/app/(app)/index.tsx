import { Sparkles } from "lucide-react-native";
import { ScrollView, View } from "react-native";

import { Badge } from "@/components/ui/Badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/Card";
import { EmptyState } from "@/components/ui/EmptyState";
import { Text } from "@/components/ui/Text";
import { useAuthSession } from "@/providers/AuthSessionProvider";

export default function HomeScreen() {
  const { user } = useAuthSession();

  return (
    <ScrollView className="flex-1 bg-background" contentContainerStyle={{ padding: 20, paddingBottom: 120 }}>
      <View className="gap-5">
        <Card>
          <CardHeader>
            <Badge className="self-start">Template</Badge>
            <CardTitle>Welcome {user?.name || "there"}</CardTitle>
            <CardDescription>You're signed in and ready to build your product.</CardDescription>
          </CardHeader>
          <CardContent>
            <Text variant="small" className="text-muted">
              Signed in as {user?.email ?? "unknown"}
            </Text>
          </CardContent>
        </Card>

        <EmptyState
          icon={<Sparkles color="#00D4AA" size={24} />}
          title="Start building"
          description="Add your first domain package in backend/internal and your first feature screen in mobile/app/(app)."
        />
      </View>
    </ScrollView>
  );
}

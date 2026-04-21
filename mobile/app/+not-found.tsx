import { Link } from "expo-router";
import { View } from "react-native";

import { Text } from "@/components/ui/Text";

export default function NotFoundScreen() {
  return (
    <View className="flex-1 items-center justify-center gap-3 bg-background px-6">
      <Text variant="h2">Not found</Text>
      <Text variant="small" className="text-center text-muted">
        This route does not exist in Eva Board.
      </Text>
      <Link href="/" asChild>
        <Text variant="small" className="text-primary underline">
          Go home
        </Text>
      </Link>
    </View>
  );
}

import { ActivityIndicator, View } from "react-native";
import { Redirect } from "expo-router";

import { useAuthSession } from "@/providers/AuthSessionProvider";

export default function IndexScreen() {
  const { isLoading, isAuthenticated } = useAuthSession();

  if (isLoading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator color="#00D4AA" />
      </View>
    );
  }

  return <Redirect href={isAuthenticated ? "/(app)" : "/(auth)/welcome"} />;
}

import { Redirect, Tabs } from "expo-router";

import { FloatingTabBar } from "@/components/FloatingTabBar";
import { useAuthSession } from "@/providers/AuthSessionProvider";

export default function AppLayout() {
  const { isLoading, isAuthenticated } = useAuthSession();

  if (!isLoading && !isAuthenticated) {
    return <Redirect href="/(auth)/welcome" />;
  }

  return (
    <Tabs tabBar={(props) => <FloatingTabBar {...props} />} screenOptions={{ headerShown: false, tabBarStyle: { display: "none" } }}>
      <Tabs.Screen name="index" options={{ href: null }} />
      <Tabs.Screen name="board" options={{ title: "Board" }} />
      <Tabs.Screen name="board-card/[id]" options={{ href: null }} />
      <Tabs.Screen name="repos" options={{ href: null }} />
      <Tabs.Screen name="settings" options={{ title: "Settings" }} />
    </Tabs>
  );
}

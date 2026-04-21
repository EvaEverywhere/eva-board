import { Redirect, Tabs } from "expo-router";
import { Platform } from "react-native";

import { FloatingTabBar } from "@/components/FloatingTabBar";
import { useAuthSession } from "@/providers/AuthSessionProvider";

export default function AppLayout() {
  const { isLoading, isAuthenticated } = useAuthSession();

  if (!isLoading && !isAuthenticated) {
    return <Redirect href="/(auth)/welcome" />;
  }

  // Layout shape differs per platform:
  //   - Web: the custom FloatingTabBar paints itself as a fixed 240px
  //     sidebar on the left (CSS position: fixed). We hide the default
  //     tab-bar layout and push the scene content right via sceneStyle
  //     marginLeft so screens flow into the remaining viewport.
  //   - Native: the custom FloatingTabBar is an absolutely positioned
  //     floating bottom bar overlaid on content. Hiding the default tab
  //     bar layout stops bottom-tabs from reserving space for it.
  const screenOptions = Platform.select({
    web: {
      headerShown: false,
      tabBarStyle: { display: "none" as const },
      sceneStyle: { marginLeft: 240 }
    },
    default: {
      headerShown: false,
      tabBarStyle: { display: "none" as const }
    }
  });

  return (
    <Tabs tabBar={(props) => <FloatingTabBar {...props} />} screenOptions={screenOptions}>
      <Tabs.Screen name="index" options={{ href: null }} />
      <Tabs.Screen name="board" options={{ title: "Board" }} />
      <Tabs.Screen name="board-card/[id]" options={{ href: null }} />
      <Tabs.Screen name="repos" options={{ href: null }} />
      <Tabs.Screen name="settings" options={{ title: "Settings" }} />
    </Tabs>
  );
}

import { BottomTabBarProps } from "@react-navigation/bottom-tabs";
import { Columns3, Settings } from "lucide-react-native";
import * as Haptics from "expo-haptics";
import { Platform, Pressable, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

import { Text } from "@/components/ui/Text";

const iconMap: Record<string, typeof Columns3> = {
  board: Columns3,
  settings: Settings
};

const labelMap: Record<string, string> = {
  board: "Board",
  settings: "Settings"
};

/**
 * FloatingTabBar renders navigation differently per platform:
 *
 *   - On native (iOS, Android) it is a floating bottom bar, absolutely
 *     positioned over the content, matching the feel of a native mobile
 *     app.
 *   - On web it is a left sidebar (vertical list) — the layout in
 *     (app)/_layout.tsx pushes scene content right by 240px via
 *     sceneStyle.marginLeft so nothing renders underneath the sidebar.
 *     A bottom bar on a wide browser window feels wrong; a sidebar
 *     reads like a proper desktop app.
 *
 * Both branches share the route-to-icon/label maps so adding a new tab
 * is a one-line change here.
 */
export function FloatingTabBar(props: BottomTabBarProps) {
  if (Platform.OS === "web") {
    return <WebSidebar {...props} />;
  }
  return <NativeBottomBar {...props} />;
}

// ---------- Native ---------------------------------------------------------

function NativeBottomBar({ state, descriptors, navigation }: BottomTabBarProps) {
  const insets = useSafeAreaInsets();

  return (
    <View
      className="absolute left-4 right-4 rounded-2xl border border-border bg-card/95 px-2 py-2"
      style={{ bottom: Math.max(insets.bottom, 12) }}
    >
      <View className="flex-row items-center justify-around">
        {state.routes.map((route, index) => {
          const descriptor = descriptors[route.key];
          const options = descriptor.options;

          if (options.tabBarButton === null) {
            return null;
          }
          if (!iconMap[route.name]) {
            return null;
          }

          const isFocused = state.index === index;
          const Icon = iconMap[route.name] ?? Columns3;
          const label = labelMap[route.name] ?? route.name;

          return (
            <Pressable
              key={route.key}
              className="items-center justify-center rounded-xl px-4 py-2"
              onPress={() => {
                const event = navigation.emit({
                  type: "tabPress",
                  target: route.key,
                  canPreventDefault: true
                });
                if (!isFocused && !event.defaultPrevented) {
                  void Haptics.selectionAsync().catch(() => undefined);
                  navigation.navigate(route.name, route.params);
                }
              }}
            >
              <Icon size={18} color={isFocused ? "#00D4AA" : "#9AA4B2"} />
              <Text variant="small" className={isFocused ? "text-primary mt-1" : "text-muted mt-1"}>
                {label}
              </Text>
            </Pressable>
          );
        })}
      </View>
    </View>
  );
}

// ---------- Web ------------------------------------------------------------

function WebSidebar({ state, descriptors, navigation }: BottomTabBarProps) {
  // `position: "fixed"` pins the sidebar to the left of the viewport so
  // it stays visible while the main content scrolls independently. The
  // layout in (app)/_layout.tsx pushes the scene 240px to the right via
  // sceneStyle.marginLeft so nothing renders underneath.
  return (
    <View
      className="border-r border-border bg-background px-3 py-5"
      // `position: "fixed"` is web-only; RN types reject it. Cast keeps
      // the native typecheck happy while react-native-web forwards it.
      style={{ position: "fixed" as unknown as "absolute", top: 0, left: 0, bottom: 0, width: 240 }}
    >
      <View className="mb-6 px-2">
        <Text variant="large" className="font-semibold">
          Eva Board
        </Text>
      </View>

      <View className="gap-1">
        {state.routes.map((route, index) => {
          const descriptor = descriptors[route.key];
          const options = descriptor.options;

          if (options.tabBarButton === null) {
            return null;
          }
          if (!iconMap[route.name]) {
            return null;
          }

          const isFocused = state.index === index;
          const Icon = iconMap[route.name] ?? Columns3;
          const label = labelMap[route.name] ?? route.name;

          return (
            <Pressable
              key={route.key}
              className={
                "flex-row items-center gap-3 rounded-lg px-3 py-2 " +
                (isFocused ? "bg-primary/10" : "active:bg-card-foreground/5")
              }
              onPress={() => {
                const event = navigation.emit({
                  type: "tabPress",
                  target: route.key,
                  canPreventDefault: true
                });
                if (!isFocused && !event.defaultPrevented) {
                  navigation.navigate(route.name, route.params);
                }
              }}
            >
              <Icon size={18} color={isFocused ? "#00D4AA" : "#9AA4B2"} />
              <Text
                variant="small"
                className={isFocused ? "text-primary font-medium" : "text-muted"}
              >
                {label}
              </Text>
            </Pressable>
          );
        })}
      </View>
    </View>
  );
}

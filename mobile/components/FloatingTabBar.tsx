import { BottomTabBarProps } from "@react-navigation/bottom-tabs";
import { Columns3, Settings } from "lucide-react-native";
import * as Haptics from "expo-haptics";
import { Pressable, View } from "react-native";
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

export function FloatingTabBar({ state, descriptors, navigation }: BottomTabBarProps) {
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
          // Tabs.Screen with `href: null` (e.g. our redirect index) is hidden
          // from the tab bar via the iconMap lookup below — only known route
          // names render an icon.
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

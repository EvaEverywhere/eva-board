import { Image, View } from "react-native";

import { cn } from "@/lib/utils";
import { Text } from "@/components/ui/Text";

type Size = "sm" | "md" | "lg";

const sizeClasses: Record<Size, string> = {
  sm: "h-8 w-8",
  md: "h-12 w-12",
  lg: "h-16 w-16"
};

export function Avatar({
  uri,
  fallback,
  size = "md"
}: {
  uri?: string | null;
  fallback: string;
  size?: Size;
}) {
  return (
    <View className={cn("items-center justify-center rounded-full bg-secondary border border-border", sizeClasses[size])}>
      {uri ? (
        <Image source={{ uri }} className={cn("rounded-full", sizeClasses[size])} resizeMode="cover" />
      ) : (
        <Text variant="small" className="font-semibold">
          {fallback.slice(0, 2).toUpperCase()}
        </Text>
      )}
    </View>
  );
}

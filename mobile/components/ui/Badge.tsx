import { View } from "react-native";

import { cn } from "@/lib/utils";
import { Text } from "@/components/ui/Text";

type Variant = "default" | "secondary" | "destructive" | "outline";

const badgeClasses: Record<Variant, string> = {
  default: "bg-primary/15 border border-primary/30",
  secondary: "bg-secondary border border-border",
  destructive: "bg-destructive/15 border border-destructive/30",
  outline: "bg-transparent border border-border"
};

export function Badge({
  children,
  variant = "default",
  className
}: {
  children: React.ReactNode;
  variant?: Variant;
  className?: string;
}) {
  return (
    <View className={cn("rounded-full px-2 py-1", badgeClasses[variant], className)}>
      <Text variant="small">{children}</Text>
    </View>
  );
}

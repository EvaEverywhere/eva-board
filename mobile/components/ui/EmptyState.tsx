import { ReactNode } from "react";
import { View } from "react-native";

import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";

type Props = {
  icon?: ReactNode;
  title: string;
  description: string;
  actionLabel?: string;
  onAction?: () => void;
};

export function EmptyState({ icon, title, description, actionLabel, onAction }: Props) {
  return (
    <View className="w-full items-center justify-center gap-3 rounded-2xl border border-border bg-card p-6">
      {icon}
      <Text variant="h4">{title}</Text>
      <Text variant="small" className="text-center text-muted">
        {description}
      </Text>
      {actionLabel && onAction ? (
        <Button size="sm" onPress={onAction}>
          {actionLabel}
        </Button>
      ) : null}
    </View>
  );
}

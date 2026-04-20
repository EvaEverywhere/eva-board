import { ReactNode } from "react";
import { View, ViewProps } from "react-native";

import { cn } from "@/lib/utils";
import { Text } from "@/components/ui/Text";

type CardProps = ViewProps & {
  className?: string;
};

export function Card({ className, ...props }: CardProps) {
  return <View className={cn("rounded-2xl border border-border bg-card p-4", className)} {...props} />;
}

export function CardHeader({ className, ...props }: CardProps) {
  return <View className={cn("mb-3 gap-1", className)} {...props} />;
}

export function CardTitle({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <Text variant="h4" className={cn(className)}>
      {children}
    </Text>
  );
}

export function CardDescription({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <Text variant="small" className={cn("text-muted", className)}>
      {children}
    </Text>
  );
}

export function CardContent({ className, ...props }: CardProps) {
  return <View className={cn("gap-3", className)} {...props} />;
}

export function CardFooter({ className, ...props }: CardProps) {
  return <View className={cn("mt-4", className)} {...props} />;
}

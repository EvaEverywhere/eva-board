import { ReactNode } from "react";
import { ActivityIndicator, Pressable, PressableProps } from "react-native";
import * as Haptics from "expo-haptics";

import { cn } from "@/lib/utils";
import { Text } from "@/components/ui/Text";

type Variant = "default" | "destructive" | "outline" | "secondary" | "ghost" | "link";
type Size = "default" | "sm" | "lg" | "icon";

type Props = Omit<PressableProps, "children"> & {
  variant?: Variant;
  size?: Size;
  loading?: boolean;
  className?: string;
  textClassName?: string;
  icon?: ReactNode;
  children?: ReactNode;
};

const variantClasses: Record<Variant, string> = {
  default: "bg-primary",
  destructive: "bg-destructive",
  outline: "border border-border bg-transparent",
  secondary: "bg-secondary",
  ghost: "bg-transparent",
  link: "bg-transparent"
};

const sizeClasses: Record<Size, string> = {
  default: "h-11 px-4 py-2",
  sm: "h-9 px-3",
  lg: "h-12 px-6",
  icon: "h-10 w-10"
};

const textVariantClasses: Record<Variant, string> = {
  default: "text-background",
  destructive: "text-background",
  outline: "text-foreground",
  secondary: "text-foreground",
  ghost: "text-foreground",
  link: "text-primary underline"
};

export function Button({
  variant = "default",
  size = "default",
  loading = false,
  className,
  textClassName,
  icon,
  disabled,
  onPress,
  children,
  ...props
}: Props) {
  const isDisabled = disabled || loading;

  return (
    <Pressable
      className={cn(
        "items-center justify-center rounded-lg flex-row gap-2 active:opacity-80",
        variantClasses[variant],
        sizeClasses[size],
        isDisabled ? "opacity-50" : "",
        className
      )}
      disabled={isDisabled}
      onPress={(event) => {
        void Haptics.selectionAsync().catch(() => undefined);
        onPress?.(event);
      }}
      {...props}
    >
      {loading ? <ActivityIndicator size="small" color="#06070A" /> : icon}
      {!loading && typeof children === "string" ? (
        <Text variant="small" className={cn("font-semibold", textVariantClasses[variant], textClassName)}>
          {children}
        </Text>
      ) : !loading ? (
        children
      ) : null}
    </Pressable>
  );
}

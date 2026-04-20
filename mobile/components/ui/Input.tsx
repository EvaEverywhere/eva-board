import { useState } from "react";
import { TextInput, TextInputProps, View } from "react-native";

import { cn } from "@/lib/utils";
import { Text } from "@/components/ui/Text";

type Props = TextInputProps & {
  label?: string;
  error?: string;
  className?: string;
};

export function Input({ label, error, className, ...props }: Props) {
  const [focused, setFocused] = useState(false);

  return (
    <View className="w-full gap-2">
      {label ? <Text variant="small">{label}</Text> : null}
      <TextInput
        className={cn(
          "w-full rounded-lg border bg-card px-3 py-3 text-foreground",
          focused ? "border-primary" : "border-border",
          error ? "border-destructive" : "",
          className
        )}
        placeholderTextColor="#9AA4B2"
        onFocus={(event) => {
          setFocused(true);
          props.onFocus?.(event);
        }}
        onBlur={(event) => {
          setFocused(false);
          props.onBlur?.(event);
        }}
        {...props}
      />
      {error ? (
        <Text variant="small" className="text-destructive">
          {error}
        </Text>
      ) : null}
    </View>
  );
}

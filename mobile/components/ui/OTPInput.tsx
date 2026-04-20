import { useRef, useState } from "react";
import { Platform, Pressable, TextInput, View } from "react-native";

import { cn } from "@/lib/utils";
import { Text } from "@/components/ui/Text";

type Props = {
  length?: number;
  value: string;
  onChange: (value: string) => void;
  onComplete?: (value: string) => void;
  label?: string;
  error?: string;
};

export function OTPInput({ length = 6, value, onChange, onComplete, label, error }: Props) {
  const inputRef = useRef<TextInput>(null);
  const [focused, setFocused] = useState(false);

  const digits = value.split("").concat(Array(length).fill("")).slice(0, length);

  const handleChange = (text: string) => {
    const cleaned = text.replace(/\D/g, "").slice(0, length);
    onChange(cleaned);
    if (cleaned.length === length) {
      onComplete?.(cleaned);
    }
  };

  const handlePress = () => {
    inputRef.current?.focus();
  };

  return (
    <View className="w-full gap-2">
      {label ? <Text variant="small">{label}</Text> : null}
      <Pressable onPress={handlePress}>
        <View className="flex-row justify-between gap-2">
          {digits.map((digit, index) => {
            const isActive = focused && index === Math.min(value.length, length - 1);
            return (
              <View
                key={index}
                className={cn(
                  "flex-1 items-center justify-center rounded-lg border bg-card",
                  isActive ? "border-primary" : "border-border",
                  error ? "border-destructive" : "",
                  Platform.OS === "web" ? "aspect-square max-h-14" : ""
                )}
                style={Platform.OS !== "web" ? { height: 56 } : undefined}
              >
                <Text
                  className={cn(
                    "text-center text-xl",
                    digit ? "text-foreground" : "text-muted"
                  )}
                >
                  {digit || (isActive ? "│" : "·")}
                </Text>
              </View>
            );
          })}
        </View>
      </Pressable>
      <TextInput
        ref={inputRef}
        value={value}
        onChangeText={handleChange}
        keyboardType="number-pad"
        maxLength={length}
        autoComplete="one-time-code"
        textContentType="oneTimeCode"
        returnKeyType="go"
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        style={{
          position: "absolute",
          opacity: 0,
          height: 1,
          width: 1,
        }}
        caretHidden
      />
      {error ? (
        <Text variant="small" className="text-destructive">
          {error}
        </Text>
      ) : null}
    </View>
  );
}

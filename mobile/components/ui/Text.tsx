import { Text as RNText, TextProps } from "react-native";

import { cn } from "@/lib/utils";

type Variant = "h1" | "h2" | "h3" | "h4" | "p" | "lead" | "large" | "small" | "muted";

type Props = TextProps & {
  variant?: Variant;
  className?: string;
};

const variantClasses: Record<Variant, string> = {
  h1: "text-3xl font-bold text-foreground",
  h2: "text-2xl font-bold text-foreground",
  h3: "text-xl font-semibold text-foreground",
  h4: "text-lg font-semibold text-foreground",
  p: "text-base text-foreground",
  lead: "text-lg text-foreground/90",
  large: "text-lg font-semibold text-foreground",
  small: "text-sm text-foreground/90",
  muted: "text-sm text-muted"
};

export function Text({ variant = "p", className, ...props }: Props) {
  return <RNText className={cn(variantClasses[variant], className)} {...props} />;
}

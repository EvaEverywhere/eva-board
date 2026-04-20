import { View } from "react-native";

import { cn } from "@/lib/utils";

export function Separator({
  orientation = "horizontal",
  className
}: {
  orientation?: "horizontal" | "vertical";
  className?: string;
}) {
  return (
    <View
      className={cn(
        "bg-border",
        orientation === "horizontal" ? "h-px w-full" : "h-full w-px",
        className
      )}
    />
  );
}

import type { ReactNode } from "react";
import { ScrollViewStyleReset } from "expo-router/html";

export default function Root({ children }: { children: ReactNode }) {
  return (
    <html lang="en" className="dark">
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <meta name="theme-color" content="#06070A" />
      </head>
      <body style={{ backgroundColor: "#06070A" }}>
        <ScrollViewStyleReset />
        {children}
      </body>
    </html>
  );
}

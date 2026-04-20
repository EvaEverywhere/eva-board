import { Platform } from "react-native";

export const API_URL =
  process.env.EXPO_PUBLIC_API_URL ??
  Platform.select({
    ios: "http://localhost:8080",
    android: "http://10.0.2.2:8080",
    default: "http://localhost:8080"
  })!;

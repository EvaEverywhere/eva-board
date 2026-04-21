import { Platform } from "react-native";
import * as SecureStore from "expo-secure-store";

import { API_URL as DEFAULT_API_URL } from "@/config";

const STORAGE_KEY = "eva_board_server_url";

// undefined = not yet hydrated from storage; null = hydrated, no override.
let cachedOverride: string | null | undefined;

async function readStoredOverride(): Promise<string | null> {
  if (Platform.OS === "web") {
    return globalThis.localStorage?.getItem(STORAGE_KEY) ?? null;
  }
  return SecureStore.getItemAsync(STORAGE_KEY);
}

function resolve(override: string | null | undefined): string {
  const trimmed = typeof override === "string" ? override.trim() : "";
  return trimmed || DEFAULT_API_URL;
}

export async function getServerUrl(): Promise<string> {
  if (cachedOverride === undefined) {
    cachedOverride = await readStoredOverride();
  }
  return resolve(cachedOverride);
}

export function getServerUrlSync(): string {
  return resolve(cachedOverride);
}

export async function setServerUrl(url: string | null): Promise<void> {
  const trimmed = url?.trim() || null;
  cachedOverride = trimmed;
  if (Platform.OS === "web") {
    if (trimmed) {
      globalThis.localStorage?.setItem(STORAGE_KEY, trimmed);
    } else {
      globalThis.localStorage?.removeItem(STORAGE_KEY);
    }
    return;
  }
  if (trimmed) {
    await SecureStore.setItemAsync(STORAGE_KEY, trimmed);
    return;
  }
  await SecureStore.deleteItemAsync(STORAGE_KEY);
}

export function getDefaultServerUrl(): string {
  return DEFAULT_API_URL;
}

export async function hydrateServerUrl(): Promise<void> {
  await getServerUrl();
}

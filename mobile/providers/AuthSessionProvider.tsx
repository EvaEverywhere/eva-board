import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { Platform } from "react-native";
import * as Linking from "expo-linking";
import * as SecureStore from "expo-secure-store";

import { getMe, sendMagicLink, type User, verifyCode } from "@/services/auth";
import { setAccessTokenProvider } from "@/services/api";

type AuthSessionContextValue = {
  isLoading: boolean;
  isAuthenticated: boolean;
  user: User | null;
  login: (email: string) => Promise<void>;
  verifyCode: (email: string, code: string) => Promise<void>;
  logout: () => Promise<void>;
  getAccessToken: () => Promise<string | null>;
};

const TOKEN_KEY = "template_app_token";

const AuthSessionContext = createContext<AuthSessionContextValue | null>(null);

function decodeTokenPayload(token: string): { exp?: number } | null {
  const parts = token.split(".");
  if (parts.length < 2) {
    return null;
  }

  const base64 = parts[1].replace(/-/g, "+").replace(/_/g, "/");
  const padded = base64.padEnd(Math.ceil(base64.length / 4) * 4, "=");
  try {
		if (typeof globalThis.atob !== "function") {
			return null;
		}
		const decoded = globalThis.atob(padded);
    return JSON.parse(decoded) as { exp?: number };
  } catch {
    return null;
  }
}

function isTokenExpired(token: string): boolean {
  const payload = decodeTokenPayload(token);
  if (!payload?.exp) {
    return false;
  }
  return payload.exp * 1000 <= Date.now();
}

async function readStoredToken() {
  if (Platform.OS === "web") {
    return globalThis.localStorage?.getItem(TOKEN_KEY) ?? null;
  }
  return SecureStore.getItemAsync(TOKEN_KEY);
}

async function writeStoredToken(token: string | null) {
  if (Platform.OS === "web") {
    if (token) {
      globalThis.localStorage?.setItem(TOKEN_KEY, token);
    } else {
      globalThis.localStorage?.removeItem(TOKEN_KEY);
    }
    return;
  }

  if (token) {
    await SecureStore.setItemAsync(TOKEN_KEY, token);
    return;
  }
  await SecureStore.deleteItemAsync(TOKEN_KEY);
}

export function AuthSessionProvider({ children }: { children: React.ReactNode }) {
  const [isLoading, setIsLoading] = useState(true);
  const [isAuthenticated, setIsAuthenticated] = useState(false);
  const [user, setUser] = useState<User | null>(null);
  const tokenRef = useRef<string | null>(null);

  const clearSession = useCallback(async () => {
    tokenRef.current = null;
    setIsAuthenticated(false);
    setUser(null);
    await writeStoredToken(null);
  }, []);

  const applyToken = useCallback(
    async (token: string) => {
      if (!token || isTokenExpired(token)) {
        await clearSession();
        return;
      }

      tokenRef.current = token;
      await writeStoredToken(token);
      setIsAuthenticated(true);

      const profile = await getMe();
      setUser(profile);
    },
    [clearSession]
  );

  useEffect(() => {
    const hydrate = async () => {
      try {
        const token = await readStoredToken();
        if (!token || isTokenExpired(token)) {
          await clearSession();
          return;
        }
        tokenRef.current = token;
        setIsAuthenticated(true);
        const profile = await getMe();
        setUser(profile);
      } catch {
        await clearSession();
      } finally {
        setIsLoading(false);
      }
    };

    void hydrate();
  }, [clearSession]);

  const getAccessToken = useCallback(async () => {
    if (!tokenRef.current) {
      return null;
    }
    if (isTokenExpired(tokenRef.current)) {
      await clearSession();
      return null;
    }
    return tokenRef.current;
  }, [clearSession]);

  useEffect(() => {
    setAccessTokenProvider(getAccessToken);
    return () => setAccessTokenProvider(null);
  }, [getAccessToken]);

  useEffect(() => {
    const onURL = async (url: string) => {
      const parsed = Linking.parse(url);
      const token = typeof parsed.queryParams?.token === "string" ? parsed.queryParams.token : null;
      if (token) {
        await applyToken(token);
      }
    };

    const init = async () => {
      const initialURL = await Linking.getInitialURL();
      if (initialURL) {
        await onURL(initialURL);
      }
    };
    void init();

    const subscription = Linking.addEventListener("url", ({ url }) => {
      void onURL(url);
    });

    return () => subscription.remove();
  }, [applyToken]);

  const login = useCallback(async (email: string) => {
    await sendMagicLink(email);
  }, []);

  const verifyLoginCode = useCallback(
    async (email: string, code: string) => {
      const response = await verifyCode(email, code);
      await applyToken(response.token);
    },
    [applyToken]
  );

  const logout = useCallback(async () => {
    await clearSession();
  }, [clearSession]);

  const value = useMemo<AuthSessionContextValue>(
    () => ({
      isLoading,
      isAuthenticated,
      user,
      login,
      verifyCode: verifyLoginCode,
      logout,
      getAccessToken
    }),
    [getAccessToken, isAuthenticated, isLoading, login, logout, user, verifyLoginCode]
  );

  return <AuthSessionContext.Provider value={value}>{children}</AuthSessionContext.Provider>;
}

export function useAuthSession() {
  const context = useContext(AuthSessionContext);
  if (!context) {
    throw new Error("useAuthSession must be used within AuthSessionProvider");
  }
  return context;
}

import { getServerUrl } from "@/services/serverUrl";

type AccessTokenProvider = () => Promise<string | null>;

type RequestOptions = RequestInit & {
  auth?: boolean;
};

let accessTokenProvider: AccessTokenProvider | null = null;

export class APIError extends Error {
  status: number;
  body: string;

  constructor(status: number, body: string) {
    super(body || `Request failed with status ${status}`);
    this.name = "APIError";
    this.status = status;
    this.body = body;
  }
}

export class AuthenticationError extends APIError {
  constructor(status: number, body: string) {
    super(status, body);
    this.name = "AuthenticationError";
  }
}

export function isAuthenticationError(error: unknown): error is AuthenticationError {
  return error instanceof AuthenticationError;
}

export function setAccessTokenProvider(provider: AccessTokenProvider | null) {
  accessTokenProvider = provider;
}

export async function getAccessToken(): Promise<string | null> {
  if (!accessTokenProvider) {
    return null;
  }
  return accessTokenProvider();
}

export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { auth = true, headers, ...rest } = options;
  const resolvedHeaders: Record<string, string> = {
    "Content-Type": "application/json",
    ...(headers as Record<string, string> | undefined)
  };

  if (auth) {
    // Wait briefly for the AuthSessionProvider to register an access token
    // provider. On a deep-link/direct-navigate the provider mounts in a
    // useEffect and may not have registered yet by the time the first
    // screen issues a fetch.
    let waited = 0;
    while (!accessTokenProvider && waited < 1500) {
      await new Promise((resolve) => setTimeout(resolve, 50));
      waited += 50;
    }
    if (!accessTokenProvider) {
      throw new AuthenticationError(401, "Authentication provider is not configured");
    }
    const token = await accessTokenProvider();
    if (!token) {
      throw new AuthenticationError(401, "Missing access token");
    }
    resolvedHeaders.Authorization = `Bearer ${token}`;
  }

  const baseUrl = await getServerUrl();
  const response = await fetch(`${baseUrl.replace(/\/+$/, "")}${path}`, {
    ...rest,
    headers: resolvedHeaders
  });

  if (!response.ok) {
    const body = await response.text();
    if (response.status === 401 || response.status === 403) {
      throw new AuthenticationError(response.status, body);
    }
    throw new APIError(response.status, body);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  const contentType = response.headers.get("content-type") ?? "";
  if (!contentType.includes("application/json")) {
    return (await response.text()) as T;
  }
  return (await response.json()) as T;
}

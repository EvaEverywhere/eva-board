import { request } from "@/services/api";

export type AuthResponse = {
  token: string;
  user_id: string;
  email: string;
  name: string;
};

export type User = {
  id: string;
  identity_key: string;
  email: string;
  name: string;
  created_at: string;
  updated_at: string;
};

export async function sendMagicLink(email: string) {
  return request<{ status: string }>("/auth/magic-link", {
    method: "POST",
    auth: false,
    body: JSON.stringify({ email })
  });
}

export async function verifyCode(email: string, code: string) {
  return request<AuthResponse>("/auth/verify", {
    method: "POST",
    auth: false,
    body: JSON.stringify({ email, code })
  });
}

export async function getMe() {
  return request<User>("/api/me");
}

export async function getDevToken(email: string, name = "Developer") {
  return request<AuthResponse>("/auth/login", {
    method: "POST",
    auth: false,
    body: JSON.stringify({ email, name })
  });
}

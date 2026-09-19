export type LoginCredentialError = "username_required" | "password_required";
export type ServerURLValidationError = "protocol_required" | "invalid_url";

export function validateLoginCredentials(
  username: string,
  password: string
): LoginCredentialError | null {
  if (!username.trim()) return "username_required";
  if (!password) return "password_required";
  return null;
}

export function validateServerURL(value: string): ServerURLValidationError | null {
  const trimmed = value.trim();
  if (!/^https?:\/\//i.test(trimmed)) return "protocol_required";
  try {
    const parsed = new URL(trimmed);
    if ((parsed.protocol !== "http:" && parsed.protocol !== "https:") || !parsed.hostname) {
      return "invalid_url";
    }
  } catch {
    return "invalid_url";
  }
  return null;
}

export function shouldInitializeAuthorizedSession(
  deviceStatus: "pending" | "approved" | "revoked" | undefined
): boolean {
  return deviceStatus !== "pending";
}

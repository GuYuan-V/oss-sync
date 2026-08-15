export type LoginCredentialError = "username_required" | "password_required";

export function validateLoginCredentials(
  username: string,
  password: string
): LoginCredentialError | null {
  if (!username.trim()) return "username_required";
  if (!password) return "password_required";
  return null;
}

export function shouldInitializeAuthorizedSession(
  deviceStatus: "pending" | "approved" | "revoked" | undefined
): boolean {
  return deviceStatus !== "pending";
}

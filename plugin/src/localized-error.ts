import type { TranslationKey } from "./i18n";

type Translator = (key: TranslationKey) => string;

const NETWORK_ERROR_KEYS: ReadonlyArray<readonly [string, TranslationKey]> = [
  ["ERR_CONNECTION_REFUSED", "network.connectionRefused"],
  ["ERR_CONNECTION_TIMED_OUT", "network.connectionTimedOut"],
  ["ERR_NAME_NOT_RESOLVED", "network.nameNotResolved"],
  ["ERR_INTERNET_DISCONNECTED", "network.disconnected"],
  ["ERR_NETWORK_CHANGED", "network.disconnected"],
];

const AUTH_ERROR_KEYS: ReadonlyArray<readonly [string, TranslationKey]> = [
  ["jwt token expired", "auth.tokenExpired"],
  ["invalid credentials", "auth.invalidCredentials"],
  ["unauthorized", "auth.invalidCredentials"],
];

const AUTH_CODE_KEYS: ReadonlyArray<readonly [string, TranslationKey]> = [
  ["device_pending", "auth.devicePending"],
  ["device_revoked", "auth.deviceRevoked"],
  ["device_not_authorized", "auth.deviceUnauthorized"],
  ["device_unknown", "auth.deviceUnknown"],
];

export function localizeError(error: unknown, t: Translator, fallback: string): string {
  const code = typeof error === "object" && error !== null && "code" in error && typeof error.code === "string" ? error.code : "";
  for (const [candidate, key] of AUTH_CODE_KEYS) {
    if (code === candidate) return t(key);
  }
  const message = error instanceof Error ? error.message : "";
  for (const [code, key] of NETWORK_ERROR_KEYS) {
    if (message.includes(code)) return t(key);
  }
  const normalized = message.toLowerCase();
  for (const [fragment, key] of AUTH_ERROR_KEYS) {
    if (normalized.includes(fragment)) return t(key);
  }
  return message || fallback;
}

export type DeviceLoginRecoveryResult<T> = {
  readonly response: T;
  readonly replacedRevokedIdentity: boolean;
};

function isDeviceRevokedError(error: unknown): boolean {
  return typeof error === "object" && error !== null && "code" in error && error.code === "device_revoked";
}

export function createClientID(): string {
  if (typeof globalThis.crypto?.randomUUID === "function") {
    return globalThis.crypto.randomUUID();
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

/** 仅在设备确实被吊销时替换本地身份并重试登录一次 */
export async function loginWithRevokedDeviceRecovery<T>(
  login: () => Promise<T>,
  replaceIdentity: () => Promise<void>
): Promise<DeviceLoginRecoveryResult<T>> {
  try {
    return { response: await login(), replacedRevokedIdentity: false };
  } catch (error: unknown) {
    if (!isDeviceRevokedError(error)) throw error;
    await replaceIdentity();
    return { response: await login(), replacedRevokedIdentity: true };
  }
}

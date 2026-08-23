// 插件设置及默认值。
import type { LanguagePreference } from "./i18n";

export interface OSSSettings {
  /** 服务端 URL（不含尾斜杠），例如 http://localhost:8080 */
  serverUrl: string;
  username: string;
  password: string;
  /** 本地变更触发同步前的防抖时间。 */
  syncIntervalSec: number;
  /** 同时执行的上传和下载任务数。 */
  maxConcurrency: number;
  /** 是否同步 .obsidian 下容易产生设备冲突的本地状态文件。 */
  syncPoisonObsidianFiles: boolean;
  /** 是否优先使用增量同步。 */
  incrementalCheck: boolean;
  /** 博客分享时是否保留目录结构。 */
  keepDirectoryTree: boolean;
  /** 当前本地 Obsidian Vault 绑定的服务端 Vault UUID。 */
  vaultId: string;
  /** 当前绑定的服务端 Vault 名称，仅用于 UI 展示。 */
  vaultName: string;
  /** 当前设备稳定 ID，不随登录或插件重启变化。 */
  clientId: string;
  /** 当前设备在服务端设备列表中的显示名称。 */
  deviceName: string;
  /** 没有本地变更时轮询远端 revision 的间隔。 */
  remotePollIntervalSec: number;
  /** 协作事件仅使用 SSE，连接失败时不回退长轮询。 */
  forceSSE: boolean;
  /** 是否将安全的传输事件写入开发者控制台。 */
  diagnosticsEnabled: boolean;
  /** 用户选择的同步模式偏好，仅在服务端策略为 user_choice 时生效。 */
  vaultSyncMode: "short_poll" | "long_poll";
  /** 插件界面语言；auto 跟随 Obsidian。 */
  language: LanguagePreference;
  /** 当前登录用户在服务端上的角色；admin 才能执行插件在线更新。 */
  role: string;
  /** GitHub 仓库（owner/repo），用于检查与下载 Release。 */
  updateRepo: string;
  /** 未解决冲突文件的编辑前是否持续警告，默认开启 */
  conflictEditWarning: boolean;
}

export const DEFAULT_SETTINGS: OSSSettings = {
  serverUrl: "http://localhost:8080",
  username: "",
  password: "",
  syncIntervalSec: 3,
  maxConcurrency: 6,
  syncPoisonObsidianFiles: false,
  incrementalCheck: true,
  keepDirectoryTree: true,
  vaultId: "",
  vaultName: "",
  clientId: "",
  deviceName: "",
  remotePollIntervalSec: 30,
  forceSSE: false,
  diagnosticsEnabled: false,
  vaultSyncMode: "short_poll",
  language: "auto",
  role: "",
  updateRepo: "helantianshen/oss-sync",
  conflictEditWarning: true,
};

// 协作管理模块：维护 协作oss/ 目录与远程协作文件的同步、事件传输状态与邀请弹窗。
//
// 协作文件只通过协作 API 同步：列表 / 响应邀请 / 上传正文；
// 远端内容变化经 SSE 或长轮询发现后，用协作内容接口刷新本地副本。
// 本地编辑防抖 2 秒后尝试上传。

import { App, Modal, Notice, Setting, TFile } from "obsidian";
import type OSSPlugin from "./main";
import { localizeError } from "./localized-error";
import type { CollabEntry, OSSApiClient } from "./api";
import { normalizePath } from "./baseline";
import type { Diagnostics } from "./diagnostics";

export const COLLAB_DIR = "协作oss";

/** 计算协作文件在本地 Vault 中的路径。 */
export function collabLocalPath(owner: string, filePath: string): string {
  return `${COLLAB_DIR}/${owner}/${filePath}`;
}

/** 判断路径是否位于协作目录下。 */
export function isCollabPath(path: string): boolean {
  const p = normalizePath(path);
  return p === COLLAB_DIR || p.startsWith(COLLAB_DIR + "/");
}

const COLLAB_POLL_WAIT_SEC = 30;
const COLLAB_INBOX_REFRESH_MS = 30_000;
const UPLOAD_DEBOUNCE_MS = 2000;
const SUPPRESS_MS = 5000;

export class CollabManager {
  private collabs: CollabEntry[] = [];
  private started = false;
  private pollGen = 0;
  private pollController: AbortController | null = null;
  private after = 0;
  private readonly suppressed = new Set<string>();
  private readonly uploadTimers = new Map<string, number>();
  private eventSource: EventSource | null = null;
  private streamOpenedAt: number | null = null;
  private inboxTimer: number | null = null;
  private transport: "disconnected" | "sse" | "sse_failed" | "sse_unavailable" | "long_poll" =
    "disconnected";

  constructor(
    private readonly app: App,
    private readonly api: OSSApiClient,
    private readonly plugin: OSSPlugin,
    private readonly onChange: () => void,
    private readonly diagnostics?: Diagnostics
  ) {}

  /** 启动协作事件传输，优先使用安全 SSE，失败时回退长轮询。 */
  start(): void {
    if (this.started) return;
    this.started = true;
    this.after = 0;
    void this.refresh();
    this.inboxTimer = window.setInterval(() => void this.refresh(), COLLAB_INBOX_REFRESH_MS);
    const streamURL = this.api.collabAccountEventStreamURL();
    if (streamURL) {
      this.startSSE(streamURL);
      return;
    }
    if (this.plugin.settings.forceSSE) {
      this.transport = "sse_unavailable";
      this.onChange();
      return;
    }
    this.startLongPoll();
  }

  /** 停止协作传输。 */
  stop(): void {
    this.started = false;
    this.pollGen++;
    if (this.pollController) {
      this.pollController.abort();
      this.pollController = null;
    }
    if (this.eventSource) this.diagnostics?.record({ kind: "sse_state", at: Date.now(), state: "closed" });
    this.eventSource?.close();
    this.eventSource = null;
    this.streamOpenedAt = null;
    if (this.inboxTimer !== null) {
      window.clearInterval(this.inboxTimer);
      this.inboxTimer = null;
    }
    this.transport = "disconnected";
    for (const timer of this.uploadTimers.values()) window.clearTimeout(timer);
    this.uploadTimers.clear();
    this.suppressed.clear();
  }

  isRunning(): boolean {
    return this.started;
  }

  /** 侧边栏展示的传输状态文案。 */
  getTransportStatus(): string {
    if (this.transport === "sse") return this.plugin.t("sidebar.collabSSE");
    if (this.transport === "sse_failed") return this.plugin.t("sidebar.collabSSEFailed");
    if (this.transport === "sse_unavailable") return this.plugin.t("sidebar.collabSSEUnavailable");
    if (this.transport === "long_poll") return this.plugin.t("sidebar.collabConnected");
    return this.plugin.t("sidebar.collabDisconnected");
  }

  getPendingCollabs(): CollabEntry[] {
    return this.collabs.filter(
      (entry) =>
        entry.status === "pending" &&
        entry.collaborator_username === this.plugin.settings.username
    );
  }

  getCollaborations(): readonly CollabEntry[] {
    return this.collabs;
  }

  getHistoryTarget(path: string): { vaultID: string; path: string; canRestore: boolean } | null {
    const normalized = normalizePath(path);
    for (const entry of this.collabs) {
      if (entry.status !== "accepted") continue;
      if (collabLocalPath(entry.owner_username, entry.file_path) !== normalized) continue;
      return { vaultID: entry.vault_id, path: entry.file_path, canRestore: false };
    }
    return null;
  }

  /** 刷新协作列表，并同步已接受协作文件的远端正文到 协作oss/。 */
  async refresh(): Promise<void> {
    const vaultID = this.plugin.settings.vaultId;
    if (!this.api.hasToken() || !vaultID) {
      this.collabs = [];
      this.onChange();
      return;
    }
    try {
      const [vaultResult, inboxResult] = await Promise.all([
        this.api.collabList(vaultID),
        this.api.collabInbox(),
      ]);
      const merged = new Map<number, CollabEntry>();
      for (const entry of vaultResult.collaborations) merged.set(entry.id, entry);
      for (const entry of inboxResult.collaborations) merged.set(entry.id, entry);
      this.collabs = [...merged.values()];
      this.diagnostics?.record({
        kind: "collab_activity",
        at: Date.now(),
        entries: this.collabs.length,
        newestCreatedAt: newestCreatedAt(this.collabs),
      });
      this.onChange();
      await this.syncAcceptedContents();
    } catch (error) {
      new Notice(this.plugin.t("collab.loadFailed", { error: errorMessage(error, this.plugin) }));
    }
  }

  /** 处理协作目录下的本地新增/修改：防抖 2 秒后上传。 */
  handleLocalEdit(path: string): void {
    if (!isCollabPath(path)) return;
    const key = normalizePath(path);
    if (this.suppressed.has(key)) return;
    const existing = this.uploadTimers.get(key);
    if (existing !== undefined) window.clearTimeout(existing);
    const timer = window.setTimeout(() => {
      this.uploadTimers.delete(key);
      void this.uploadLocalEdit(key);
    }, UPLOAD_DEBOUNCE_MS);
    this.uploadTimers.set(key, timer);
  }

  /** 接受或拒绝协作邀请。 */
  async respond(entry: CollabEntry, accept: boolean): Promise<void> {
    try {
      await this.api.collabRespond(entry.vault_id, entry.id, accept);
      new Notice(this.plugin.t(accept ? "collab.accepted" : "collab.rejected"));
      await this.refresh();
    } catch (error) {
      new Notice(this.plugin.t("collab.respondFailed", { error: errorMessage(error, this.plugin) }));
    }
  }

  /** 撤回邀请或解除协作。 */
  async revoke(entry: CollabEntry): Promise<void> {
    try {
      await this.api.collabRevoke(entry.vault_id, entry.id);
      new Notice(this.plugin.t("collab.revoked"));
      await this.refresh();
    } catch (error) {
      new Notice(this.plugin.t("collab.revokeFailed", { error: errorMessage(error, this.plugin) }));
    }
  }

  /** 退出已接受的协作关系。 */
  async leave(entry: CollabEntry): Promise<void> {
    try {
      await this.api.collabLeave(entry.vault_id, entry.id);
      new Notice(this.plugin.t("collab.left"));
      await this.refresh();
    } catch (error) {
      new Notice(this.plugin.t("collab.leaveFailed", { error: errorMessage(error, this.plugin) }));
    }
  }

  private async pollLoop(gen: number, controller: AbortController): Promise<void> {
    while (this.started && !controller.signal.aborted && gen === this.pollGen) {
      const vaultID = this.plugin.settings.vaultId;
      if (!vaultID || !this.api.hasToken()) return;
      const startedAt = Date.now();
      try {
        const result = await this.api.collabAccountPoll(this.after, COLLAB_POLL_WAIT_SEC);
        if (!this.started || controller.signal.aborted || gen !== this.pollGen) return;
        this.after = result.version;
        this.diagnostics?.record({
          kind: "poll",
          at: Date.now(),
          scope: "collab",
          changed: result.changed,
          version: result.version,
          durationMs: Date.now() - startedAt,
        });
        if (result.changed) {
          await this.applyRemoteChange();
        }
      } catch (error) {
        if (!this.started || controller.signal.aborted || gen !== this.pollGen) return;
        this.diagnostics?.record({
          kind: "poll",
          at: Date.now(),
          scope: "collab",
          durationMs: Date.now() - startedAt,
          failed: true,
        });
        await sleep(3000);
      }
    }
  }

  private startSSE(streamURL: string): void {
    const source = new EventSource(streamURL);
    this.eventSource = source;
    this.transport = "sse";
    this.streamOpenedAt = Date.now();
    this.diagnostics?.record({ kind: "sse_state", at: this.streamOpenedAt, state: "connecting" });
    this.onChange();
    source.onopen = () => this.diagnostics?.record({ kind: "sse_state", at: Date.now(), state: "open" });
    for (const event of ["changed", "revoked", "invited"] as const) {
      source.addEventListener(event, () => {
        this.diagnostics?.record({
          kind: "sse_event",
          at: Date.now(),
          event,
          connectionAgeMs: Date.now() - (this.streamOpenedAt ?? Date.now()),
        });
        if (event === "changed") {
          void this.applyRemoteChange();
        } else {
          void this.refresh();
        }
      });
    }
    source.onerror = () => {
      if (!this.started || this.eventSource !== source) return;
      source.close();
      this.eventSource = null;
      this.diagnostics?.record({
        kind: "sse_state",
        at: Date.now(),
        state: "failed",
        reason: this.plugin.settings.forceSSE ? "forced" : "fallback",
      });
      if (this.plugin.settings.forceSSE) {
        this.failSSE();
        return;
      }
      this.startLongPoll();
    };
  }

  private failSSE(): void {
    this.transport = "sse_failed";
    this.onChange();
  }

  private startLongPoll(): void {
    this.transport = "long_poll";
    this.onChange();
    const gen = ++this.pollGen;
    const controller = new AbortController();
    this.pollController = controller;
    void this.pollLoop(gen, controller);
  }

  private async applyRemoteChange(): Promise<void> {
    await Promise.all([
      this.refresh(),
      this.plugin.syncEngine.runOnce({ forceFull: false }),
    ]);
  }

  /** 把已接受协作文件的最新正文下载到 协作oss/。 */
  private async syncAcceptedContents(): Promise<void> {
    const accepted = this.collabs.filter(
      (entry) =>
        entry.status === "accepted" &&
        entry.collaborator_username === this.plugin.settings.username
    );
    for (const entry of accepted) {
      try {
        const result = await this.api.downloadCollabContent(entry.vault_id, entry.file_id);
        if (!result) {
          continue;
        }
        await this.writeCollabFile(collabLocalPath(entry.owner_username, entry.file_path), result.content);
        new Notice(this.plugin.t("collab.downloaded", { path: entry.file_path }));
      } catch (error) {
        new Notice(this.plugin.t("collab.refreshFailed", { error: errorMessage(error, this.plugin) }));
      }
    }
    if (accepted.length > 0) this.onChange();
  }

  private async writeCollabFile(path: string, content: ArrayBuffer): Promise<void> {
    const existing = this.app.vault.getAbstractFileByPath(path);
    if (existing instanceof TFile) {
      this.suppress(path);
      await this.app.vault.modifyBinary(existing, content);
      return;
    }
    await this.ensureParentFolders(path);
    this.suppress(path);
    await this.app.vault.createBinary(path, content);
  }

  private async uploadLocalEdit(path: string): Promise<void> {
    const entry = this.findAcceptedByLocalPath(path);
    if (!entry || entry.file_id <= 0) return;
    const file = this.app.vault.getAbstractFileByPath(path);
    if (!(file instanceof TFile)) return;
    try {
      const content = await this.app.vault.read(file);
      await this.api.collabUpload(entry.vault_id, entry.file_id, content);
      new Notice(this.plugin.t("collab.uploaded", { path: entry.file_path }));
      this.onChange();
    } catch (error) {
      new Notice(this.plugin.t("collab.uploadFailed", { error: errorMessage(error, this.plugin) }));
    }
  }

  private findAcceptedByLocalPath(path: string): CollabEntry | null {
    const p = normalizePath(path);
    for (const entry of this.collabs) {
      if (entry.status !== "accepted") continue;
      if (collabLocalPath(entry.owner_username, entry.file_path) === p) return entry;
    }
    return null;
  }

  private suppress(path: string): void {
    const key = normalizePath(path);
    this.suppressed.add(key);
    window.setTimeout(() => this.suppressed.delete(key), SUPPRESS_MS);
  }

  private async ensureParentFolders(path: string): Promise<void> {
    const parts = normalizePath(path).split("/");
    parts.pop();
    let current = "";
    for (const part of parts) {
      current = current ? `${current}/${part}` : part;
      if (!this.app.vault.getAbstractFileByPath(current)) {
        try {
          await this.app.vault.createFolder(current);
        } catch {
          if (!this.app.vault.getAbstractFileByPath(current)) {
            throw new Error(this.plugin.t("collab.mkdirFailed", { path: current }));
          }
        }
      }
    }
  }
}

/** 邀请协作弹窗：输入对方用户名后发送邀请。 */
export class CollabInviteModal extends Modal {
  private username = "";

  constructor(app: App, private plugin: OSSPlugin, private filePath: string) {
    super(app);
  }

  onOpen(): void {
    const { contentEl, titleEl } = this;
    titleEl.setText(this.plugin.t("collab.inviteTitle", { path: this.filePath }));

    new Setting(contentEl)
      .setName(this.plugin.t("collab.username"))
      .setDesc(this.plugin.t("collab.usernameDesc"))
      .addText((text) =>
        text
          .setPlaceholder(this.plugin.t("collab.usernamePlaceholder"))
          .onChange((value) => {
            this.username = value.trim();
          })
      );

    new Setting(contentEl)
      .addButton((button) =>
        button.setButtonText(this.plugin.t("common.cancel")).setWarning().onClick(() => this.close())
      )
      .addButton((button) =>
        button.setButtonText(this.plugin.t("collab.send")).onClick(() => void this.invite())
      );
  }

  onClose(): void {
    this.contentEl.empty();
  }

  private async invite(): Promise<void> {
    if (!this.username) {
      new Notice(this.plugin.t("collab.usernameRequired"));
      return;
    }
    const vaultID = this.plugin.settings.vaultId;
    if (!vaultID) {
      new Notice(this.plugin.t("collab.bindFirst"));
      return;
    }
    try {
      await this.plugin.api.collabInvite(vaultID, this.filePath, this.username);
      new Notice(this.plugin.t("collab.sent"));
      this.close();
    } catch (error) {
      new Notice(this.plugin.t("collab.sendFailed", { error: errorMessage(error, this.plugin) }));
    }
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function errorMessage(error: unknown, plugin: OSSPlugin): string {
	return localizeError(error, plugin.t.bind(plugin), plugin.t("common.unknownError"));
}

function newestCreatedAt(entries: readonly CollabEntry[]): string | undefined {
  let newest: string | undefined;
  for (const entry of entries) {
    if (entry.created_at && (!newest || entry.created_at > newest)) newest = entry.created_at;
  }
  return newest;
}

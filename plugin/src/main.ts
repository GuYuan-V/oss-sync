// Obsidian 插件入口。

import {
  App,
  getLanguage as getObsidianLanguage,
  Notice,
  Plugin,
  PluginManifest,
  TAbstractFile,
  TFile,
  TFolder,
  Vault,
} from "obsidian";
import type { Command } from "obsidian";
import { OSSApiClient, VaultOut } from "./api";
import type { ShareOut } from "./api";
import type { AuthResponse } from "./api";
import { BaselineStore } from "./baseline";
import { ConflictModal, ConflictResolution } from "./conflict-modal";
import { OSSSettingTab } from "./settings-tab";
import { DEFAULT_SETTINGS, OSSSettings } from "./settings";
import { Diagnostics } from "./diagnostics";
import type { DiagnosticEvent } from "./diagnostics";
import { ShareModal } from "./share-modal";
import { SyncEngine, SyncState } from "./sync-engine";
import { CollabInviteModal, CollabManager, isCollabPath } from "./collab-manager";
import { HistoryModal } from "./history-modal";
import { SidebarView, SIDEBAR_VIEW_TYPE } from "./sidebar-view";
import { ShareManagerModal } from "./share-manager-modal";
import { CollabManagerModal } from "./collab-manager-modal";
import { RecycleManagerModal } from "./recycle-manager-modal";
import {
  createClientID,
  loginWithRevokedDeviceRecovery,
  type DeviceLoginRecoveryResult,
} from "./device-login-recovery";
import { shouldInitializeAuthorizedSession } from "./login-state";
import {
  resolveLanguage,
  translate,
  type PluginLanguage,
  type TranslationKey,
  type TranslationParams,
} from "./i18n";
import { localizeError } from "./localized-error";

interface PluginData extends OSSSettings {
  token?: string;
}

export default class OSSPlugin extends Plugin {
  settings: OSSSettings = DEFAULT_SETTINGS;
  api: OSSApiClient;
  baseline!: BaselineStore;
  syncEngine!: SyncEngine;
  collabManager!: CollabManager;
  sidebarView?: SidebarView;

  private readonly diagnostics = new Diagnostics(() => undefined);
  private token?: string;
  private statusBarEl?: HTMLElement;
  private ribbonEl?: HTMLElement;
  private readonly localizedCommands: Array<{ command: Command; key: TranslationKey }> = [];
  availableVaults: VaultOut[] = [];

  constructor(app: App, manifest: PluginManifest) {
    super(app, manifest);
    this.api = new OSSApiClient(this.settings, this.diagnostics);
  }

  async onload(): Promise<void> {
    await this.loadSettings();

    this.api = new OSSApiClient(this.settings, this.diagnostics);
    if (this.token) this.api.setToken(this.token);

    this.baseline = new BaselineStore(this.app.vault);
    this.syncEngine = new SyncEngine(this.app, this.api, this.baseline, this, this.diagnostics);
    this.syncEngine.start();

    this.collabManager = new CollabManager(
      this.app,
      this.api,
      this,
      () => this.sidebarView?.refresh(),
      this.diagnostics
    );

    this.registerView(SIDEBAR_VIEW_TYPE, (leaf) => {
      this.sidebarView = new SidebarView(leaf, this);
      return this.sidebarView;
    });
    this.ribbonEl = this.addRibbonIcon("refresh-cw", this.t("ribbon.openSidebar"), () => {
      void this.activateSidebar();
    });
    this.ribbonEl.addClass("oss-ribbon-button");
    this.ribbonEl.setAttribute("data-oss-sync-ribbon", "true");

    this.statusBarEl = this.addStatusBarItem();
    this.statusBarEl.addClass("oss-status-bar");
    this.setSyncState("idle");

    this.statusBarEl.onClickEvent(() => {
      void this.activateSidebar();
    });

    this.registerCommands();

    this.registerEvent(
      this.app.vault.on("create", (f: TAbstractFile) => {
        if (f instanceof TFile && isCollabPath(f.path)) {
          this.collabManager.handleLocalEdit(f.path);
          return;
        }
        if (f instanceof TFile && !this.syncEngine.isSuppressed(f.path)) {
          this.syncEngine.enqueueUpsert(normalizeRel(f.path));
        }
      })
    );
    this.registerEvent(
      this.app.vault.on("modify", (f: TAbstractFile) => {
        if (f instanceof TFile && isCollabPath(f.path)) {
          this.collabManager.handleLocalEdit(f.path);
          return;
        }
        if (f instanceof TFile && !this.syncEngine.isSuppressed(f.path)) {
          this.syncEngine.enqueueUpsert(normalizeRel(f.path));
        }
      })
    );
    this.registerEvent(
      this.app.vault.on("delete", (f: TAbstractFile) => {
        if (isCollabPath(f.path)) return;
        if (f instanceof TFile && !this.syncEngine.isSuppressed(f.path)) {
          this.syncEngine.enqueueDelete(normalizeRel(f.path));
        } else if (f instanceof TFolder && !this.syncEngine.isSuppressed(f.path)) {
          this.syncEngine.enqueueDeleteTree(normalizeRel(f.path));
        }
      })
    );
    this.registerEvent(
      this.app.vault.on("rename", (f: TAbstractFile, oldPath: string) => {
        if (isCollabPath(f.path) || isCollabPath(oldPath)) return;
        if (f instanceof TFile && !this.syncEngine.isSuppressed(f.path)) {
          this.syncEngine.enqueueRename(normalizeRel(oldPath), normalizeRel(f.path));
        } else if (f instanceof TFolder) {
          const newRoot = normalizeRel(f.path);
          const oldRoot = normalizeRel(oldPath);
          Vault.recurseChildren(f, (child) => {
            if (!(child instanceof TFile) || this.syncEngine.isSuppressed(child.path)) return;
            const suffix = normalizeRel(child.path).slice(newRoot.length).replace(/^\/+/, "");
            const previousPath = suffix ? `${oldRoot}/${suffix}` : oldRoot;
            this.syncEngine.enqueueRename(previousPath, normalizeRel(child.path));
          });
        }
      })
    );

    this.registerEvent(
      this.app.workspace.on("file-menu", (menu, file) => {
        if (file instanceof TFile) {
          menu.addItem((item) => {
            item
              .setTitle(this.t("menu.fileHistory"))
              .setIcon("history")
              .onClick(() => {
                new HistoryModal(this.app, this, file.path).open();
              });
          });
        }
        if (file instanceof TFile && file.extension === "md") {
          menu.addItem((item) => {
            item
              .setTitle(this.t("menu.inviteCollaboration"))
              .setIcon("user-plus")
              .onClick(() => {
                new CollabInviteModal(this.app, this, file.path).open();
              });
          });
        }
        if (file instanceof TFile || file instanceof TFolder) {
          menu.addItem((item) => {
            item
              .setTitle(this.t("menu.share"))
              .setIcon("share")
              .onClick(() => {
                void this.toggleShare(file);
              });
            void this.updateShareMenuItem(item, file);
          });
        }
      })
    );

    this.addSettingTab(new OSSSettingTab(this.app, this));

    this.app.workspace.onLayoutReady(() => {
      if (this.token) {
        void this.ensureVaultBinding().then(() => {
          if (this.settings.vaultId) {
            this.collabManager.start();
            void this.syncEngine.runOnce({ forceFull: true });
          }
        }).catch((error: unknown) => {
          new Notice(this.t("notice.loadVaultsFailed", { error: this.localizedError(error) }));
        });
      }
    });
  }

  onunload(): void {
    this.syncEngine?.stop();
    this.collabManager?.stop();
    this.app.workspace.detachLeavesOfType(SIDEBAR_VIEW_TYPE);
  }

  private registerCommands(): void {
    this.addLocalizedCommand("command.openSidebar", {
      id: "oss-sync-open-sidebar",
      callback: () => {
        void this.activateSidebar();
      },
    });
    this.addLocalizedCommand("command.forceFullSync", {
      id: "oss-sync-force-sync",
      callback: () => {
        if (!this.settings.vaultId) {
          new Notice(this.t("notice.bindVaultFirst"));
          return;
        }
        new Notice(this.t("notice.fullSyncTriggered"));
        void this.syncEngine.runOnce({ forceFull: true });
      },
    });
    this.addLocalizedCommand("command.syncCurrentVault", {
      id: "oss-sync-now",
      callback: () => {
        if (!this.settings.vaultId) {
          new Notice(this.t("notice.bindVaultFirst"));
          return;
        }
        void this.syncEngine.runOnce({ forceFull: true });
      },
    });
    this.addLocalizedCommand("command.openConsole", {
      id: "oss-sync-open-console",
      callback: () => {
        window.open(this.webURL("/dashboard"), "_blank", "noopener,noreferrer");
      },
    });
    this.addLocalizedCommand("command.fileHistory", {
      id: "oss-file-history",
      callback: () => {
        const file = this.app.workspace.getActiveFile();
        if (!file) {
          new Notice(this.t("notice.noActiveFile"));
          return;
        }
        new HistoryModal(this.app, this, file.path).open();
      },
    });
  }

  private addLocalizedCommand(key: TranslationKey, command: Omit<Command, "name">): void {
    const registered = this.addCommand({ ...command, name: this.t(key) });
    this.localizedCommands.push({ command: registered, key });
  }

  private async activateSidebar(): Promise<void> {
    const leaves = this.app.workspace.getLeavesOfType(SIDEBAR_VIEW_TYPE);
    if (leaves.length > 0) {
      await this.app.workspace.revealLeaf(leaves[0]);
      return;
    }
    const leaf = this.app.workspace.getRightLeaf(false);
    if (!leaf) return;
    await leaf.setViewState({ type: SIDEBAR_VIEW_TYPE, active: true });
    await this.app.workspace.revealLeaf(leaf);
  }

  async loadSettings(): Promise<void> {
    const data = (await this.loadData()) as PluginData | null;
    if (data) {
      this.settings = Object.assign({}, DEFAULT_SETTINGS, data);
      this.token = data.token;
    } else {
      this.settings = Object.assign({}, DEFAULT_SETTINGS);
    }
    // Passwords from older plugin versions are never retained after loading.
    this.settings.password = "";
    if (!this.settings.clientId) {
      this.settings.clientId = createClientID();
      await this.saveSettings();
    }
    if (!this.settings.deviceName) {
      this.settings.deviceName = `${this.app.vault.getName()} - Obsidian`;
      await this.saveSettings();
    }
  }

  async saveSettings(): Promise<void> {
    const data: PluginData = { ...this.settings, password: "", token: this.token };
    await this.saveData(data);
  }

  getLanguage(): PluginLanguage {
    return resolveLanguage(this.settings.language, getObsidianLanguage());
  }

  webURL(path: "/dashboard" | "/register"): string {
    return new URL(path, this.settings.serverUrl.replace(/\/$/, "") + "/").toString();
  }

  t(key: TranslationKey, params: TranslationParams = {}): string {
    return translate(this.getLanguage(), key, params);
  }

  localizedError(error: unknown): string {
    return localizeError(error, this.t.bind(this), this.t("common.unknownError"));
  }

  getDiagnostics(): readonly DiagnosticEvent[] {
    return this.diagnostics.snapshot();
  }

  refreshLocalizedUI(): void {
    const ribbonLabel = this.t("ribbon.openSidebar");
    this.ribbonEl?.setAttribute("aria-label", ribbonLabel);
    this.ribbonEl?.setAttribute("data-tooltip-position", "right");
    this.ribbonEl?.setAttribute("title", ribbonLabel);
    for (const item of this.localizedCommands) {
      item.command.name = this.t(item.key);
    }
    this.sidebarView?.refresh();
  }

  async login(): Promise<DeviceLoginRecoveryResult<AuthResponse>> {
    const result = await loginWithRevokedDeviceRecovery(
      () => this.api.login(),
      async () => {
        this.settings.clientId = createClientID();
        this.token = undefined;
        this.api.setToken(null);
        await this.saveSettings();
      }
    );
    const res = result.response;
    this.token = res.token;
    this.settings.password = "";
    await this.saveSettings();
    if (!shouldInitializeAuthorizedSession(res.device_status)) {
      this.settings.vaultId = "";
      this.settings.vaultName = "";
      await this.saveSettings();
      this.collabManager.stop();
      return result;
    }
    await this.ensureVaultBinding();
    this.syncEngine.start();
    if (this.settings.vaultId) {
      this.collabManager.start();
    }
    return result;
  }

  isLoggedIn(): boolean {
    return this.api.hasToken();
  }

  async logout(): Promise<void> {
    this.token = undefined;
    this.api.setToken(null);
    this.availableVaults = [];
    this.settings.password = "";
    this.settings.vaultId = "";
    this.settings.vaultName = "";
    this.syncEngine.stop();
    this.collabManager.stop();
    await this.saveSettings();
    this.setSyncState("idle");
  }

  openSettings(): void {
    const settings = Reflect.get(this.app, "setting");
    if (!isSettingsController(settings)) return;
    settings.open();
    settings.openTabById(this.manifest.id);
  }

  openShareManager(): void {
    new ShareManagerModal(this.app, this).open();
  }

  openCollabManager(): void {
    new CollabManagerModal(this.app, this).open();
  }

  openRecycleManager(): void {
    new RecycleManagerModal(this.app, this).open();
  }

  private async toggleShare(file: TFile | TFolder): Promise<void> {
    try {
      const existing = findShare(await this.api.listShares(), file);
      if (!existing) {
        new ShareModal(this.app, this, file).open();
        return;
      }
      await this.api.deleteShare(existing.share_id);
      this.sidebarView?.reloadShares();
      new Notice(this.t("sidebar.deleteShare"));
    } catch (error) {
      new Notice(this.t("sidebar.shareActionFailed", { error: this.localizedError(error) }));
    }
  }

  private async updateShareMenuItem(item: { setTitle(title: string): unknown; setIcon(icon: string): unknown }, file: TFile | TFolder): Promise<void> {
    try {
      const existing = findShare(await this.api.listShares(), file);
      item.setTitle(this.t(existing ? "menu.unshare" : "menu.share"));
      item.setIcon(existing ? "x" : "share");
    } catch {
      // Keep the default share action when the current share state cannot load.
    }
  }

  async refreshVaults(): Promise<VaultOut[]> {
    if (!this.api.hasToken()) {
      this.availableVaults = [];
      return [];
    }
    const result = await this.api.listVaults();
    this.availableVaults = result.vaults;
    return this.availableVaults;
  }

  async ensureVaultBinding(): Promise<void> {
    const vaults = await this.refreshVaults();
    if (vaults.length === 0) {
      if (this.settings.vaultId || this.settings.vaultName) {
        this.settings.vaultId = "";
        this.settings.vaultName = "";
        await this.saveSettings();
        this.collabManager.stop();
      }
      return;
    }
    const current = vaults.find((vault) => vault.id === this.settings.vaultId);
    if (current) {
      this.settings.vaultName = current.name;
      await this.saveSettings();
      return;
    }
    if (this.settings.vaultId || this.settings.vaultName) {
      this.settings.vaultId = "";
      this.settings.vaultName = "";
      await this.saveSettings();
      this.collabManager.stop();
    }
  }

  async bindVault(vault: VaultOut): Promise<boolean> {
    const changed = this.settings.vaultId !== vault.id;
    this.settings.vaultId = vault.id;
    this.settings.vaultName = vault.name;
    await this.saveSettings();
    await this.baseline.load();
    if (this.baseline.bindVault(vault.id)) {
      await this.baseline.save();
    }
    this.collabManager.start();
    if (changed) {
      return this.syncEngine.runOnce({ forceFull: true });
    }
    return true;
  }

  setSyncState(state: SyncState, label?: string): void {
    if (!this.statusBarEl) return;
    this.statusBarEl.empty();
    this.statusBarEl.removeClass("is-syncing", "is-error");
    const text = label ? `: ${label}` : "";
    const span = this.statusBarEl.createSpan();
    if (state === "syncing") {
      this.statusBarEl.addClass("is-syncing");
      span.setText(this.t("status.syncing", { detail: text }));
    } else if (state === "error") {
      this.statusBarEl.addClass("is-error");
      span.setText(this.t("status.error", { detail: text ? ` ${text}` : "" }));
    } else {
      span.setText(this.t("status.idle"));
    }
    this.sidebarView?.refresh();
  }

  openConflictModal(path: string): void {
    const file = this.app.vault.getAbstractFileByPath(path);
    if (!(file instanceof TFile)) {
      new Notice(this.t("notice.conflictFileMissing", { path }));
      this.syncEngine.dismissConflict(path);
      this.sidebarView?.refresh();
      return;
    }
    void (async () => {
      let remote: string;
      try {
        const conflict = this.syncEngine.getConflict(path);
        if (!conflict || conflict.remoteDeleted) {
          new Notice(this.t("notice.conflictTextUnavailable"));
          return;
        }
        const res = await this.api.downloadV2(
          this.settings.vaultId,
          path,
          conflict.remoteRevision
        );
        remote = new TextDecoder().decode(new Uint8Array(res.content));
      } catch (e) {
        new Notice(this.t("notice.fetchRemoteFailed", { error: this.localizedError(e) }));
        return;
      }
      new ConflictModal(this.app, this, this.api, file, remote, async (r) => {
        await this.applyConflictResolution(path, r);
      }).open();
    })();
  }

  async applyConflictResolution(path: string, r: ConflictResolution): Promise<void> {
    await this.syncEngine.resolveConflict(path, r);
    const resolutionKeys: Record<ConflictResolution, TranslationKey> = {
      accept_remote: "conflict.acceptRemoteButton",
      force_local: "conflict.forceLocalButton",
      keep_both: "conflict.keepBothButton",
    };
    new Notice(this.t("notice.conflictResolved", { resolution: this.t(resolutionKeys[r]) }), 4000);
  }
}

function normalizeRel(p: string): string {
  return p.replace(/\\/g, "/").replace(/^\.\/+/, "");
}

function findShare(shares: { readonly shares: readonly ShareOut[] }, file: TFile | TFolder): ShareOut | undefined {
  return shares.shares.find(
    (share) => share.target_path === file.path && share.is_folder === (file instanceof TFolder)
  );
}

interface SettingsController {
  open(): void;
  openTabById(id: string): void;
}

function isSettingsController(value: unknown): value is SettingsController {
  if (!value || typeof value !== "object") return false;
  const open = Reflect.get(value, "open");
  const openTabById = Reflect.get(value, "openTabById");
  return typeof open === "function" && typeof openTabById === "function";
}

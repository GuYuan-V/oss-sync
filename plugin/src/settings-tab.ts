import { App, Notice, PluginSettingTab, Setting } from "obsidian";
import type OSSPlugin from "./main";
import type { OSSSettings } from "./settings";
import { validateLoginCredentials } from "./login-state";

export class OSSSettingTab extends PluginSettingTab {
  constructor(app: App, private plugin: OSSPlugin) {
    super(app, plugin);
  }

  display(): void {
    const { containerEl } = this;
    containerEl.empty();

    containerEl.createEl("h3", { text: this.plugin.t("settings.language.title") });

    new Setting(containerEl)
      .setName(this.plugin.t("settings.language.name"))
      .setDesc(this.plugin.t("settings.language.desc"))
      .addDropdown((dropdown) =>
        dropdown
          .addOption("auto", this.plugin.t("settings.language.auto"))
          .addOption("zh", this.plugin.t("settings.language.zh"))
          .addOption("en", this.plugin.t("settings.language.en"))
          .setValue(this.plugin.settings.language)
          .onChange(async (value) => {
            this.plugin.settings.language = parseLanguage(value);
            await this.plugin.saveSettings();
            this.plugin.refreshLocalizedUI();
            this.display();
          })
      );

    containerEl.createEl("h3", { text: this.plugin.t("settings.server.title") });

    new Setting(containerEl)
      .setName(this.plugin.t("settings.server.url"))
      .setDesc(this.plugin.t("settings.server.urlDesc"))
      .addText((text) =>
        text
          .setPlaceholder("http://localhost:8080")
          .setValue(this.plugin.settings.serverUrl)
          .onChange(async (value) => {
            this.plugin.settings.serverUrl = value.replace(/\/$/, "");
            await this.plugin.saveSettings();
          })
      );

    if (this.plugin.isLoggedIn()) {
      new Setting(containerEl)
        .setName(this.plugin.t("settings.server.signedIn"))
        .setDesc(this.plugin.t("settings.server.signedInAs", { username: this.plugin.settings.username }))
        .addButton((button) =>
          button.setButtonText(this.plugin.t("settings.server.logoutButton")).setWarning().onClick(async () => {
            await this.plugin.logout();
            new Notice(this.plugin.t("notice.logoutSuccess"));
            this.display();
          })
        );
    } else {
      new Setting(containerEl)
        .setName(this.plugin.t("settings.server.username"))
        .addText((text) =>
          text
            .setValue(this.plugin.settings.username)
            .onChange(async (value) => {
              this.plugin.settings.username = value;
              await this.plugin.saveSettings();
            })
        );

      new Setting(containerEl)
        .setName(this.plugin.t("settings.server.password"))
        .addText((text) => {
          text.inputEl.type = "password";
          text
            .setValue(this.plugin.settings.password)
            .onChange((value) => {
              this.plugin.settings.password = value;
            });
        });

      const authSetting = new Setting(containerEl)
        .setName(this.plugin.t("settings.server.login"))
        .setDesc(this.plugin.t("settings.server.checking"))
        .addButton((btn) =>
          btn.setButtonText(this.plugin.t("settings.server.loginButton")).onClick(async () => {
            const error = this.validateCredentials();
            if (error) {
              new Notice(error === this.plugin.t("settings.server.usernameRequired") || error === this.plugin.t("settings.server.passwordRequired") ? error : this.plugin.t("notice.invalidCredentials"));
              return;
            }
            try {
              const login = await this.plugin.login();
              let message = this.plugin.settings.vaultId
                ? this.plugin.t("notice.loginSuccess")
                : this.plugin.t("notice.loginSuccessNoVault");
              if (login.response.device_status === "pending") {
                message = this.plugin.t("notice.loginPending");
              }
              if (login.replacedRevokedIdentity) {
                message = this.plugin.t("notice.loginReregistered");
              }
              new Notice(message);
              this.display();
            } catch (e) {
              new Notice(this.plugin.t("notice.loginFailed", { error: this.errorMessage(e) }));
            }
          })
        )
        .addButton((btn) =>
          btn.setButtonText(this.plugin.t("settings.server.registrationButton")).onClick(() => {
            window.open(this.plugin.webURL("/register"), "_blank", "noopener,noreferrer");
          })
        );

      void this.plugin.api.authStatus().then((status) => {
        if (status.needs_first_admin) {
          authSetting.setDesc(this.plugin.t("settings.server.needsAdmin"));
        } else if (status.registration_enabled) {
          authSetting.setDesc(this.plugin.t("settings.server.registrationOpen"));
        } else {
          authSetting.setDesc(this.plugin.t("settings.server.registrationClosed"));
        }
      }).catch((e: unknown) => {
        authSetting.setDesc(this.plugin.t("settings.server.statusUnavailable"));
        new Notice(this.plugin.t("notice.authStatusFailed", { error: this.errorMessage(e) }));
      });
    }

    containerEl.createEl("h3", { text: this.plugin.t("settings.vault.title") });

    const boundVaultSetting = new Setting(containerEl)
      .setName(this.plugin.t("settings.vault.bound"))
      .setDesc(this.plugin.t("settings.vault.boundDesc"))
      .addDropdown((dropdown) => {
        dropdown.addOption("", this.plugin.t("settings.vault.select"));
        dropdown.setValue(this.plugin.settings.vaultId);
        dropdown.onChange(async (vaultID) => {
          const vault = this.plugin.availableVaults.find((item) => item.id === vaultID);
          if (!vault) return;
          try {
            const synced = await this.plugin.bindVault(vault);
            new Notice(this.plugin.t(synced ? "notice.vaultBound" : "notice.vaultBoundPartial", { name: vault.name }));
          } catch (error: unknown) {
            new Notice(this.plugin.t("notice.vaultBindFailed", { error: this.errorMessage(error) }));
          }
        });
        void this.plugin.refreshVaults().then((vaults) => {
          if (vaults.length === 0) {
            dropdown.addOption("", this.plugin.t("settings.vault.none"));
            boundVaultSetting.setDesc(this.plugin.t("settings.vault.noneDesc"));
          }
          for (const vault of vaults) {
            dropdown.addOption(
              vault.id,
              vault.is_default
                ? `${vault.name} (${this.plugin.t("settings.vault.defaultSuffix")})`
                : vault.name
            );
          }
          dropdown.setValue(this.plugin.settings.vaultId);
        }).catch(() => {
          // 尚未登录时保留空列表。
        });
      });
    let newVaultName = "";
    new Setting(containerEl)
      .setName(this.plugin.t("settings.vault.create"))
      .setDesc(this.plugin.t("settings.vault.createDesc"))
      .addText((text) =>
        text.setPlaceholder(this.plugin.t("settings.vault.namePlaceholder")).onChange((value) => {
          newVaultName = value.trim();
        })
      )
      .addButton((button) =>
        button.setButtonText(this.plugin.t("settings.vault.createButton")).onClick(async () => {
          if (!newVaultName) {
            new Notice(this.plugin.t("notice.vaultNameRequired"));
            return;
          }
          button.setDisabled(true);
          try {
            const vault = await this.plugin.api.createVault(newVaultName);
            await this.plugin.refreshVaults();
            const synced = await this.plugin.bindVault(vault);
            new Notice(this.plugin.t(synced ? "notice.vaultCreated" : "notice.vaultCreatedPartial", { name: vault.name }));
            this.display();
          } catch (error: unknown) {
            new Notice(this.plugin.t("notice.vaultCreateFailed", { error: this.errorMessage(error) }));
          } finally {
            button.setDisabled(false);
          }
        })
      );

    containerEl.createEl("h3", { text: this.plugin.t("settings.devices.title") });

    let nextDeviceName = this.plugin.settings.deviceName;
    new Setting(containerEl)
      .setName(this.plugin.t("settings.devices.thisDevice"))
      .setDesc(this.plugin.t("settings.devices.thisDeviceDesc"))
      .addText((text) =>
        text.setValue(nextDeviceName).onChange((value) => {
          nextDeviceName = value.trim();
        })
      )
      .addButton((button) =>
        button.setButtonText(this.plugin.t("settings.devices.rename")).onClick(async () => {
          if (!nextDeviceName) {
            new Notice(this.plugin.t("notice.deviceNameRequired"));
            return;
          }
          try {
            await this.plugin.api.renameDevice(this.plugin.settings.clientId, nextDeviceName);
            this.plugin.settings.deviceName = nextDeviceName;
            await this.plugin.saveSettings();
            new Notice(this.plugin.t("notice.deviceRenamed"));
            this.display();
          } catch (error: unknown) {
            new Notice(this.plugin.t("notice.deviceRenameFailed", { error: this.errorMessage(error) }));
          }
        })
      );

    const devicesEl = containerEl.createDiv({ cls: "oss-device-list" });
    devicesEl.setText(this.plugin.t("settings.devices.loading"));
    void this.plugin.api.listDevices().then((result) => {
      devicesEl.empty();
      for (const device of result.devices) {
        let deviceName = device.name || device.client_id;
        const cursorSummary = device.vaults.length > 0
          ? device.vaults
              .map((vault) => `${vault.vault_name}: ${vault.last_cursor}/${vault.head_revision}`)
              .join(" · ")
          : this.plugin.t("common.noVaultSync");
        const state = this.plugin.t(device.stale ? "common.expired" : "common.valid");
        const lastSeen = device.last_seen_at
          ? new Date(device.last_seen_at).toLocaleString()
          : this.plugin.t("common.unknown");
        const setting = new Setting(devicesEl)
          .setName(`${device.name || this.plugin.t("settings.devices.unnamed")}${device.is_current ? ` (${this.plugin.t("settings.devices.currentSuffix")})` : ""}`)
          .setDesc(this.plugin.t("settings.devices.summary", { state, lastSeen, cursor: cursorSummary }))
          .addText((text) =>
            text.setValue(device.name).onChange((value) => {
              deviceName = value.trim();
            })
          )
          .addButton((button) =>
            button.setButtonText(this.plugin.t("common.save")).onClick(async () => {
              if (!deviceName) {
                new Notice(this.plugin.t("notice.deviceNameRequired"));
                return;
              }
              try {
                await this.plugin.api.renameDevice(device.client_id, deviceName);
                if (device.is_current) {
                  this.plugin.settings.deviceName = deviceName;
                  await this.plugin.saveSettings();
                }
                this.display();
              } catch (error: unknown) {
                new Notice(this.plugin.t("notice.deviceRenameFailed", { error: this.errorMessage(error) }));
              }
            })
          );
        if (!device.is_current) {
          setting.addButton((button) =>
            button
              .setButtonText(this.plugin.t("settings.devices.revoke"))
              .setWarning()
              .onClick(async () => {
                try {
                  await this.plugin.api.revokeDevice(device.client_id);
                  new Notice(this.plugin.t("notice.deviceRevoked", { name: device.name || device.client_id }));
                  this.display();
                } catch (error: unknown) {
                  new Notice(this.plugin.t("notice.deviceRevokeFailed", { error: this.errorMessage(error) }));
                }
              })
          );
        }
      }
      if (result.devices.length === 0) {
        devicesEl.setText(this.plugin.t("settings.devices.none"));
      }
    }).catch((error: unknown) => {
      devicesEl.setText(this.plugin.t("settings.devices.loadFailed"));
      new Notice(this.plugin.t("notice.devicesLoadFailed", { error: this.errorMessage(error) }));
    });

    containerEl.createEl("h3", { text: this.plugin.t("settings.sync.title") });

    new Setting(containerEl)
      .setName(this.plugin.t("settings.sync.interval"))
      .setDesc(this.plugin.t("settings.sync.intervalDesc"))
      .addText((text) =>
        text
          .setPlaceholder("3")
          .setValue(String(this.plugin.settings.syncIntervalSec))
          .onChange(async (value) => {
            const n = parseInt(value, 10);
            if (!isNaN(n) && n >= 1) {
              this.plugin.settings.syncIntervalSec = n;
              await this.plugin.saveSettings();
              this.plugin.syncEngine.resetDebounce();
            }
          })
      );

    new Setting(containerEl)
      .setName(this.plugin.t("settings.sync.mode"))
      .setDesc(this.plugin.t("settings.sync.modeDesc"))
      .addDropdown((dropdown) =>
        dropdown
          .addOption("short_poll", this.plugin.t("common.shortPoll"))
          .addOption("long_poll", this.plugin.t("common.longPoll"))
          .setValue(this.plugin.settings.vaultSyncMode)
          .onChange(async (value) => {
            this.plugin.settings.vaultSyncMode = value as "short_poll" | "long_poll";
            await this.plugin.saveSettings();
            this.plugin.syncEngine.resetPolling();
          })
      );

    new Setting(containerEl)
      .setName(this.plugin.t("settings.sync.maxConcurrency"))
      .setDesc(this.plugin.t("settings.sync.maxConcurrencyDesc"))
      .addText((text) =>
        text
          .setPlaceholder("6")
          .setValue(String(this.plugin.settings.maxConcurrency))
          .onChange(async (value) => {
            const n = parseInt(value, 10);
            if (!isNaN(n) && n >= 1 && n <= 10) {
              this.plugin.settings.maxConcurrency = n;
              await this.plugin.saveSettings();
            }
          })
      );

    new Setting(containerEl)
      .setName(this.plugin.t("settings.sync.localConfig"))
      .setDesc(this.plugin.t("settings.sync.localConfigDesc"))
      .addToggle((toggle) =>
        toggle
          .setValue(this.plugin.settings.syncPoisonObsidianFiles)
          .onChange(async (value) => {
            this.plugin.settings.syncPoisonObsidianFiles = value;
            await this.plugin.saveSettings();
          })
      );

    new Setting(containerEl)
      .setName(this.plugin.t("settings.sync.incremental"))
      .setDesc(this.plugin.t("settings.sync.incrementalDesc"))
      .addToggle((toggle) =>
        toggle
          .setValue(this.plugin.settings.incrementalCheck)
          .onChange(async (value) => {
            this.plugin.settings.incrementalCheck = value;
            await this.plugin.saveSettings();
          })
      );

    new Setting(containerEl)
      .setName(this.plugin.t("settings.sync.remoteInterval"))
      .setDesc(this.plugin.t("settings.sync.remoteIntervalDesc"))
      .addText((text) =>
        text
          .setPlaceholder("30")
          .setValue(String(this.plugin.settings.remotePollIntervalSec))
          .onChange(async (value) => {
            const n = parseInt(value, 10);
            if (!isNaN(n) && n >= 10) {
              this.plugin.settings.remotePollIntervalSec = n;
              await this.plugin.saveSettings();
              this.plugin.syncEngine.resetPolling();
            }
          })
      );

    new Setting(containerEl)
      .setName(this.plugin.t("settings.sync.forceSSE"))
      .setDesc(this.plugin.t("settings.sync.forceSSEDesc"))
      .addToggle((toggle) =>
        toggle
          .setValue(this.plugin.settings.forceSSE)
          .onChange(async (value) => {
            this.plugin.settings.forceSSE = value;
            await this.plugin.saveSettings();
            if (this.plugin.collabManager.isRunning()) {
              this.plugin.collabManager.stop();
              this.plugin.collabManager.start();
            }
          })
      );

    new Setting(containerEl)
      .setName(this.plugin.t("settings.sync.diagnostics"))
      .setDesc(this.plugin.t("settings.sync.diagnosticsDesc"))
      .addToggle((toggle) =>
        toggle
          .setValue(this.plugin.settings.diagnosticsEnabled)
          .onChange(async (value) => {
            this.plugin.settings.diagnosticsEnabled = value;
            await this.plugin.saveSettings();
          })
      );

    new Setting(containerEl)
      .setName(this.plugin.t("settings.sync.forceFull"))
      .setDesc(this.plugin.t("settings.sync.forceFullDesc"))
      .addButton((btn) =>
        btn.setButtonText(this.plugin.t("settings.sync.syncNow")).onClick(async () => {
          const synced = await this.plugin.syncEngine.runOnce({ forceFull: true });
          if (!synced) new Notice(this.plugin.t("notice.syncIncomplete"));
        })
      );
  }

  private validateCredentials(): string | null {
    const error = validateLoginCredentials(
      this.plugin.settings.username,
      this.plugin.settings.password
    );
    switch (error) {
      case "username_required":
        return this.plugin.t("settings.server.usernameRequired");
      case "password_required":
        return this.plugin.t("settings.server.passwordRequired");
      default:
        return null;
    }
  }

  private errorMessage(error: unknown): string {
    return this.plugin.localizedError(error);
  }
}

function parseLanguage(value: string): OSSSettings["language"] {
  switch (value) {
    case "zh":
      return "zh";
    case "en":
      return "en";
    default:
      return "auto";
  }
}

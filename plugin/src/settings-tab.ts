import { App, Notice, PluginSettingTab, Setting } from "obsidian";
import type OSSPlugin from "./main";
import type { OSSSettings } from "./settings";

export class OSSSettingTab extends PluginSettingTab {
  constructor(app: App, private plugin: OSSPlugin) {
    super(app, plugin);
  }

  display(): void {
    const { containerEl } = this;
    containerEl.empty();

    containerEl.createEl("h3", { text: "OSS Server" });

    new Setting(containerEl)
      .setName("Server URL")
      .setDesc("后端地址，例如 http://localhost:8080")
      .addText((text) =>
        text
          .setPlaceholder("http://localhost:8080")
          .setValue(this.plugin.settings.serverUrl)
          .onChange(async (value) => {
            this.plugin.settings.serverUrl = value.replace(/\/$/, "");
            await this.plugin.saveSettings();
          })
      );

    new Setting(containerEl)
      .setName("Username")
      .addText((text) =>
        text
          .setValue(this.plugin.settings.username)
          .onChange(async (value) => {
            this.plugin.settings.username = value;
            await this.plugin.saveSettings();
          })
      );

    new Setting(containerEl)
      .setName("Password")
      .addText((text) => {
        text.inputEl.type = "password";
        text
          .setValue(this.plugin.settings.password)
          .onChange((value) => {
            this.plugin.settings.password = value;
          });
      });

    const authSetting = new Setting(containerEl)
      .setName("Login")
      .setDesc("正在检查服务端注册状态…")
      .addButton((btn) =>
        btn.setButtonText("Login").onClick(async () => {
          const error = this.validateCredentials();
          if (error) {
            new Notice(error);
            return;
          }
          try {
            await this.plugin.login();
            new Notice(
              this.plugin.settings.vaultId
                ? "OSS 登录成功"
                : "OSS 登录成功；请在 Vault 区域创建服务端仓库后开始同步"
            );
          } catch (e) {
            new Notice("OSS 登录失败: " + this.errorMessage(e));
          }
        })
      )
      .addButton((btn) =>
        btn.setButtonText("Open registration page").onClick(() => {
          const url = this.plugin.settings.serverUrl.replace(/\/$/, "") + "/register";
          window.open(url, "_blank", "noopener,noreferrer");
        })
      );

    void this.plugin.api.authStatus().then((status) => {
      if (status.needs_first_admin) {
        authSetting.setDesc("服务端尚未完成管理员初始化，请检查服务端启动终端。");
      } else if (status.registration_enabled) {
        authSetting.setDesc("没有账户？请先在网页注册，然后回到这里登录。");
      } else {
        authSetting.setDesc("新用户注册已关闭；已有账户仍可直接登录。");
      }
    }).catch((e: unknown) => {
      authSetting.setDesc("无法读取服务端认证状态，请检查 Server URL 和后端服务。");
      new Notice("OSS 状态检查失败: " + this.errorMessage(e));
    });

    containerEl.createEl("h3", { text: "Vault" });

    const boundVaultSetting = new Setting(containerEl)
      .setName("Bound vault")
      .setDesc("明确选择后会绑定当前 Obsidian Vault，并立即执行一次全量同步。")
      .addDropdown((dropdown) => {
        dropdown.addOption("", "Select vault");
        dropdown.setValue(this.plugin.settings.vaultId);
        dropdown.onChange(async (vaultID) => {
          const vault = this.plugin.availableVaults.find((item) => item.id === vaultID);
          if (!vault) return;
          try {
            const synced = await this.plugin.bindVault(vault);
            new Notice(synced ? `OSS: 已绑定仓库 ${vault.name}` : `OSS: 已绑定仓库 ${vault.name}，但首次同步失败；请修复后重试`);
          } catch (error: unknown) {
            new Notice("OSS: 绑定仓库失败 " + this.errorMessage(error));
          }
        });
        void this.plugin.refreshVaults().then((vaults) => {
          if (vaults.length === 0) {
            dropdown.addOption("", "No vaults — create one below");
            boundVaultSetting.setDesc(
              "尚未创建服务端 Vault。请在下方明确创建；创建完成后才会开始同步。"
            );
          }
          for (const vault of vaults) {
            dropdown.addOption(vault.id, vault.is_default ? `${vault.name} (default)` : vault.name);
          }
          dropdown.setValue(this.plugin.settings.vaultId);
        }).catch(() => {
          // 尚未登录时保留空列表。
        });
      });
    let newVaultName = "";
    new Setting(containerEl)
      .setName("Create and sync vault")
      .setDesc("创建后会立即绑定当前 Obsidian Vault，并执行一次全量上传。")
      .addText((text) =>
        text.setPlaceholder("例如 Note").onChange((value) => {
          newVaultName = value.trim();
        })
      )
      .addButton((button) =>
        button.setButtonText("Create & sync").onClick(async () => {
          if (!newVaultName) {
            new Notice("请输入仓库名称");
            return;
          }
          button.setDisabled(true);
          try {
            const vault = await this.plugin.api.createVault(newVaultName);
            await this.plugin.refreshVaults();
            const synced = await this.plugin.bindVault(vault);
            new Notice(synced ? `OSS: 已创建 ${vault.name} 并完成首次同步` : `OSS: 已创建 ${vault.name}，但首次同步失败；请修复后重试`);
            this.display();
          } catch (error: unknown) {
            new Notice("OSS: 创建仓库失败 " + this.errorMessage(error));
          } finally {
            button.setDisabled(false);
          }
        })
      );

    containerEl.createEl("h3", { text: "Devices" });

    let nextDeviceName = this.plugin.settings.deviceName;
    new Setting(containerEl)
      .setName("This device")
      .setDesc("设备名称用于识别多台 Obsidian 客户端。")
      .addText((text) =>
        text.setValue(nextDeviceName).onChange((value) => {
          nextDeviceName = value.trim();
        })
      )
      .addButton((button) =>
        button.setButtonText("Rename").onClick(async () => {
          if (!nextDeviceName) {
            new Notice("设备名称不能为空");
            return;
          }
          try {
            await this.plugin.api.renameDevice(this.plugin.settings.clientId, nextDeviceName);
            this.plugin.settings.deviceName = nextDeviceName;
            await this.plugin.saveSettings();
            new Notice("OSS: 当前设备已重命名");
            this.display();
          } catch (error: unknown) {
            new Notice("OSS: 设备重命名失败 " + this.errorMessage(error));
          }
        })
      );

    const devicesEl = containerEl.createDiv({ cls: "oss-device-list" });
    devicesEl.setText("Loading devices...");
    void this.plugin.api.listDevices().then((result) => {
      devicesEl.empty();
      for (const device of result.devices) {
        let deviceName = device.name || device.client_id;
        const cursorSummary = device.vaults.length > 0
          ? device.vaults
              .map((vault) => `${vault.vault_name}: ${vault.last_cursor}/${vault.head_revision}`)
              .join(" · ")
          : "尚未同步任何仓库";
        const state = device.stale ? "已过期" : "有效";
        const lastSeen = device.last_seen_at
          ? new Date(device.last_seen_at).toLocaleString()
          : "未知";
        const setting = new Setting(devicesEl)
          .setName(`${device.name || "Unnamed device"}${device.is_current ? " (current)" : ""}`)
          .setDesc(`${state} · 最后在线 ${lastSeen} · ${cursorSummary}`)
          .addText((text) =>
            text.setValue(device.name).onChange((value) => {
              deviceName = value.trim();
            })
          )
          .addButton((button) =>
            button.setButtonText("Save").onClick(async () => {
              if (!deviceName) {
                new Notice("设备名称不能为空");
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
                new Notice("OSS: 设备重命名失败 " + this.errorMessage(error));
              }
            })
          );
        if (!device.is_current) {
          setting.addButton((button) =>
            button
              .setButtonText("Revoke")
              .setWarning()
              .onClick(async () => {
                try {
                  await this.plugin.api.revokeDevice(device.client_id);
                  new Notice(`OSS: 已吊销设备 ${device.name || device.client_id}`);
                  this.display();
                } catch (error: unknown) {
                  new Notice("OSS: 吊销设备失败 " + this.errorMessage(error));
                }
              })
          );
        }
      }
      if (result.devices.length === 0) {
        devicesEl.setText("No registered devices.");
      }
    }).catch((error: unknown) => {
      devicesEl.setText("Unable to load devices.");
      new Notice("OSS: 加载设备列表失败 " + this.errorMessage(error));
    });

    containerEl.createEl("h3", { text: "Sync" });

    new Setting(containerEl)
      .setName("Sync interval (seconds)")
      .setDesc("防抖时间，停顿多久后触发自动同步。默认 300 = 5 分钟")
      .addText((text) =>
        text
          .setPlaceholder("300")
          .setValue(String(this.plugin.settings.syncIntervalSec))
          .onChange(async (value) => {
            const n = parseInt(value, 10);
            if (!isNaN(n) && n >= 5) {
              this.plugin.settings.syncIntervalSec = n;
              await this.plugin.saveSettings();
              this.plugin.syncEngine.resetDebounce();
            }
          })
      );

    new Setting(containerEl)
      .setName("Max concurrency")
      .setDesc("同时上传和下载的任务上限。范围 1–10，默认 6。")
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
      .setName("Sync local .obsidian state")
      .setDesc("同步 workspace.json、cache 和插件 data.json 等本地状态。默认关闭。")
      .addToggle((toggle) =>
        toggle
          .setValue(this.plugin.settings.syncPoisonObsidianFiles)
          .onChange(async (value) => {
            this.plugin.settings.syncPoisonObsidianFiles = value;
            await this.plugin.saveSettings();
          })
      );

    new Setting(containerEl)
      .setName("Incremental check")
      .setDesc("日常同步只处理本地变更和服务端新增 revision。默认开启。")
      .addToggle((toggle) =>
        toggle
          .setValue(this.plugin.settings.incrementalCheck)
          .onChange(async (value) => {
            this.plugin.settings.incrementalCheck = value;
            await this.plugin.saveSettings();
          })
      );

    new Setting(containerEl)
      .setName("Remote poll interval (seconds)")
      .setDesc("没有本地修改时检查其他设备 revision 的间隔，最小 10 秒。")
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
      .setName("Force full sync now")
      .setDesc("立即执行一次完整清单校验。")
      .addButton((btn) =>
        btn.setButtonText("Sync now").onClick(async () => {
          const synced = await this.plugin.syncEngine.runOnce({ forceFull: true });
          if (!synced) new Notice("OSS: 同步未完成，请查看状态栏错误信息后重试");
        })
      );
  }

  private validateCredentials(): string | null {
    if (this.plugin.settings.username.trim().length < 3) {
      return "用户名至少需要 3 个字符";
    }
    if (this.plugin.settings.password.length < 8) {
      return "密码至少需要 8 个字符";
    }
    return null;
  }

  private errorMessage(error: unknown): string {
    return error instanceof Error ? error.message : "未知错误";
  }
}

/** 单次同步任务；forceFull 表示本次是否要求全量快照 */
export type SyncRunTask = (forceFull: boolean) => Promise<void>;

/** 同一时刻只执行一次同步；并发请求合并为下一轮，完整同步请求优先 */
export class SyncRunCoordinator {
  private activeRun: Promise<void> | null = null;
  private queued = false;
  private queuedForceFull = false;

  /** 已有同步在跑时登记合并请求并等待当前轮结束，不重复启动任务 */
  async run(forceFull: boolean, task: SyncRunTask): Promise<void> {
    if (this.activeRun) {
      this.queued = true;
      this.queuedForceFull ||= forceFull;
      await this.activeRun;
      return;
    }

    this.activeRun = this.drain(forceFull, task).finally(() => {
      this.activeRun = null;
      this.queued = false;
      this.queuedForceFull = false;
    });
    await this.activeRun;
  }

  // 排空合并队列；只要期间有新请求就再跑一轮，标记位在每轮开始前消费
  private async drain(forceFull: boolean, task: SyncRunTask): Promise<void> {
    let nextForceFull = forceFull;
    while (true) {
      await task(nextForceFull);
      if (!this.queued) return;
      nextForceFull = this.queuedForceFull;
      this.queued = false;
      this.queuedForceFull = false;
    }
  }
}

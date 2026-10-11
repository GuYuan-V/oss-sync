/** 串行执行协作同步，失败的任务不会阻断后续任务 */
export class CollaborationSyncCoordinator {
  private chain: Promise<void> = Promise.resolve();
  /** 追加到串行链尾；返回值是该任务自身的结果，失败不污染后续任务 */
  run(task: () => Promise<void>): Promise<void> {
    const next = this.chain.then(task, task);
    this.chain = next.catch(() => undefined);
    return next;
  }
}

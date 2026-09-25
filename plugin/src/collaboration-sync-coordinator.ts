/** 串行执行协作同步，失败的任务不会阻断后续任务 */
export class CollaborationSyncCoordinator {
  private chain: Promise<void> = Promise.resolve();
  run(task: () => Promise<void>): Promise<void> {
    const next = this.chain.then(task, task);
    this.chain = next.catch(() => undefined);
    return next;
  }
}

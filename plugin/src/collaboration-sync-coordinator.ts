export class CollaborationSyncCoordinator {
  private chain: Promise<void> = Promise.resolve();
  run(task: () => Promise<void>): Promise<void> {
    const next = this.chain.then(task, task);
    this.chain = next.catch(() => undefined);
    return next;
  }
}

/** 同一路径只允许一个冲突对话框进入打开流程 */
export class ConflictOpeningGuard {
  private readonly paths = new Set<string>();

  begin(path: string): boolean {
    if (this.paths.has(path)) return false;
    this.paths.add(path);
    return true;
  }

  end(path: string): void {
    this.paths.delete(path);
  }
}

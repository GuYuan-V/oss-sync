/** 同一路径只允许一个冲突对话框进入打开流程 */
export class ConflictOpeningGuard {
  private readonly paths = new Set<string>();

  /** 首次占用返回 true；重复 begin 返回 false，调用方应直接放弃 */
  begin(path: string): boolean {
    if (this.paths.has(path)) return false;
    this.paths.add(path);
    return true;
  }

  /** 结束标记必须与 begin 配对，否则该路径后续无法再打开 */
  end(path: string): void {
    this.paths.delete(path);
  }
}

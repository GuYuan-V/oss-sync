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

export function runGoldenCases(
  execute: (request: any) => Promise<any>,
  comparePaths?: (a: string, b: string) => number,
): Promise<void>;

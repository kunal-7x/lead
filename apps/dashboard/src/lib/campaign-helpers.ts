/** Helpers for campaign context array fields. */

export function splitLines(text: string): string[] {
  return text
    .split(/\r?\n|,/)
    .map((s) => s.trim())
    .filter(Boolean);
}

export function joinLines(arr: string[]): string {
  return arr.join('\n');
}

export function parseObjections(
  text: string,
): { objection: string; response: string }[] {
  return text
    .split(/\r?\n/)
    .map((line) => {
      const [objection, ...rest] = line.split('::');
      return { objection: (objection ?? '').trim(), response: rest.join('::').trim() };
    })
    .filter((item) => item.objection);
}

export function joinObjections(
  arr: { objection: string; response: string }[],
): string {
  return arr.map((item) => `${item.objection} :: ${item.response}`).join('\n');
}

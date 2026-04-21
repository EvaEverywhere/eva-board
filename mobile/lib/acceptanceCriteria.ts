// Parse acceptance-criteria checkboxes out of a markdown description.
//
// We treat any top-level GitHub-flavored task list line as an
// acceptance criterion. The regex is intentionally simple: a leading
// `-` or `*`, optional whitespace, `[ ]` or `[x]` (case-insensitive),
// and the rest of the line is the criterion text.

export type AcceptanceCriterion = {
  checked: boolean;
  text: string;
  line: number;
};

const TASK_LINE_RE = /^\s*[-*]\s+\[([ xX])\]\s+(.+?)\s*$/;

export function parseAcceptanceCriteria(markdown: string): AcceptanceCriterion[] {
  if (!markdown) {
    return [];
  }
  const out: AcceptanceCriterion[] = [];
  const lines = markdown.split(/\r?\n/);
  for (let i = 0; i < lines.length; i += 1) {
    const match = TASK_LINE_RE.exec(lines[i]);
    if (!match) {
      continue;
    }
    out.push({
      checked: match[1].toLowerCase() === "x",
      text: match[2],
      line: i,
    });
  }
  return out;
}

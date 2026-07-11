// editorSelection maps the saved editor command to a dropdown selection:
// the matching known command, or "custom" (with the raw value) otherwise.
export function editorSelection(
  cfgEditor: string,
  options: { command: string }[],
): { selected: string; customValue: string } {
  const editor = (cfgEditor ?? "").trim();
  const known = options.some((o) => o.command === editor);
  return { selected: editor && known ? editor : "custom", customValue: cfgEditor ?? "" };
}

import { describe, it, expect } from "vitest";
import { parseAction } from "./agentAction";

describe("parseAction", () => {
  it("splits an Edit into removed/added lines", () => {
    const a = parseAction("edit", "Edit", { file_path: "a/b.ts", old_string: "x\ny", new_string: "x\nz" });
    expect(a.kind).toBe("diff");
    if (a.kind === "diff") {
      expect(a.file).toBe("a/b.ts");
      expect(a.removed).toEqual(["x", "y"]);
      expect(a.added).toEqual(["x", "z"]);
    }
  });
  it("previews a Write (first lines)", () => {
    const a = parseAction("edit", "Write", { file_path: "n.txt", content: "l1\nl2" });
    expect(a.kind).toBe("write");
    if (a.kind === "write") expect(a.preview).toEqual(["l1", "l2"]);
  });
  it("returns the command for Bash", () => {
    const a = parseAction("remote", "Bash", { command: "git push origin feat/x" });
    expect(a).toEqual({ kind: "command", command: "git push origin feat/x" });
  });
  it("accepts a JSON string and never throws on garbage", () => {
    expect(parseAction("edit", "Edit", '{"file_path":"z","old_string":"","new_string":"q"}').kind).toBe("diff");
    expect(parseAction("edit", "Edit", "not json").kind).toBe("raw");
  });
  it("returns a string for undefined/non-serializable input", () => {
    const a = parseAction("edit", "Edit", undefined);
    expect(a.kind).toBe("raw");
    if (a.kind === "raw") {
      expect(typeof a.json).toBe("string");
      expect(a.json).toBe("undefined");
    }
  });
});

import { describe, it, expect } from "vitest";
import { renderBrief } from "./markdown";

describe("renderBrief", () => {
  it("escapes HTML so LLM/localStorage text can't inject markup (XSS-safe)", () => {
    const out = renderBrief('<script>alert(1)</script> & <b>x</b>');
    expect(out).not.toContain("<script>");
    expect(out).toContain("&lt;script&gt;");
    expect(out).toContain("&amp;");
    // the only tags present are our own
    expect(out).not.toContain("<b>x</b>");
  });

  it("renders bold, code and italic inline", () => {
    expect(renderBrief("do **X** now")).toContain("<strong>X</strong>");
    expect(renderBrief("use `go test`")).toContain("<code>go test</code>");
    expect(renderBrief("this is _important_")).toContain("<em>important</em>");
  });

  it("groups consecutive bullets into one list", () => {
    const out = renderBrief("- a\n- b\n- c");
    expect(out).toContain('<ul class="md-list">');
    expect((out.match(/<li>/g) || []).length).toBe(3);
    expect((out.match(/<ul/g) || []).length).toBe(1);
  });

  it("renders a heading and a paragraph", () => {
    const out = renderBrief("# Today\nwork on EMG");
    expect(out).toContain('<div class="md-h">Today</div>');
    expect(out).toContain("<p>work on EMG</p>");
  });

  it("handles empty / null input", () => {
    expect(renderBrief("")).toBe("");
    expect(renderBrief(null as unknown as string)).toBe("");
  });
});

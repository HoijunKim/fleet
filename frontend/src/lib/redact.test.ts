import { describe, it, expect } from "vitest";
import { redactSecrets } from "./redact";

describe("redactSecrets", () => {
  it("masks secret-ish assignments", () => {
    expect(redactSecrets('api_key = "abcdef123456"')).toContain("[redacted]");
    expect(redactSecrets("password: hunter2xyz")).toContain("[redacted]");
    expect(redactSecrets('api_key = "abcdef123456"')).not.toContain("abcdef123456");
  });

  it("masks known token shapes", () => {
    expect(redactSecrets("use sk-abcdefghij1234567890")).toContain("[redacted-secret]");
    expect(redactSecrets("token ghp_" + "a".repeat(30))).toContain("[redacted-secret]");
  });

  it("masks PEM private keys", () => {
    const pem = "-----BEGIN RSA PRIVATE KEY-----\nMIIabc\n-----END RSA PRIVATE KEY-----";
    expect(redactSecrets(pem)).toBe("[redacted-private-key]");
  });

  it("does NOT redact a 40-char commit hash or normal code", () => {
    const hash = "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0";
    expect(redactSecrets("fix in commit " + hash)).toContain(hash);
    expect(redactSecrets("const total = a + b;")).toBe("const total = a + b;");
  });

  it("handles empty input", () => {
    expect(redactSecrets("")).toBe("");
  });
});

// Mask obvious secrets in text (a diff) before it is sent to an AI provider.
// Conservative on purpose: only clear secret-ish assignments, known token
// shapes, and PEM private keys - never generic long strings (a 40-char commit
// hash or identifier must survive so the model keeps useful code context).

export function redactSecrets(text: string): string {
  if (!text) return text;
  let s = text;

  // key = value / key: value where the key name reads secret-ish
  s = s.replace(
    /((?:api[_-]?key|secret|token|password|passwd|client[_-]?secret|access[_-]?key|auth[_-]?token)\w*\s*[:=]\s*)(['"]?)[^\s'"]{6,}\2/gi,
    "$1$2[redacted]$2"
  );

  // known provider token shapes by prefix
  s = s.replace(
    /\b(sk-[A-Za-z0-9_-]{10,}|ghp_[A-Za-z0-9]{20,}|gho_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|AKIA[0-9A-Z]{12,}|AIza[0-9A-Za-z_-]{30,}|xox[baprs]-[A-Za-z0-9-]{10,})\b/g,
    "[redacted-secret]"
  );

  // PEM private key blocks
  s = s.replace(
    /-----BEGIN[^-]*PRIVATE KEY-----[\s\S]*?-----END[^-]*PRIVATE KEY-----/g,
    "[redacted-private-key]"
  );

  return s;
}

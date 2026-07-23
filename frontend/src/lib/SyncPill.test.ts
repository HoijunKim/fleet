import { describe, it, expect } from "vitest";
import { render as ssrRender } from "svelte/server";
import SyncPill from "./SyncPill.svelte";

function html(state: string): string {
  return ssrRender(SyncPill, { props: { state } }).body as string;
}

describe("SyncPill", () => {
  it("offers Retry only where retrying can help", () => {
    expect(html("error")).toContain("Retry");
    // Paused means the local data is not safe to push: retrying changes nothing
    // until the user resolves it, so offering the button would be a lie.
    expect(html("paused")).not.toContain("Retry");
  });

  it("says sync is paused rather than falling through to 'Sign in'", () => {
    const out = html("paused");
    expect(out).toContain("Sync paused");
    expect(out).not.toContain("Sign in"); // the user IS signed in
  });

  it("still renders the ordinary states", () => {
    expect(html("synced")).toContain("Synced");
    expect(html("offline")).toContain("Offline");
    expect(html("signedout")).toContain("Sign in to sync");
  });
});

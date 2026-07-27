// @vitest-environment happy-dom
//
// DOM test of the help overlay: it renders the feature sections and closes on
// the Close button and on Escape.
import { describe, it, expect, vi } from "vitest";
import { render, fireEvent } from "@testing-library/svelte";
import HelpOverlay from "./HelpOverlay.svelte";

describe("HelpOverlay (DOM)", () => {
  it("shows feature sections and closes via the button", async () => {
    const onClose = vi.fn();
    const { getByText, getByLabelText } = render(HelpOverlay, { props: { onClose } });
    // A few representative sections are present.
    getByText("Detail panel - git");
    getByText("Search & command palette");
    await fireEvent.click(getByLabelText("Close help"));
    expect(onClose).toHaveBeenCalled();
  });

  it("closes on Escape", async () => {
    const onClose = vi.fn();
    render(HelpOverlay, { props: { onClose } });
    await fireEvent.keyDown(window, { key: "Escape" });
    expect(onClose).toHaveBeenCalled();
  });
});

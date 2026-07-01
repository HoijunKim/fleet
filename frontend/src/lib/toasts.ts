import { writable } from "svelte/store";

export type ToastKind = "success" | "error" | "info";

export interface Toast {
  id: number;
  kind: ToastKind;
  message: string;
}

export const toasts = writable<Toast[]>([]);

let nextId = 1;

export function pushToast(message: string, kind: ToastKind = "info", ttl = 3000): number {
  const id = nextId++;
  toasts.update((all) => [...all, { id, kind, message }]);
  if (ttl > 0) {
    setTimeout(() => dismissToast(id), ttl);
  }
  return id;
}

export function dismissToast(id: number): void {
  toasts.update((all) => all.filter((t) => t.id !== id));
}

export function toastSuccess(message: string): number {
  return pushToast(message, "success", 3000);
}

export function toastError(message: string): number {
  return pushToast(message, "error", 5000);
}

export function toastInfo(message: string): number {
  return pushToast(message, "info", 3000);
}

/**
 * Wrap a Wails binding that returns "" on success or an error string.
 * Surfaces the outcome as a toast and returns true on success.
 */
export async function runAction(label: string, fn: () => Promise<string>): Promise<boolean> {
  try {
    const err = await fn();
    if (err) {
      toastError(label + ": " + err);
      return false;
    }
    toastSuccess(label);
    return true;
  } catch (e) {
    toastError(label + ": " + String(e));
    return false;
  }
}

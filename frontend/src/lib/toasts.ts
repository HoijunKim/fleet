import { writable } from "svelte/store";

export type ToastKind = "success" | "error" | "info";

export interface ToastAction {
  label: string;
  run: () => void;
}

export interface Toast {
  id: number;
  kind: ToastKind;
  message: string;
  action?: ToastAction;
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

// A toast that names something the user may need to act on, and so does not
// expire on its own: it stays until dismissed. Used where the alternative is a
// 5-second window onto a file the app never otherwise discloses.
export function toastAction(message: string, kind: ToastKind, action: ToastAction): number {
  const id = nextId++;
  toasts.update((all) => [...all, { id, kind, message, action }]);
  return id;
}

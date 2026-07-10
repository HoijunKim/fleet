// Polyfill window for node environment
if (typeof window === 'undefined') {
  (global as any).window = global;
}

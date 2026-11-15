// Minimal types for node:async_hooks (workerd provides it under the
// nodejs_als compatibility flag, which twd sets for tenant code).
declare module "node:async_hooks" {
  export class AsyncLocalStorage<T> {
    getStore(): T | undefined;
    run<R>(store: T | undefined, fn: () => R): R;
  }
}

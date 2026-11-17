# create-tailworks

Create a [tailworks](https://github.com/rosskukulinski/tailworks) app from a
template:

```sh
npm create tailworks@latest my-app -- --template hello
cd my-app && npm install && npx tsc --noEmit
```

Templates: `hello`, `agent`, `mcp`, `ai-agent`, `lock`, `refund-approval`,
`node-workflow`. With the `tw` CLI on your PATH this runs `tw init`;
otherwise it copies the template.

Each template has a `tsconfig.json` (`@cloudflare/workers-types` plus
`@tailworks/tailnet`) and a `worker-configuration.d.ts` with the global
`Env` that `tw types` generates from `wrangler.jsonc` and `tailworks.jsonc`.
Run `tw types` again after you change either.

`create-tailworks build [dir]` and `create-tailworks build-node [dir]` are
the bundlers `tw deploy` uses.

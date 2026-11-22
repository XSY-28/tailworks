# noble-pug: README quick start uses `npm create tailworks`

`create-tailworks` and the `@tailworks/*` packages are on npm (0.3.0), so
the README quick start now starts from npm:

- "Only want to see the code first?" is `npm create tailworks@latest hello`
  then `npm install && npx tsc --noEmit`. It needs only Node.js ≥ 20, with
  no tailworks install. Running it (`npm run dev`, i.e. `tw dev`) still
  needs step 1.
- Step 4 scaffolds with `npm create tailworks@latest support -- --template
  agent` and runs `npm install`, so editor types work. With `tw` on PATH
  the bin runs `tw init`, so tags are still named after the app. The text
  says this and that `tw init` works directly.
- Step 1 (get.sh tarball) is unchanged: the `tw` CLI itself is not on npm.

## Tests

- Fresh temp dir, no `tw` on PATH: `npm create tailworks@latest hello`
  then `npm install && npx tsc --noEmit`: pass.
- `make changes-check`: ok (no fragment needed).
- `make lint test`: pass.

#!/usr/bin/env node
// create-tailworks: scaffold a tailworks app from a template, or bundle one.
//   npm create tailworks@latest <dir> -- --template hello|agent|mcp|ai-agent
//   create-tailworks build [dir]        (bundles wrangler.jsonc main → dist/index.js)
//   create-tailworks build-node [dir] [outdir]  (runtime "node" apps: dist/server.mjs + Workflow bundles)
import { cpSync, existsSync, readdirSync, renameSync } from "node:fs";
import { spawnSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const templates = path.join(here, "..", "templates");
const args = process.argv.slice(2);

if (args[0] === "build") {
  const { buildApp } = await import("../lib/build.mjs");
  const r = await buildApp(path.resolve(args[1] ?? "."));
  console.log(`built ${r.outfile} (${r.bytes} bytes)`);
  process.exit(0);
}

if (args[0] === "build-node") {
  const { buildNodeApp } = await import("../lib/build-node.mjs");
  const dir = path.resolve(args[1] ?? ".");
  const r = await buildNodeApp(dir, path.resolve(args[2] ?? path.join(dir, ".tailworks-build")));
  console.log(`built ${r.outdir}/${r.main} (${r.bytes} bytes${r.workflows ? ", with Vercel Workflow bundles" : ""})`);
  process.exit(0);
}

const ti = args.indexOf("--template");
const template = ti >= 0 ? args[ti + 1] : "hello";
const dir = args.find((a, i) => !a.startsWith("--") && i !== ti + 1) ?? template;
const available = readdirSync(templates);
if (!available.includes(template)) {
  console.error(`create-tailworks: unknown template "${template}" (available: ${available.join(", ")})`);
  process.exit(2);
}
if (existsSync(dir) && readdirSync(dir).length) {
  console.error(`create-tailworks: ${dir} exists and is not empty`);
  process.exit(2);
}
// `npm create tailworks`: with the tailworks CLI installed (the release
// tarball, or npm install -g tailworks), `tw init` does it (it also names the
// workload and its tags after the directory); without it, copy the template
// as it is, and point at `npx tailworks`.
const tw = spawnSync("tw", ["init", dir, "--template", template], { stdio: "inherit" });
if (!tw.error) process.exit(tw.status ?? 1);
cpSync(path.join(templates, template), dir, { recursive: true });
// npm drops .gitignore from published packages: templates ship "gitignore".
if (existsSync(path.join(dir, "gitignore"))) renameSync(path.join(dir, "gitignore"), path.join(dir, ".gitignore"));
console.log(`created ${dir} from template ${template}\n  cd ${dir} && npm install && npx tsc --noEmit\n  then run it locally: npx tailworks dev   (or npm install -g tailworks; docs: https://github.com/rosskukulinski/tailworks/blob/master/docs/INSTALL.md)`);

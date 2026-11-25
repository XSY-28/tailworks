import { existsSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { stripJSONC } from "./build.mjs";

// Match workloadLabel in cmd/tw/app.go. Go's simple case mapping turns
// U+0130 into i without the combining dot that JavaScript adds.
export function workloadLabel(name) {
  return name.replaceAll("\u0130", "i").toLowerCase()
    .replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 40).replace(/-$/, "");
}

// Match appTags in cmd/tw/inittags.go, keeping references and comments in
// the JSONC source in sync without reformatting the template.
function renameTemplateTags(source, workload) {
  const { tailnet = {} } = JSON.parse(stripJSONC(source));
  const names = new Map();
  const rename = (tag, label) => {
    if (tag.startsWith("tag:tw-") && !names.has(tag)) names.set(tag, label);
  };
  for (const tag of tailnet.tags ?? []) rename(tag, `tag:tw-wl-${workload}`);
  const objects = tailnet.objects ?? {};
  for (const name of Object.keys(objects).sort()) {
    const label = workloadLabel(name.replace(/([a-z0-9])([A-Z])/g, "$1-$2"));
    for (const tag of objects[name].tags ?? []) rename(tag, `tag:tw-${workload}-${label}`);
  }
  return source.replace(/"(tag:tw-[a-zA-Z0-9._-]+)"/g, (quoted, tag) =>
    names.has(tag) ? JSON.stringify(names.get(tag)) : quoted);
}

export function renameScaffold(dir, workload) {
  for (const name of ["package.json", "tailworks.jsonc", "wrangler.jsonc"]) {
    const file = path.join(dir, name);
    if (!existsSync(file)) continue;
    let source = readFileSync(file, "utf8");
    source = source.replace(/("name"\s*:\s*)"[^"]*"/, `$1"${workload}"`);
    if (name === "tailworks.jsonc") source = renameTemplateTags(source, workload);
    if (name === "wrangler.jsonc") {
      source = source.replace(/("APP_NAME"\s*:\s*)"[^"]*"/g, `$1"${workload}"`);
    }
    writeFileSync(file, source);
  }
}

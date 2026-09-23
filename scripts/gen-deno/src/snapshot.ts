// Strips doc comments and source locations from `deno doc --json` output so
// the snapshot stays small enough to commit.
//
// Usage: node src/snapshot.ts <input.json> <output.json>
// Typically run via `pnpm run snapshot`, which produces deno-doc.json from
// `deno types` + `deno doc --json` (requires the deno binary).

import { readFile, writeFile } from "node:fs/promises";

const [input, output] = process.argv.slice(2);
if (!input || !output) {
	console.error("usage: node src/snapshot.ts <input.json> <output.json>");
	process.exit(1);
}

function strip(x: unknown) {
	if (!x || typeof x !== "object") return;
	if (Array.isArray(x)) {
		for (const i of x) strip(i);
		return;
	}
	const o = x as Record<string, unknown>;
	delete o.jsDoc;
	delete o.location;
	for (const k in o) strip(o[k]);
}

const doc = JSON.parse(await readFile(input, "utf8"));
strip(doc);
await writeFile(output, JSON.stringify(doc));
console.log(`wrote ${output}`);

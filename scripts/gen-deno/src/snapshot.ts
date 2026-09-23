// Refreshes the Deno API snapshot consumed by the generator: runs
// `deno types` and `deno doc --json`, then strips source locations and jsDoc
// tags (jsDoc.doc is kept because the generator emits it as Go doc comments).
// Writes deno.d.ts and deno-doc.json next to this package.
//
// Requires the deno binary. Run via `pnpm run snapshot`.
//
// `deno doc` runs in a temporary directory on purpose: when the current
// working directory contains node_modules, deno resolves ambient @types
// packages and the doc output becomes dependent on installed dependencies.

import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const pkgDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const tmp = mkdtempSync(path.join(tmpdir(), "gen-deno-snapshot-"));
try {
	const typesPath = path.join(tmp, "deno.d.ts");
	const types = execFileSync("deno", ["types"], { encoding: "utf8" });
	await writeFile(typesPath, types);
	// Keep a copy in the package for reference and diffing on upgrades.
	await writeFile(path.join(pkgDir, "deno.d.ts"), types);

	const raw = execFileSync("deno", ["doc", "--json", typesPath], {
		encoding: "utf8",
		cwd: tmp,
		maxBuffer: 64 * 1024 * 1024,
	});
	const doc = JSON.parse(raw);
	strip(doc);
	// `nodes` is keyed by module file URL; rewrite keys to a stable value so
	// the snapshot doesn't change on every run.
	if (doc.nodes && typeof doc.nodes === "object") {
		const nodes: Record<string, unknown> = {};
		for (const [k, v] of Object.entries(doc.nodes)) {
			nodes[path.basename(new URL(k).pathname)] = v;
		}
		doc.nodes = nodes;
	}
	const out = path.join(pkgDir, "deno-doc.json");
	await writeFile(out, JSON.stringify(doc));
	console.log(`wrote ${out}`);
} finally {
	rmSync(tmp, { recursive: true, force: true });
}

function strip(x: unknown) {
	if (!x || typeof x !== "object") return;
	if (Array.isArray(x)) {
		for (const i of x) strip(i);
		return;
	}
	const o = x as Record<string, unknown>;
	delete o.location;
	const jsDoc = o.jsDoc;
	if (jsDoc && typeof jsDoc === "object") {
		delete (jsDoc as Record<string, unknown>).tags;
	}
	for (const k in o) strip(o[k]);
}

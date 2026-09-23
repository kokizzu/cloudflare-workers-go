# gen-deno

Generates the Go bindings in `exp/deno` for the Deno runtime APIs (the `Deno`
global namespace) from `deno doc --json` output.

## Generate bindings

```console
pnpm install
pnpm run gen
```

Requires only Node.js (>= 24, native TypeScript execution) and `gofmt`.
The generator reads the committed API snapshot `deno-doc.json`, so the `deno`
binary is not needed for code generation.

Options (`node src/index.ts`):

* `--config <path>` — roots config (default: `gen.json` in this package)
* `--doc <path>` — deno doc JSON path (default: `deno-doc.json`)
* `--types <path>` — deno.d.ts path; runs `deno doc --json` on it (requires deno)
* `--out <dir>` — output directory (default: `<repo>/exp/deno`)

## Refresh the API snapshot

```console
pnpm run snapshot
```

Requires the `deno` binary. It runs `deno types` + `deno doc --json`, strips
doc comments and source locations, and rewrites `deno.d.ts` and
`deno-doc.json`. Commit both files together with any regenerated bindings.

## Files

* `gen.json` — the `roots` list: Deno namespace members whose transitive type
  dependencies are turned into bindings
* `deno.d.ts` — `deno types` output the snapshot was built from
* `deno-doc.json` — stripped `deno doc --json` snapshot consumed by the
  generator
* `src/index.ts` — the generator
* `src/snapshot.ts` — snapshot post-processor

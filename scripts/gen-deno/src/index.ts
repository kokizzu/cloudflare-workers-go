// Generates Go bindings for the Deno runtime APIs (the `Deno` global
// namespace) from `deno doc --json` output into the exp/deno package.
//
// Usage:
//   node src/index.ts [options]
//
// Options:
//   --config <path>  gen.json path (default: gen.json in this package)
//   --doc <path>     deno doc JSON path (default: deno-doc.json snapshot in
//                    this package; refresh it with `pnpm run snapshot`,
//                    which requires the deno binary)
//   --types <path>   deno.d.ts path; when given, `deno doc --json` is invoked
//                    on it (requires the deno binary)
//   --out <dir>      output directory (default: <repo>/exp/deno)

import { execFileSync } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

type Doc = any;

// ---------------- CLI / IO ----------------

function parseArgs(argv: string[]): Record<string, string> {
  const args: Record<string, string> = {};
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (!a.startsWith("--")) continue;
    const eq = a.indexOf("=");
    if (eq >= 0) {
      args[a.slice(2, eq)] = a.slice(eq + 1);
    } else if (i + 1 < argv.length && !argv[i + 1].startsWith("--")) {
      args[a.slice(2)] = argv[++i];
    } else {
      args[a.slice(2)] = "true";
    }
  }
  return args;
}

function capture(cmd: string[]): string {
  return execFileSync(cmd[0], cmd.slice(1), {
    encoding: "utf8",
    // `deno doc --json` output is several MB.
    maxBuffer: 64 * 1024 * 1024,
  });
}

// ---------------- naming ----------------

const GO_KEYWORDS = new Set([
  "break", "case", "chan", "const", "continue", "default", "defer", "else",
  "fallthrough", "for", "func", "go", "goto", "if", "import", "interface",
  "map", "package", "range", "return", "select", "struct", "switch", "type",
  "var",
]);

function pascal(s: string): string {
  const parts = s.split(/[^a-zA-Z0-9]+/).filter(Boolean);
  let r = parts.map((w) => w[0].toUpperCase() + w.slice(1)).join("");
  if (!r) r = "X";
  if (/^[0-9]/.test(r)) r = "N" + r;
  return r;
}

function safeIdent(s: string): string {
  s = s.replace(/[^a-zA-Z0-9_]/g, "_");
  if (/^[0-9]/.test(s)) s = "_" + s;
  if (GO_KEYWORDS.has(s)) s += "_";
  return s;
}

const lastSeg = (n: string) => n.split(".").pop()!;

// fromJSName returns the unexported converter function generated for a type
// (e.g. KvListEntry -> kvListEntryFromJS), matching the unexported toJS()
// methods so generated internals stay out of the public API surface.
function fromJSName(name: string): string {
  return name[0].toLowerCase() + name.slice(1) + "FromJS";
}

// ---------------- doc comments ----------------

const DOC_BASE = "https://docs.deno.com/api/deno/~/";

// emitDoc writes jsDoc text as `//` comment lines (indent for fields).
function emitDoc(doc: string | undefined, out: string[], indent = "") {
  if (typeof doc !== "string" || doc.trim() === "") return;
  for (const line of doc.trimEnd().split("\n")) {
    const t = line.trimEnd();
    out.push(indent + (t === "" ? "//" : "// " + t));
  }
}

// emitDocLink appends a Deno API reference link, in the same style as the
// cloudflare bindings' workers-types links.
function emitDocLink(symbol: string, out: string[], indent = "") {
  out.push(`${indent}//   - ${DOC_BASE}${symbol}`);
}

// ---------------- type conversion model ----------------

interface Conv {
  go: string; // Go type expression
  fromJS: (v: string) => string; // js.Value expr -> Go expr
  toJS: (x: string) => string; // Go expr -> js.Value expr
  fromJSFunc?: string; // identifier usable as func(js.Value) T
  nilable: boolean; // zero value can mean "absent"
  scalar?: boolean; // string/float64/bool or string-alias
  inline?: boolean; // generated struct for an anonymous TS object shape
}

const stringConv: Conv = {
  go: "string",
  fromJS: (v) => `getString(${v})`,
  toJS: (x) => `js.ValueOf(${x})`,
  fromJSFunc: "getString",
  nilable: false,
  scalar: true,
};
const floatConv: Conv = {
  go: "float64",
  fromJS: (v) => `getFloat(${v})`,
  toJS: (x) => `js.ValueOf(${x})`,
  fromJSFunc: "getFloat",
  nilable: false,
  scalar: true,
};
const boolConv: Conv = {
  go: "bool",
  fromJS: (v) => `getBool(${v})`,
  toJS: (x) => `js.ValueOf(${x})`,
  fromJSFunc: "getBool",
  nilable: false,
  scalar: true,
};
const anyConv: Conv = {
  go: "any",
  fromJS: (v) => `jsToAny(${v})`,
  toJS: (x) => `anyToJS(${x})`,
  fromJSFunc: "jsToAny",
  nilable: true,
};
const jsValueConv: Conv = {
  go: "js.Value",
  fromJS: (v) => v,
  toJS: (x) => x,
  nilable: true,
};
const bigIntConv: Conv = {
  go: "uint64",
  fromJS: (v) => `bigIntToUint64(${v})`,
  toJS: (x) => `BigInt(${x})`,
  fromJSFunc: "bigIntToUint64",
  nilable: false,
  scalar: true,
};
const bytesConv: Conv = {
  go: "[]byte",
  fromJS: (v) => `uint8ArrayToBytes(${v})`,
  toJS: (x) => `bytesToUint8Array(${x})`,
  fromJSFunc: "uint8ArrayToBytes",
  nilable: true,
};
const timeConv: Conv = {
  go: "time.Time",
  fromJS: (v) => `dateToTime(${v})`,
  toJS: (x) => `jsutil.TimeToDate(${x})`,
  nilable: false,
};
const strMapConv: Conv = {
  go: "map[string]string",
  fromJS: (v) => `jsutil.StrRecordToMap(${v})`,
  toJS: (x) => `strMapToJS(${x})`,
  nilable: true,
};

function sliceConv(elem: Conv): Conv {
  return {
    go: `[]${elem.go}`,
    fromJS: (v) => `sliceFromJS(${v}, ${fromJSFunc(elem)})`,
    toJS: (x) => `sliceToJS(${x}, ${toJSFunc(elem)})`,
    nilable: true,
  };
}

function ptrConv(base: Conv): Conv {
  return {
    go: `*${base.go}`,
    fromJS: (v) => `ptrFromJS(${v}, ${fromJSFunc(base)})`,
    toJS: (x) => `ptrToJS(${x}, ${toJSFunc(base)})`,
    nilable: true,
  };
}

function fromJSFunc(c: Conv): string {
  return c.fromJSFunc ??
    `func(e js.Value) ${c.go} { return ${c.fromJS("e")} }`;
}

function toJSFunc(c: Conv): string {
  return `func(e ${c.go}) js.Value { return ${c.toJS("e")} }`;
}

function zeroOf(go: string): string {
  if (go === "string") return `""`;
  if (go === "float64" || go === "int") return "0";
  if (go === "bool") return "false";
  if (
    go === "any" || go.startsWith("*") || go.startsWith("[]") ||
    go.startsWith("map[")
  ) return "nil";
  return `${go}{}`;
}

// ---------------- doc model ----------------

const cliArgs = parseArgs(process.argv.slice(2));
const pkgDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = path.resolve(pkgDir, "../..");
const configPath = cliArgs.config ?? path.join(pkgDir, "gen.json");
const outDir = cliArgs.out ?? path.join(repoRoot, "exp/deno");

async function loadDocText(): Promise<string> {
  if (cliArgs.doc) return await readFile(cliArgs.doc, "utf8");
  if (cliArgs.types) {
    // Requires the deno binary; normally the committed snapshot is used
    // instead (see `pnpm run snapshot`).
    return capture(["deno", "doc", "--json", cliArgs.types]);
  }
  return await readFile(path.join(pkgDir, "deno-doc.json"), "utf8");
}

const doc: Doc = JSON.parse(await loadDocText());
const config = JSON.parse(await readFile(configPath, "utf8"));

// ---------------- doc JSON normalization ----------------
//
// `deno doc --json` output shape changed across Deno versions. Older
// versions emit `nodes` as a flat array of `{kind, name, <kind>Def}` nodes
// with `tsType` subtrees of `{kind, <kind>: payload}`. Newer versions emit
// `nodes` as a map of module URL -> `{symbols}` where each symbol has
// `declarations: [{kind, def, jsDoc}]` and tsType subtrees of
// `{kind, repr, value}`. norm* functions below convert the new schema into
// the old one, which the rest of the generator operates on.

function normTsType(t: Doc): Doc {
  if (!t || typeof t !== "object") return t;
  if (t.value === undefined && t.typeRef !== undefined) return t; // already normalized
  const v = t.value;
  const r = t.repr;
  switch (t.kind) {
    case "keyword":
      return { kind: "keyword", keyword: v, repr: r };
    case "literal":
      return { kind: "literal", literal: v, repr: r };
    case "typeRef":
      return {
        kind: "typeRef",
        repr: r,
        typeRef: {
          typeName: v?.typeName,
          typeParams: v?.typeParams?.map(normTsType),
        },
      };
    case "union":
      return { kind: "union", union: v?.map(normTsType), repr: r };
    case "intersection":
      return { kind: "intersection", intersection: v?.map(normTsType), repr: r };
    case "array":
      return { kind: "array", array: normTsType(v), repr: r };
    case "tuple":
      return { kind: "tuple", tuple: v?.map(normTsType), repr: r };
    case "rest":
      return { kind: "rest", rest: normTsType(v), repr: r };
    case "parenthesized":
      return { kind: "parenthesized", parenthesized: normTsType(v), repr: r };
    case "optional":
      return { kind: "optional", optional: normTsType(v), repr: r };
    case "typeOperator":
      return {
        kind: "typeOperator",
        repr: r,
        typeOperator: { operator: v?.operator, tsType: normTsType(v?.tsType) },
      };
    case "mapped":
      return {
        kind: "mapped",
        repr: r,
        mappedType: {
          tsType: normTsType(v?.tsType),
          typeParam: {
            name: v?.typeParam?.name,
            constraint: normTsType(v?.typeParam?.constraint),
          },
        },
      };
    case "typeLiteral":
      return { kind: "typeLiteral", repr: r, typeLiteral: normMembers(v) };
    case "this":
      return { kind: "this", repr: r };
    default:
      // conditional, indexedAccess, infer, typeQuery, template, importType,
      // fnOrConstructor, ... -> mapped to any/js.Value by mapType anyway.
      return t;
  }
}

function normParam(p: Doc): Doc {
  if (p.kind === "rest") {
    return {
      kind: "rest",
      arg: p.arg ? { name: p.arg.name } : undefined,
      tsType: normTsType(p.tsType),
    };
  }
  return {
    kind: p.kind,
    name: p.name,
    optional: p.optional,
    tsType: normTsType(p.tsType),
    left: p.left ? normParam(p.left) : undefined,
  };
}

function normFnDef(fd: Doc): Doc {
  if (!fd) return fd;
  return {
    params: (fd.params ?? []).map(normParam),
    returnType: normTsType(fd.returnType),
    typeParams: fd.typeParams,
  };
}

function normMembers(def: Doc): Doc {
  if (!def) return def;
  return {
    constructors: (def.constructors ?? []).map((c: Doc) => ({
      params: (c.params ?? []).map(normParam),
      jsDoc: c.jsDoc,
    })),
    properties: (def.properties ?? []).map((p: Doc) => ({
      ...p,
      tsType: normTsType(p.tsType),
    })),
    methods: (def.methods ?? []).map((m: Doc) => ({
      ...m,
      functionDef: m.functionDef ? normFnDef(m.functionDef) : undefined,
      params: m.params?.map(normParam),
      returnType: m.returnType ? normTsType(m.returnType) : undefined,
    })),
    indexSignatures: (def.indexSignatures ?? []).map((s: Doc) => ({
      ...s,
      params: (s.params ?? []).map(normParam),
      tsType: normTsType(s.tsType),
    })),
    callSignatures: (def.callSignatures ?? []).map((s: Doc) => ({
      ...s,
      params: (s.params ?? []).map(normParam),
      tsType: normTsType(s.tsType),
      returnType: normTsType(s.returnType),
    })),
    typeParams: def.typeParams,
    extends: def.extends,
  };
}

function normElement(el: Doc): Doc | null {
  // Old-schema nodes already carry <kind>Def; pass them through.
  if (
    el.namespaceDef || el.classDef || el.interfaceDef || el.typeAliasDef ||
    el.enumDef || el.variableDef || el.functionDef
  ) {
    return el;
  }
  const decls: Doc[] = el.declarations ?? [el];
  const kindRank: Record<string, number> = {
    class: 5,
    interface: 4,
    typeAlias: 3,
    enum: 3,
    variable: 2,
    function: 1,
    namespace: 0,
  };
  let best: Doc | null = null;
  for (const d of decls) {
    if (!(d.kind in kindRank)) continue;
    if (!best) {
      best = d;
      continue;
    }
    if (d.kind === "function" && best.kind === "function") {
      if ((d.def?.params?.length ?? 0) > (best.def?.params?.length ?? 0)) {
        best = d;
      }
    } else if (kindRank[d.kind] > kindRank[best.kind]) {
      best = d;
    }
  }
  if (!best) return null;
  const def = best.def;
  const out: Doc = {
    kind: best.kind,
    name: el.name,
    jsDoc: best.jsDoc ?? el.jsDoc,
  };
  switch (best.kind) {
    case "namespace":
      out.namespaceDef = {
        elements: (def?.elements ?? []).map(normElement).filter(Boolean),
      };
      break;
    case "class":
      out.classDef = normMembers(def);
      break;
    case "interface":
      out.interfaceDef = normMembers(def);
      break;
    case "typeAlias":
      out.typeAliasDef = {
        tsType: normTsType(def?.tsType),
        typeParams: def?.typeParams,
      };
      break;
    case "enum":
      out.enumDef = {
        members: (def?.members ?? []).map((m: Doc) => ({
          ...m,
          init: normTsType(m.init),
        })),
      };
      break;
    case "variable":
      out.variableDef = { tsType: normTsType(def?.tsType), kind: def?.kind };
      break;
    case "function":
      out.functionDef = normFnDef(def);
      break;
    default:
      return null;
  }
  return out;
}

// `nodes` is either a flat array (old schema) or a map of
// module URL -> { symbols } (new schema). Flatten to a symbol list.
const docNodes: Doc[] = Array.isArray(doc.nodes)
  ? doc.nodes
  : Object.values(doc.nodes).flatMap((m: Doc) => m.symbols ?? m);
const denoSym = docNodes.find(
  (n: Doc) => n.name === "Deno" &&
    (n.kind === "namespace" ||
      n.declarations?.some((d: Doc) => d.kind === "namespace")),
);
const nsNode = denoSym ? normElement(denoSym) : null;
if (!nsNode) throw new Error("Deno namespace not found in doc JSON");

// elements map: name -> node. Overloads (functions with the same name) keep
// the variant with the most parameters.
const elements = new Map<string, Doc>();
for (const e of nsNode.namespaceDef.elements) {
  if (e.kind === "namespace" || e.kind === "import") continue;
  if (typeof e.name !== "string" || e.name.startsWith("[")) continue;
  const prev = elements.get(e.name);
  if (prev) {
    const np = e.functionDef?.params?.length ?? 0;
    const pp = prev.functionDef?.params?.length ?? 0;
    if (np > pp) elements.set(e.name, e);
    continue;
  }
  elements.set(e.name, e);
}

function collectTypeNames(x: Doc, out: Set<string>) {
  if (!x || typeof x !== "object") return;
  if (Array.isArray(x)) {
    for (const i of x) collectTypeNames(i, out);
    return;
  }
  for (const k in x) {
    if (k === "location" || k === "jsDoc" || k === "repr") continue;
    if (k === "typeName" && typeof x[k] === "string") {
      out.add(lastSeg(x[k]));
      continue;
    }
    collectTypeNames(x[k], out);
  }
}

// dependency closure from roots
const reached = new Set<string>();
{
  const queue = [...config.roots];
  while (queue.length) {
    const n = queue.pop()!;
    if (reached.has(n)) continue;
    const el = elements.get(n);
    if (!el) {
      console.error(`warn: root "${n}" not found in Deno namespace`);
      continue;
    }
    reached.add(n);
    const names = new Set<string>();
    collectTypeNames(el, names);
    for (const m of names) {
      if (!reached.has(m) && elements.has(m)) queue.push(m);
    }
  }
}

// ---------------- classification ----------------

type ElemClass =
  | "wrapper"
  | "struct"
  | "stralias"
  | "alias"
  | "merged"
  | "enum"
  | "variable"
  | "function";

const elemClass = new Map<string, ElemClass>();

function typeParamsOf(el: Doc): Set<string> {
  const tp = el.classDef?.typeParams ?? el.interfaceDef?.typeParams ??
    el.typeAliasDef?.typeParams ?? [];
  return new Set(tp.map((p: Doc) => p.name));
}

function isNullishType(t: Doc): boolean {
  if (!t) return false;
  return (t.kind === "keyword" &&
    (t.keyword === "null" || t.keyword === "undefined" ||
      t.keyword === "void")) ||
    (t.kind === "literal" && t.literal?.kind === "null");
}

// propMembers returns alternative property sets for prop-resolvable types
// (typeLiteral, prop-only interface, merged alias, parenthesized, union and
// intersection of those). Returns null when the type is not resolvable.
function propMembers(t: Doc, seen: Set<string>): Map<string, Doc>[] | null {
  if (!t) return null;
  switch (t.kind) {
    case "parenthesized":
      return propMembers(t.parenthesized, seen);
    case "typeLiteral": {
      const tl = t.typeLiteral;
      if ((tl.methods?.length ?? 0) > 0 || (tl.callSignatures?.length ?? 0) > 0 ||
        (tl.indexSignatures?.length ?? 0) > 0) return null;
      const m = new Map<string, Doc>();
      for (const p of tl.properties ?? []) {
        if (typeof p.name !== "string" || p.computed === true ||
          p.name.startsWith("[")) continue;
        m.set(p.name, {
          tsType: p.tsType,
          optional: p.optional,
          jsDoc: p.jsDoc,
        });
      }
      return [m];
    }
    case "typeRef": {
      const name = lastSeg(t.typeRef.typeName);
      if (seen.has(name)) return null;
      const el = elements.get(name);
      if (!el) return null;
      seen.add(name);
      let r: Map<string, Doc>[] | null = null;
      if (el.kind === "interface" && isPropOnlyInterface(el)) {
        r = interfaceProps(el);
      } else if (el.kind === "typeAlias") {
        r = propMembers(el.typeAliasDef.tsType, seen);
      }
      seen.delete(name);
      return r;
    }
    case "union": {
      const all: Map<string, Doc>[] = [];
      for (const m of t.union ?? []) {
        const r = propMembers(m, seen);
        if (!r) return null;
        all.push(...r);
      }
      return all;
    }
    case "intersection": {
      // A union nested inside an intersection expands to a cartesian
      // product of member sets: A & (B | C) => A&B | A&C.
      let acc: Map<string, Doc>[] = [new Map()];
      for (const m of t.intersection) {
        const r = propMembers(m, seen);
        if (!r) return null;
        const next: Map<string, Doc>[] = [];
        for (const a of acc) {
          for (const b of r) {
            const merged = new Map(a);
            for (const [k, v] of b) merged.set(k, v);
            next.push(merged);
          }
        }
        acc = next;
      }
      return acc;
    }
    default:
      return null;
  }
}

function interfaceProps(el: Doc): Map<string, Doc>[] {
  const m = new Map<string, Doc>();
  for (const p of el.interfaceDef.properties ?? []) {
    if (typeof p.name !== "string" || p.computed === true ||
      p.name.startsWith("[")) continue;
    m.set(p.name, {
      tsType: p.tsType,
      optional: p.optional,
      jsDoc: p.jsDoc,
    });
  }
  return [m];
}

function isPropOnlyInterface(el: Doc): boolean {
  const d = el.interfaceDef;
  return (d.methods?.length ?? 0) === 0 &&
    (d.callSignatures?.length ?? 0) === 0;
}

function isStringLiteralUnion(t: Doc): boolean {
  if (t.kind === "literal") return t.literal?.kind === "string";
  if (t.kind !== "union") return false;
  return t.union.every((m: Doc) =>
    m.kind === "literal" && m.literal?.kind === "string"
  );
}

function classify(el: Doc): ElemClass {
  switch (el.kind) {
    case "class":
      return "wrapper";
    case "interface":
      return isPropOnlyInterface(el) ? "struct" : "wrapper";
    case "typeAlias": {
      const t = el.typeAliasDef.tsType;
      if (isStringLiteralUnion(t)) return "stralias";
      if (propMembers(t, new Set([el.name]))) return "merged";
      return "alias";
    }
    case "enum":
      return "enum";
    case "variable":
      return "variable";
    case "function":
      return "function";
    default:
      return "alias";
  }
}

for (const name of reached) {
  elemClass.set(name, classify(elements.get(name)));
}

// ---------------- inline struct registry ----------------

interface Field {
  goName: string;
  jsonName: string;
  conv: Conv;
  optional: boolean;
  doc?: string;
}

interface StructDecl {
  name: string;
  fields: Field[];
  doc?: string;
  inline?: boolean; // generated for an anonymous TS object shape
}

const structDecls: StructDecl[] = [];
const structKeys = new Map<string, string>(); // normalized key -> name
let anonCounter = 0;

interface Ctx {
  owner: string; // enclosing class/func name for inline naming
  typeParams: Set<string>;
}

function structConv(name: string, inline = false): Conv {
  return {
    go: name,
    fromJS: (v) => `${fromJSName(name)}(${v})`,
    toJS: (x) => `${x}.toJS()`,
    fromJSFunc: fromJSName(name),
    nilable: false,
    inline,
  };
}

// pickFieldType chooses the most informative tsType among union alternatives
// (prefers non-null members).
function pickFieldType(candidates: Doc[]): Doc {
  return candidates.find((c) => !isNullishType(c.tsType)) ?? candidates[0];
}

function registerStruct(
  preferred: string,
  members: Map<string, Doc>[],
  ctx: Ctx,
  selfName = false,
  doc?: string,
): string {
  const keyParts: string[] = [];
  for (const m of members) {
    const entries = [...m.entries()]
      .map(([k, v]) => `${k}:${v.optional ? "?" : ""}:${v.tsType?.repr ?? v.tsType?.kind ?? "?"}`)
      .sort();
    keyParts.push("{" + entries.join(",") + "}");
  }
  const key = keyParts.join("|");
  const existing = structKeys.get(key);
  if (existing) return existing;

  let name = preferred;
  while (
    (!selfName && elements.has(name)) ||
    structDecls.some((d) => d.name === name)
  ) {
    name = preferred + ++anonCounter;
  }

  // merged fields: a field is required only when it is required in every
  // alternative member that exists.
  const names = new Set<string>();
  for (const m of members) for (const k of m.keys()) names.add(k);
  const fields: Field[] = [];
  for (const n of names) {
    const candidates: Doc[] = [];
    let required = true;
    for (const m of members) {
      const p = m.get(n);
      if (!p || p.optional) required = false;
      if (p) candidates.push(p);
    }
    const chosen = pickFieldType(candidates);
    const nullable = candidates.some((c) => isNullishType(c.tsType));
    const fieldCtx: Ctx = { owner: preferred, typeParams: ctx.typeParams };
    let conv = mapType(chosen.tsType, fieldCtx, preferred + pascal(n));
    if (nullable && !conv.nilable) conv = ptrConv(conv);
    fields.push({
      goName: pascal(n),
      jsonName: n,
      conv,
      optional: !required,
      doc: chosen.jsDoc?.doc,
    });
  }
  structKeys.set(key, name);
  structDecls.push({ name, fields, doc, inline: !selfName });
  return name;
}

// ---------------- tsType -> Conv ----------------

function mapType(t: Doc, ctx: Ctx, inlineName: string): Conv {
  if (!t) return anyConv;
  switch (t.kind) {
    case "keyword":
      switch (t.keyword) {
        case "string":
          return stringConv;
        case "number":
          return floatConv;
        case "boolean":
          return boolConv;
        case "bigint":
          return bigIntConv;
        case "symbol":
          return jsValueConv;
        default:
          return anyConv; // any, unknown, object, null, undefined, void, ...
      }
    case "literal":
      switch (t.literal?.kind) {
        case "string":
          return stringConv;
        case "number":
          return floatConv;
        case "boolean":
          return boolConv;
        default:
          return anyConv;
      }
    case "typeRef":
      return mapTypeRef(t, ctx, inlineName);
    case "union":
      return mapUnion(t, ctx, inlineName);
    case "intersection": {
      const members = propMembers(t, new Set());
      if (members) {
        return structConv(registerStruct(inlineName, members, ctx), true);
      }
      return anyConv;
    }
    case "array":
      return sliceConv(mapType(t.array, ctx, inlineName + "Elem"));
    case "tuple": {
      // tuple with a single rest element behaves like a slice of its elem.
      const tp = t.tuple ?? [];
      if (tp.length === 1 && tp[0].kind === "rest") {
        const inner = tp[0].rest;
        if (inner.kind === "mapped") {
          return sliceConv(mapType(inner.mappedType.tsType, ctx, inlineName + "Elem"));
        }
        return sliceConv(mapType(inner, ctx, inlineName + "Elem"));
      }
      return sliceConv(anyConv);
    }
    case "typeLiteral": {
      const tl = t.typeLiteral;
      // index-signature-only object like `{ [key: string]: string }`
      // maps to a Go map.
      if (
        (tl.properties?.length ?? 0) === 0 &&
        (tl.methods?.length ?? 0) === 0 &&
        (tl.callSignatures?.length ?? 0) === 0 &&
        (tl.indexSignatures?.length ?? 0) > 0
      ) {
        const valConv = mapType(
          tl.indexSignatures[0].tsType,
          ctx,
          inlineName + "Val",
        );
        if (valConv.go === "string") return strMapConv;
        return anyConv;
      }
      const members = propMembers(t, new Set());
      if (members) {
        return structConv(registerStruct(inlineName, members, ctx), true);
      }
      return anyConv;
    }
    case "fnOrConstructor":
      return jsValueConv;
    case "this":
      return {
        go: `*${ctx.owner}`,
        fromJS: (v) => `${fromJSName(ctx.owner)}(${v})`,
        toJS: (x) => `${x}.toJS()`,
        nilable: true,
      };
    case "typeOperator":
      if (t.typeOperator.operator === "readonly") {
        return mapType(t.typeOperator.tsType, ctx, inlineName);
      }
      return anyConv;
    case "parenthesized":
      return mapType(t.parenthesized, ctx, inlineName);
    case "rest":
      return mapType(t.rest, ctx, inlineName);
    case "optional":
      return mapType(t.optional, ctx, inlineName);
    case "mapped":
      // `{ [K in keyof T]: X }` over a tuple/array is a slice of X.
      if (
        t.mappedType?.tsType &&
        t.mappedType?.typeParam?.constraint?.kind === "typeOperator" &&
        t.mappedType.typeParam.constraint.typeOperator?.operator === "keyof"
      ) {
        return sliceConv(mapType(t.mappedType.tsType, ctx, inlineName + "Elem"));
      }
      return anyConv;
    default:
      return anyConv; // conditional, indexedAccess, infer, ...
  }
}

function mapTypeRef(t: Doc, ctx: Ctx, inlineName: string): Conv {
  const name = lastSeg(t.typeRef.typeName);
  const params: Doc[] = t.typeRef.typeParams ?? [];
  switch (name) {
    case "Promise":
      return anyConv; // peeled by callers at return position
    case "Uint8Array":
    case "Uint8ClampedArray":
      return bytesConv;
    case "Date":
      return timeConv;
    case "URL":
      return stringConv;
    case "Array":
    case "ReadonlyArray":
      return params.length
        ? sliceConv(mapType(params[0], ctx, inlineName + "Elem"))
        : sliceConv(anyConv);
    case "Record":
      if (
        params.length === 2 && params[0].kind === "keyword" &&
        params[0].keyword === "string" && params[1].kind === "keyword" &&
        params[1].keyword === "string"
      ) {
        return strMapConv;
      }
      return anyConv;
  }
  if (ctx.typeParams.has(name)) return anyConv;
  const el = elements.get(name);
  if (!el) return jsValueConv; // Function, ReadableStream, AbortSignal, ...
  switch (elemClass.get(name) ?? classify(el)) {
    case "wrapper":
      return {
        go: `*${name}`,
        fromJS: (v) => `${fromJSName(name)}(${v})`,
        toJS: (x) => `${x}.toJS()`,
        fromJSFunc: fromJSName(name),
        nilable: true,
      };
    case "struct":
    case "merged":
      return structConv(name);
    case "stralias":
      return {
        go: name,
        fromJS: (v) => `${name}(getString(${v}))`,
        toJS: (x) => `js.ValueOf(string(${x}))`,
        nilable: false,
        scalar: true,
      };
    case "enum":
      return {
        go: name,
        fromJS: (v) => `${name}(int(getFloat(${v})))`,
        toJS: (x) => `js.ValueOf(int(${x}))`,
        nilable: false,
      };
    case "alias": {
      const inner = mapType(el.typeAliasDef.tsType, ctx, name);
      return {
        go: name,
        fromJS: (v) => `${name}(${inner.fromJS(v)})`,
        toJS: (x) => inner.toJS(x),
        nilable: inner.nilable,
        scalar: inner.scalar,
      };
    }
    default:
      return jsValueConv;
  }
}

function mapUnion(t: Doc, ctx: Ctx, inlineName: string): Conv {
  const members: Doc[] = t.union ?? [];
  const rest = members.filter((m: Doc) => !isNullishType(m));
  const hasNullish = rest.length !== members.length;
  if (rest.length === 0) return anyConv;
  if (rest.length === 1) {
    const base = mapType(rest[0], ctx, inlineName);
    return hasNullish ? (base.nilable ? base : ptrConv(base)) : base;
  }
  if (
    rest.every((m: Doc) => m.kind === "literal" && m.literal?.kind === "string")
  ) {
    return hasNullish ? ptrConv(stringConv) : stringConv;
  }
  const members2 = propMembers({ ...t, union: rest }, new Set());
  if (members2) {
    const c = structConv(registerStruct(inlineName, members2, ctx), true);
    return hasNullish ? ptrConv(c) : c;
  }
  return anyConv;
}

// ---------------- emit helpers ----------------

function emitStruct(d: StructDecl, out: string[]) {
  emitDoc(d.doc, out);
  // Anonymous shapes have no corresponding Deno API symbol to link to.
  if (!d.inline) emitDocLink(`Deno.${d.name}`, out);
  out.push(`type ${d.name} struct {`);
  for (const f of d.fields) {
    emitDoc(f.doc, out, "\t");
    let go = f.conv.go;
    if (f.optional && !f.conv.nilable) go = "*" + go;
    out.push(`\t${f.goName} ${go}`);
  }
  out.push(`}`, ``);
  out.push(`func (x ${d.name}) toJS() js.Value {`);
  out.push(`\to := jsutil.NewObject()`);
  for (const f of d.fields) {
    const ref = `x.${f.goName}`;
    if (f.optional) {
      if (f.conv.go === "js.Value") {
        // js.Value has no nil; presence is signalled by Truthy.
        out.push(
          `\tif ${ref}.Truthy() {`,
          `\t\to.Set("${f.jsonName}", ${ref})`,
          `\t}`,
        );
      } else if (f.conv.nilable) {
        out.push(
          `\tif ${ref} != nil {`,
          `\t\to.Set("${f.jsonName}", ${f.conv.toJS(ref)})`,
          `\t}`,
        );
      } else {
        // optional non-nilable: field is *T
        out.push(
          `\tif ${ref} != nil {`,
          `\t\to.Set("${f.jsonName}", ${f.conv.toJS("*" + ref)})`,
          `\t}`,
        );
      }
    } else if (f.conv.go.startsWith("*")) {
      // required nullable (T | null): emit null rather than undefined.
      out.push(
        `\tif ${ref} == nil {`,
        `\t\to.Set("${f.jsonName}", js.Null())`,
        `\t} else {`,
        `\t\to.Set("${f.jsonName}", ${f.conv.toJS(ref)})`,
        `\t}`,
      );
    } else {
      out.push(`\to.Set("${f.jsonName}", ${f.conv.toJS(ref)})`);
    }
  }
  out.push(`\treturn o`, `}`, ``);
  out.push(`func ${fromJSName(d.name)}(v js.Value) ${d.name} {`);
  out.push(`\tvar x ${d.name}`, `\tif isNullish(v) {`, `\t\treturn x`, `\t}`);
  for (const f of d.fields) {
    const get = `v.Get("${f.jsonName}")`;
    if (!f.optional || f.conv.nilable) {
      out.push(`\tx.${f.goName} = ${f.conv.fromJS(get)}`);
    } else {
      out.push(
        `\tx.${f.goName} = ptrFromJS(${get}, ${fromJSFunc(f.conv)})`,
      );
    }
  }
  out.push(`\treturn x`, `}`, ``);
}

// ---------------- function signature building ----------------

interface Param {
  name: string;
  conv: Conv;
  kind: "required" | "reqPtr" | "optVar" | "opt" | "rest" | "ptr";
}

function buildParams(fd: Doc, prefix: string, ctx: Ctx): Param[] {
  const params: Param[] = [];
  const raw: Doc[] = fd.params ?? [];
  for (let i = 0; i < raw.length; i++) {
    const p = raw[i];
    const isLast = i === raw.length - 1;
    if (p.kind === "rest") {
      const name = safeIdent(p.arg?.name ?? p.name ?? "rest");
      let elem: Doc = p.tsType;
      if (p.tsType?.kind === "array") elem = p.tsType.array;
      const c = mapType(elem, ctx, prefix + pascal(name) + "Elem");
      params.push({ name, conv: c, kind: "rest" });
      continue;
    }
    const name = safeIdent(p.name ?? p.left?.name ?? `arg${i}`);
    const optional = p.optional || p.kind === "assign";
    const c = mapType(
      p.tsType ?? p.arg?.tsType ?? p.left?.tsType,
      ctx,
      prefix + pascal(name),
    );
    if (!optional) {
      // Required inline option bags are passed as *T (nil -> undefined).
      params.push({
        name,
        conv: c,
        kind: c.inline ? "reqPtr" : "required",
      });
      continue;
    }
    if (isLast && c.scalar) {
      params.push({ name, conv: c, kind: "optVar" });
    } else if (c.go === "js.Value" || c.go === "any" || c.nilable ||
      c.inline) {
      params.push({ name, conv: c, kind: "opt" });
    } else {
      params.push({ name, conv: c, kind: "ptr" });
    }
  }
  return params;
}

function paramDecl(p: Param): string {
  switch (p.kind) {
    case "rest":
      return `${p.name} ...${p.conv.go}`;
    case "optVar":
      return `${p.name} ...${p.conv.go}`;
    case "reqPtr":
    case "ptr":
      return `${p.name} *${p.conv.go}`;
    case "opt":
      // optional inline option bags are *T; other opt convs are already
      // nilable Go types (slices, maps, wrappers, any, js.Value).
      return p.conv.inline
        ? `${p.name} *${p.conv.go}`
        : `${p.name} ${p.conv.go}`;
    default:
      return `${p.name} ${p.conv.go}`;
  }
}

// emitArgs returns lines that build `args` (or null when all params are
// required and can be inlined) plus the inline argument list.
// exprFor returns the call-argument expression for a required or reqPtr
// parameter.
function argExpr(p: Param): string {
  if (p.kind === "reqPtr") {
    return `ptrToJS(${p.name}, ${toJSFunc(p.conv)})`;
  }
  return p.conv.toJS(p.name);
}

function emitArgs(params: Param[]): { inline: string[] | null; lines: string[] } {
  const needsSlice = params.some((p) =>
    p.kind !== "required" && p.kind !== "reqPtr"
  );
  if (!needsSlice) {
    return { inline: params.map(argExpr), lines: [] };
  }
  const lines: string[] = [`args := []any{}`];
  for (const p of params) {
    switch (p.kind) {
      case "required":
      case "reqPtr":
        lines.push(`args = append(args, ${argExpr(p)})`);
        break;
      case "optVar":
        lines.push(
          `if len(${p.name}) > 0 {`,
          `\targs = append(args, ${p.conv.toJS(`${p.name}[0]`)})`,
          `}`,
        );
        break;
      case "rest":
        lines.push(
          `for _, e := range ${p.name} {`,
          `\targs = append(args, ${p.conv.toJS("e")})`,
          `}`,
        );
        break;
      case "opt":
        if (p.conv.go === "any") {
          lines.push(`args = append(args, anyToJS(${p.name}))`);
        } else if (p.conv.go === "js.Value") {
          lines.push(`args = append(args, ${p.name})`);
        } else if (p.conv.inline) {
          // optional inline option bag (*T): nil -> undefined.
          lines.push(`args = append(args, ptrToJS(${p.name}, ${toJSFunc(p.conv)}))`);
        } else if (p.conv.go.startsWith("*")) {
          // wrapper or T|null pointer conv: already nil-safe.
          lines.push(`args = append(args, ${p.conv.toJS(p.name)})`);
        } else {
          // nilable non-pointer (slices, maps): pass through or emit
          // undefined when nil.
          lines.push(
            `if ${p.name} != nil {`,
            `\targs = append(args, ${p.conv.toJS(p.name)})`,
            `} else {`,
            `\targs = append(args, js.Undefined())`,
            `}`,
          );
        }
        break;
      case "ptr":
        lines.push(`args = append(args, ptrToJS(${p.name}, ${toJSFunc(p.conv)}))`);
        break;
    }
  }
  return { inline: null, lines };
}

interface FuncEmit {
  recv?: string; // e.g. "x *Kv"
  goName: string;
  jsName: string;
  fd: Doc;
  owner: string;
  prefix: string; // PascalCase prefix for inline struct names
  target: string; // e.g. `x.instance` or `deno` or `deno.Get("KvU64")`
  callKind: "call" | "new" | "get";
  typeParams?: Set<string>; // type params in scope (e.g. from the class)
  doc?: string; // jsDoc text for the emitted comment
  docSymbol?: string; // Deno API symbol for the doc link (e.g. "Deno.Kv")
}

function emitFunc(fe: FuncEmit, out: string[]) {
  const fdTps = (fe.fd?.typeParams ?? [])
    .map((p: Doc) => p.name)
    .filter((n: Doc) => typeof n === "string");
  const ctx: Ctx = {
    owner: fe.owner,
    typeParams: new Set([...(fe.typeParams ?? []), ...fdTps]),
  };
  const params = buildParams(fe.fd, fe.prefix, ctx);
  const rt = fe.fd.returnType;

  // unwrap Promise<T>
  let awaited: Doc | null = null;
  let ret = rt;
  if (
    rt && rt.kind === "typeRef" &&
    lastSeg(rt.typeRef.typeName) === "Promise"
  ) {
    awaited = rt.typeRef.typeParams?.[0] ?? null;
    ret = null;
  }

  const isNew = fe.callKind === "new";
  const isThis = ret && ret.kind === "this";
  const isVoid = !ret || isNullishType(ret);
  const retConv = !isVoid && !isThis && !isNew
    ? mapType(ret, ctx, fe.prefix + "Result")
    : null;
  const awaitConv = awaited && !isNullishType(awaited)
    ? mapType(awaited, ctx, fe.prefix + "Result")
    : null;

  let sig = "";
  if (isNew) {
    sig = `*${fe.owner}`;
  } else if (
    awaited !== null ||
    (rt && lastSeg(rt.typeRef?.typeName ?? "") === "Promise")
  ) {
    sig = awaitConv ? `(${awaitConv.go}, error)` : `error`;
  } else if (isThis) {
    sig = `*${fe.owner}`;
  } else if (retConv) {
    sig = retConv.go;
  }

  const recv = fe.recv ? `(${fe.recv}) ` : "";
  emitDoc(fe.doc, out);
  if (fe.docSymbol) emitDocLink(fe.docSymbol, out);
  out.push(
    `func ${recv}${fe.goName}(${params.map(paramDecl).join(", ")})${
      sig ? " " + sig : ""
    } {`,
  );

  const { inline, lines } = emitArgs(params);
  for (const l of lines) out.push(`\t${l}`);
  let call: string;
  if (fe.callKind === "get") {
    call = `${fe.target}.Get("${fe.jsName}")`;
  } else {
    const argList = inline ? inline.join(", ") : "args...";
    const method = fe.callKind === "new" ? "New" : "Call";
    call = method === "New"
      ? `${fe.target}.New(${argList})`
      : `${fe.target}.Call("${fe.jsName}"${argList ? ", " + argList : ""})`;
  }

  if (isNew) {
    out.push(`\treturn ${fromJSName(fe.owner)}(${call})`);
  } else if (
    awaited !== null || (rt && rt.kind === "typeRef" &&
      lastSeg(rt.typeRef.typeName) === "Promise")
  ) {
    if (awaitConv) {
      out.push(`\tv, err := jsutil.AwaitPromise(${call})`);
      out.push(`\tif err != nil {`);
      out.push(`\t\treturn ${zeroOf(awaitConv.go)}, err`);
      out.push(`\t}`);
      out.push(`\treturn ${awaitConv.fromJS("v")}, nil`);
    } else {
      out.push(`\t_, err := jsutil.AwaitPromise(${call})`);
      out.push(`\treturn err`);
    }
  } else if (isThis) {
    out.push(`\t${call}`, `\treturn x`);
  } else if (retConv) {
    out.push(`\treturn ${retConv.fromJS(call)}`);
  } else {
    out.push(`\t${call}`);
  }
  out.push(`}`, ``);
}

// ---------------- emit elements ----------------

const typeOut: string[] = [];
const classOut: string[] = [];
const funcOut: string[] = [];
const varOut: string[] = [];

const typeNames = new Set<string>();
for (const n of reached) {
  const c = elemClass.get(n)!;
  if (["wrapper", "struct", "merged", "stralias", "alias", "enum"].includes(c)) {
    typeNames.add(n);
  }
}

function funcName(n: string): string {
  let r = pascal(n);
  if (typeNames.has(r)) r = "Get" + r;
  return r;
}

function emitWrapper(el: Doc) {
  const name = el.name;
  const def = el.classDef ?? el.interfaceDef;
  const ctx: Ctx = { owner: name, typeParams: typeParamsOf(el) };
  emitDoc(el.jsDoc?.doc, classOut);
  emitDocLink(`Deno.${name}`, classOut);
  classOut.push(`type ${name} struct {`, `\tinstance js.Value`, `}`, ``);
  classOut.push(
    `func ${fromJSName(name)}(v js.Value) *${name} {`,
    `\tif isNullish(v) {`,
    `\t\treturn nil`,
    `\t}`,
    `\treturn &${name}{instance: v}`,
    `}`,
    ``,
  );
  classOut.push(
    `func (x *${name}) toJS() js.Value {`,
    `\tif x == nil {`,
    `\t\treturn js.Undefined()`,
    `\t}`,
    `\treturn x.instance`,
    `}`,
    ``,
  );

  // constructors (classes only): keep the one with the most params.
  const ctors = [...(def.constructors ?? [])].sort(
    (a: Doc, b: Doc) => (b.params?.length ?? 0) - (a.params?.length ?? 0),
  );
  if (ctors.length > 0) {
    emitFunc({
      goName: `New${name}`,
      jsName: "",
      fd: { params: ctors[0].params, returnType: null },
      owner: name,
      prefix: `${name}New`,
      target: `deno.Get("${name}")`,
      callKind: "new",
      typeParams: ctx.typeParams,
      doc: ctors[0].jsDoc?.doc,
      docSymbol: `Deno.${name}`,
    }, classOut);
  }

  // properties -> getters (+ setters when writable)
  for (const p of def.properties ?? []) {
    if (typeof p.name !== "string" || p.name.startsWith("[") ||
      p.computed === true) {
      continue;
    }
    const conv = mapType(p.tsType, ctx, name + pascal(p.name));
    const goName = pascal(p.name);
    emitDoc(p.jsDoc?.doc, classOut);
    emitDocLink(`Deno.${name}`, classOut);
    classOut.push(
      `func (x *${name}) ${goName}() ${conv.go} {`,
      `\treturn ${conv.fromJS(`x.instance.Get("${p.name}")`)}`,
      `}`,
      ``,
    );
    if (!p.readonly) {
      classOut.push(
        `func (x *${name}) Set${goName}(v ${conv.go}) {`,
        `\tx.instance.Set("${p.name}", ${conv.toJS("v")})`,
        `}`,
        ``,
      );
    }
  }

  // methods: dedupe overloads by name, keeping the variant with the most
  // parameters.
  const methodMap = new Map<string, Doc>();
  for (const m of def.methods ?? []) {
    if (typeof m.name !== "string" || m.name.startsWith("[") ||
      m.name.startsWith("#")) continue;
    if (m.accessibility === "private" || m.accessibility === "protected") {
      continue;
    }
    // getters and setters may share a name; key them separately.
    const key = m.kind === "setter" ? m.name + " set" : m.name;
    const prev = methodMap.get(key);
    const np = (m.functionDef?.params ?? m.params ?? []).length;
    const pp = prev
      ? (prev.functionDef?.params ?? prev.params ?? []).length
      : -1;
    if (!prev || np > pp) methodMap.set(key, m);
  }

  for (const m of methodMap.values()) {
    if (m.kind === "getter") {
      const conv = mapType(
        m.functionDef?.returnType ?? m.returnType,
        ctx,
        name + pascal(m.name),
      );
      emitDoc(m.jsDoc?.doc, classOut);
      emitDocLink(`Deno.${name}`, classOut);
      classOut.push(
        `func (x *${name}) ${pascal(m.name)}() ${conv.go} {`,
        `\treturn ${conv.fromJS(`x.instance.Get("${m.name}")`)}`,
        `}`,
        ``,
      );
      continue;
    }
    if (m.kind === "setter") {
      const sp = (m.functionDef?.params ?? m.params ?? [])[0];
      const conv = mapType(
        sp?.tsType ?? sp?.arg?.tsType ?? sp?.left?.tsType,
        ctx,
        name + pascal(m.name),
      );
      emitDoc(m.jsDoc?.doc, classOut);
      emitDocLink(`Deno.${name}`, classOut);
      classOut.push(
        `func (x *${name}) Set${pascal(m.name)}(v ${conv.go}) {`,
        `\tx.instance.Set("${m.name}", ${conv.toJS("v")})`,
        `}`,
        ``,
      );
      continue;
    }
    if (m.kind !== "method") continue;
    // class methods carry functionDef; interface methods carry params /
    // returnType directly.
    const fd = m.functionDef ?? m;
    if (m.isStatic) {
      emitFunc({
        goName: name + pascal(m.name),
        jsName: m.name,
        fd,
        owner: name,
        prefix: name + pascal(m.name),
        target: `deno.Get("${name}")`,
        callKind: "call",
        typeParams: ctx.typeParams,
        doc: m.jsDoc?.doc,
        docSymbol: `Deno.${name}`,
      }, classOut);
    } else {
      emitFunc({
        recv: `x *${name}`,
        goName: pascal(m.name),
        jsName: m.name,
        fd,
        owner: name,
        prefix: name + pascal(m.name),
        target: "x.instance",
        callKind: "call",
        typeParams: ctx.typeParams,
        doc: m.jsDoc?.doc,
        docSymbol: `Deno.${name}`,
      }, classOut);
    }
  }
}

function emitStructElement(el: Doc) {
  const ctx: Ctx = { owner: el.name, typeParams: typeParamsOf(el) };
  const members = interfaceProps(el);
  structAliasIfDeduped(
    el.name,
    registerStruct(el.name, members, ctx, true, el.jsDoc?.doc),
    el,
  );
}

// structAliasIfDeduped emits `type A = B` plus a fromJS forwarder when a
// named element was deduplicated onto an already-registered identical struct.
function structAliasIfDeduped(name: string, got: string, el?: Doc) {
  if (got === name) return;
  emitDoc(el?.jsDoc?.doc, typeOut);
  emitDocLink(`Deno.${name}`, typeOut);
  typeOut.push(
    `type ${name} = ${got}`,
    ``,
    `func ${fromJSName(name)}(v js.Value) ${name} {`,
    `\treturn ${fromJSName(got)}(v)`,
    `}`,
    ``,
  );
}

function emitStralias(el: Doc) {
  const name = el.name;
  emitDoc(el.jsDoc?.doc, typeOut);
  emitDocLink(`Deno.${name}`, typeOut);
  typeOut.push(`type ${name} = string`, ``);
  const t = el.typeAliasDef.tsType;
  const lits: string[] = t.kind === "union"
    ? t.union.map((m: Doc) => m.literal.string)
    : [t.literal.string];
  typeOut.push(`const (`);
  for (const l of lits) {
    typeOut.push(`\t${name}${pascal(l)} ${name} = "${l}"`);
  }
  typeOut.push(`)`, ``);
}

function emitAlias(el: Doc) {
  const name = el.name;
  const ctx: Ctx = { owner: name, typeParams: typeParamsOf(el) };
  const c = mapType(el.typeAliasDef.tsType, ctx, name);
  emitDoc(el.jsDoc?.doc, typeOut);
  emitDocLink(`Deno.${name}`, typeOut);
  typeOut.push(`type ${name} = ${c.go}`, ``);
}

function emitEnum(el: Doc) {
  const name = el.name;
  emitDoc(el.jsDoc?.doc, typeOut);
  emitDocLink(`Deno.${name}`, typeOut);
  typeOut.push(`type ${name} int`, ``);
  typeOut.push(`const (`);
  let next = 0;
  for (const m of el.enumDef.members) {
    const init = m.init?.literal?.number ?? next;
    next = init + 1;
    typeOut.push(`\t${name}${pascal(m.name)} ${name} = ${init}`);
  }
  typeOut.push(`)`, ``);
}

function emitVariable(el: Doc) {
  const name = el.name;
  const ctx: Ctx = { owner: pascal(name), typeParams: new Set() };
  const conv = mapType(el.variableDef.tsType, ctx, pascal(name));
  emitDoc(el.jsDoc?.doc, varOut);
  emitDocLink(`Deno.${name}`, varOut);
  varOut.push(
    `func Get${pascal(name)}() ${conv.go} {`,
    `\treturn ${conv.fromJS(`deno.Get("${name}")`)}`,
    `}`,
    ``,
  );
}

function emitNamespaceFunc(el: Doc) {
  emitFunc({
    goName: funcName(el.name),
    jsName: el.name,
    fd: el.functionDef,
    owner: pascal(el.name),
    prefix: pascal(el.name),
    target: "deno",
    callKind: "call",
    doc: el.jsDoc?.doc,
    docSymbol: `Deno.${el.name}`,
  }, funcOut);
}

// Pre-register named element structs/merged aliases so they keep their
// declared names even if an identical anonymous shape is encountered later.
for (const e of nsNode.namespaceDef.elements) {
  if (!reached.has(e.name)) continue;
  if (elements.get(e.name) !== e) continue; // overload loser / namespace
  const cls = elemClass.get(e.name);
  if (cls === "struct") {
    emitStructElement(e);
  } else if (cls === "merged") {
    const got = registerStruct(
      e.name,
      propMembers(e.typeAliasDef.tsType, new Set([e.name]))!,
      {
        owner: e.name,
        typeParams: typeParamsOf(e),
      },
      true,
      e.jsDoc?.doc,
    );
    structAliasIfDeduped(e.name, got, e);
  }
}

for (const e of nsNode.namespaceDef.elements) {
  if (!reached.has(e.name)) continue;
  if (elements.get(e.name) !== e) continue; // overload loser
  switch (elemClass.get(e.name)) {
    case "wrapper":
      emitWrapper(e);
      break;
    case "stralias":
      emitStralias(e);
      break;
    case "alias":
      emitAlias(e);
      break;
    case "enum":
      emitEnum(e);
      break;
    case "variable":
      emitVariable(e);
      break;
    case "function":
      emitNamespaceFunc(e);
      break;
  }
}

// inline structs collected during emission
for (const d of structDecls) {
  emitStruct(d, typeOut);
}

// ---------------- write files ----------------

const HEADER = "// Code generated by scripts/gen-deno. DO NOT EDIT.\n\n" +
  "//go:build js && wasm\n\npackage deno\n";

function fileText(lines: string[]): string {
  const body = lines.join("\n");
  let imports = "";
  const imps: string[] = [];
  // Require an identifier after the package name so that doc comments
  // (e.g. a sentence ending in "time.") don't trigger a false import.
  if (/\bjs\.\w/.test(body)) imps.push(`"syscall/js"`);
  if (/\btime\.\w/.test(body)) imps.push(`"time"`);
  if (/\bjsutil\.\w/.test(body)) {
    imps.push(`"github.com/syumai/workers-go/internal/jsutil"`);
  }
  if (imps.length) {
    imports = "\nimport (\n" + imps.map((i) => `\t${i}`).join("\n") +
      "\n)\n";
  }
  return HEADER + imports + "\n" + body + "\n";
}

await mkdir(outDir, { recursive: true });
const files: [string, string[]][] = [
  ["types.gen.go", typeOut],
  ["classes.gen.go", classOut],
  ["functions.gen.go", funcOut],
  ["variables.gen.go", varOut],
];
const written: string[] = [];
for (const [name, lines] of files) {
  const p = path.join(outDir, name);
  await writeFile(p, fileText(lines));
  written.push(p);
}
capture(["gofmt", "-w", ...written]);
console.log(`generated: ${written.join(", ")}`);

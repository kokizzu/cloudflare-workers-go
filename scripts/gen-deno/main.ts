// Generates Go bindings for the Deno runtime APIs (the `Deno` global
// namespace) from `deno doc --json` output into the exp/deno package.
//
// Usage:
//   deno run --allow-run=deno,gofmt --allow-read --allow-write scripts/gen-deno/main.ts [options]
//
// Options:
//   --config <path>  gen.json path (default: gen.json next to this script)
//   --types <path>   deno.d.ts path (default: run `deno types` into a temp file)
//   --doc <path>     deno doc JSON path (default: run `deno doc --json`)
//   --out <dir>      output directory (default: <repo>/exp/deno)

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

async function capture(cmd: string[]): Promise<string> {
  const c = new Deno.Command(cmd[0], {
    args: cmd.slice(1),
    stdout: "piped",
    stderr: "piped",
  });
  const o = await c.output();
  if (!o.success) {
    throw new Error(
      `${cmd.join(" ")} failed: ${new TextDecoder().decode(o.stderr)}`,
    );
  }
  return new TextDecoder().decode(o.stdout);
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

const cliArgs = parseArgs(Deno.args);
const scriptDir = new URL(".", import.meta.url).pathname;
const repoRoot = new URL("../../", import.meta.url).pathname;
const configPath = cliArgs.config ?? `${scriptDir}gen.json`;
const outDir = cliArgs.out ?? `${repoRoot}exp/deno`;

async function loadDocText(): Promise<string> {
  if (cliArgs.doc) return await Deno.readTextFile(cliArgs.doc);
  let typesPath = cliArgs.types;
  if (!typesPath) {
    typesPath = await Deno.makeTempFile({ suffix: ".d.ts" });
    await Deno.writeTextFile(typesPath, await capture(["deno", "types"]));
  }
  return await capture(["deno", "doc", "--json", typesPath]);
}

const doc: Doc = JSON.parse(await loadDocText());
const config = JSON.parse(await Deno.readTextFile(configPath));
const nsNode = doc.nodes.find(
  (n: Doc) => n.kind === "namespace" && n.name === "Deno",
);
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
        m.set(p.name, { tsType: p.tsType, optional: p.optional });
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
    m.set(p.name, { tsType: p.tsType, optional: p.optional });
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
}

interface StructDecl {
  name: string;
  fields: Field[];
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
    fromJS: (v) => `${name}FromJS(${v})`,
    toJS: (x) => `${x}.ToJS()`,
    fromJSFunc: `${name}FromJS`,
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
    });
  }
  structKeys.set(key, name);
  structDecls.push({ name, fields });
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
        fromJS: (v) => `${ctx.owner}FromJS(${v})`,
        toJS: (x) => `${x}.ToJS()`,
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
        fromJS: (v) => `${name}FromJS(${v})`,
        toJS: (x) => `${x}.ToJS()`,
        fromJSFunc: `${name}FromJS`,
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
  out.push(`type ${d.name} struct {`);
  for (const f of d.fields) {
    let go = f.conv.go;
    if (f.optional && !f.conv.nilable) go = "*" + go;
    out.push(`\t${f.goName} ${go}`);
  }
  out.push(`}`, ``);
  out.push(`func (x ${d.name}) ToJS() js.Value {`);
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
  out.push(`func ${d.name}FromJS(v js.Value) ${d.name} {`);
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
  target: string; // e.g. `x.v` or `deno` or `deno.Get("KvU64")`
  callKind: "call" | "new" | "get";
  typeParams?: Set<string>; // type params in scope (e.g. from the class)
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
    out.push(`\treturn ${fe.owner}FromJS(${call})`);
  } else if (
    awaited !== null || (rt && rt.kind === "typeRef" &&
      lastSeg(rt.typeRef.typeName) === "Promise")
  ) {
    if (awaitConv) {
      out.push(`\tv, err := awaitResult(${call})`);
      out.push(`\tif err != nil {`);
      out.push(`\t\treturn ${zeroOf(awaitConv.go)}, err`);
      out.push(`\t}`);
      out.push(`\treturn ${awaitConv.fromJS("v")}, nil`);
    } else {
      out.push(`\t_, err := awaitResult(${call})`);
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
  classOut.push(`type ${name} struct {`, `\tv js.Value`, `}`, ``);
  classOut.push(
    `func ${name}FromJS(v js.Value) *${name} {`,
    `\tif isNullish(v) {`,
    `\t\treturn nil`,
    `\t}`,
    `\treturn &${name}{v: v}`,
    `}`,
    ``,
  );
  classOut.push(
    `func (x *${name}) ToJS() js.Value {`,
    `\tif x == nil {`,
    `\t\treturn js.Undefined()`,
    `\t}`,
    `\treturn x.v`,
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
    classOut.push(
      `func (x *${name}) ${goName}() ${conv.go} {`,
      `\treturn ${conv.fromJS(`x.v.Get("${p.name}")`)}`,
      `}`,
      ``,
    );
    if (!p.readonly) {
      classOut.push(
        `func (x *${name}) Set${goName}(v ${conv.go}) {`,
        `\tx.v.Set("${p.name}", ${conv.toJS("v")})`,
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
      classOut.push(
        `func (x *${name}) ${pascal(m.name)}() ${conv.go} {`,
        `\treturn ${conv.fromJS(`x.v.Get("${m.name}")`)}`,
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
      classOut.push(
        `func (x *${name}) Set${pascal(m.name)}(v ${conv.go}) {`,
        `\tx.v.Set("${m.name}", ${conv.toJS("v")})`,
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
      }, classOut);
    } else {
      emitFunc({
        recv: `x *${name}`,
        goName: pascal(m.name),
        jsName: m.name,
        fd,
        owner: name,
        prefix: name + pascal(m.name),
        target: "x.v",
        callKind: "call",
        typeParams: ctx.typeParams,
      }, classOut);
    }
  }
}

function emitStructElement(el: Doc) {
  const ctx: Ctx = { owner: el.name, typeParams: typeParamsOf(el) };
  const members = interfaceProps(el);
  structAliasIfDeduped(el.name, registerStruct(el.name, members, ctx, true));
}

// structAliasIfDeduped emits `type A = B` plus a FromJS forwarder when a
// named element was deduplicated onto an already-registered identical struct.
function structAliasIfDeduped(name: string, got: string) {
  if (got === name) return;
  typeOut.push(
    `type ${name} = ${got}`,
    ``,
    `func ${name}FromJS(v js.Value) ${name} {`,
    `\treturn ${got}FromJS(v)`,
    `}`,
    ``,
  );
}

function emitStralias(el: Doc) {
  const name = el.name;
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
  typeOut.push(`type ${name} = ${c.go}`, ``);
}

function emitEnum(el: Doc) {
  const name = el.name;
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
    );
    structAliasIfDeduped(e.name, got);
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
  if (/\bjs\./.test(body)) imps.push(`"syscall/js"`);
  if (/\btime\./.test(body)) imps.push(`"time"`);
  if (/\bjsutil\./.test(body)) {
    imps.push(`"github.com/syumai/workers-go/internal/jsutil"`);
  }
  if (imps.length) {
    imports = "\nimport (\n" + imps.map((i) => `\t${i}`).join("\n") +
      "\n)\n";
  }
  return HEADER + imports + "\n" + body + "\n";
}

await Deno.mkdir(outDir, { recursive: true });
const files: [string, string[]][] = [
  ["types.gen.go", typeOut],
  ["classes.gen.go", classOut],
  ["functions.gen.go", funcOut],
  ["variables.gen.go", varOut],
];
const written: string[] = [];
for (const [name, lines] of files) {
  const p = `${outDir}/${name}`;
  await Deno.writeTextFile(p, fileText(lines));
  written.push(p);
}
await capture(["gofmt", "-w", ...written]);
console.log(`generated: ${written.join(", ")}`);

const modPromise = WebAssembly.compileStreaming(
  fetch(new URL("./app.wasm", import.meta.url)),
);

export async function loadModule() {
  return await modPromise;
}

export function createRuntimeContext({ binding }) {
  return {
    binding,
  };
}

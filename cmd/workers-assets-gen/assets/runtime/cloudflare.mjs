import { connect } from "cloudflare:sockets";
import { EmailMessage } from "cloudflare:email";
import mod from "./app.wasm";

export async function loadModule() {
  return mod;
}

export function createRuntimeContext({ env, ctx, binding, durableObject }) {
  return {
    env,
    ctx,
    connect,
    EmailMessage,
    binding,
    durableObject,
  };
}

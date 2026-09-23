package main

type Runtime string

const (
	RuntimeCloudflare Runtime = "cloudflare"
	RuntimeBrowser    Runtime = "browser"
	RuntimeDeno       Runtime = "deno"
	RuntimeNeon       Runtime = "neon"
)

func (r Runtime) IsValid() bool {
	switch r {
	case RuntimeCloudflare, RuntimeBrowser, RuntimeDeno, RuntimeNeon:
		return true
	}
	return false
}

func (r Runtime) AssetFileName() string {
	return string(r) + ".mjs"
}

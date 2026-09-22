//go:build js && wasm

package ai

import (
	"syscall/js"
	"testing"
)

// resolve wraps v in an already-resolved JS Promise.
func resolve(v any) js.Value {
	return js.Global().Get("Promise").Call("resolve", v)
}

// TestAi_RunTextGeneration verifies RunTextGeneration passes the model name
// and the encoded inputs.prompt to the underlying run() call, and decodes
// the resolved response back into AiTextGenerationOutput.Response.
func TestAi_RunTextGeneration(t *testing.T) {
	var gotModel, gotPrompt string
	fake := js.ValueOf(map[string]any{})
	fake.Set("run", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotModel = args[0].String()
		gotPrompt = args[1].Get("prompt").String()
		return resolve(map[string]any{"response": "hello back"})
	}))

	a := AiFromJS(fake)
	out, err := a.RunTextGeneration("@cf/meta/llama-3-8b-instruct", AiTextGenerationInput{Prompt: "hello"}, nil)
	if err != nil {
		t.Fatalf("RunTextGeneration() failed: %v", err)
	}
	if gotModel != "@cf/meta/llama-3-8b-instruct" {
		t.Errorf("model sent to run() = %q, want %q", gotModel, "@cf/meta/llama-3-8b-instruct")
	}
	if gotPrompt != "hello" {
		t.Errorf("inputs.prompt sent to run() = %q, want %q", gotPrompt, "hello")
	}
	if out.Response != "hello back" {
		t.Errorf("out.Response = %q, want %q", out.Response, "hello back")
	}
}

// TestAi_RunStream verifies RunStream forces inputs.stream = true before
// calling run(), and wraps the resolved value as a non-nil io.ReadCloser
// rather than decoding it.
func TestAi_RunStream(t *testing.T) {
	var gotStream bool
	fakeReadableStream := js.ValueOf(map[string]any{})
	fake := js.ValueOf(map[string]any{})
	fake.Set("run", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotStream = args[1].Get("stream").Bool()
		return resolve(fakeReadableStream)
	}))

	a := AiFromJS(fake)
	inputs := js.ValueOf(map[string]any{"prompt": "hello"})
	rc, err := a.RunStream("@cf/meta/llama-3-8b-instruct", inputs, nil)
	if err != nil {
		t.Fatalf("RunStream() failed: %v", err)
	}
	if !gotStream {
		t.Errorf("inputs.stream sent to run() = %v, want true", gotStream)
	}
	if rc == nil {
		t.Fatal("RunStream() returned a nil io.ReadCloser")
	}
	if !inputs.Get("stream").Bool() {
		t.Errorf("RunStream() did not set stream = true on inputs in place")
	}
}

// TestAi_RunTextGenerationStream verifies RunTextGenerationStream sets
// in.Stream and forwards it through as inputs.stream on the JS call.
func TestAi_RunTextGenerationStream(t *testing.T) {
	var gotStream bool
	fake := js.ValueOf(map[string]any{})
	fake.Set("run", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotStream = args[1].Get("stream").Bool()
		return resolve(js.ValueOf(map[string]any{}))
	}))

	a := AiFromJS(fake)
	if _, err := a.RunTextGenerationStream("@cf/meta/llama-3-8b-instruct", AiTextGenerationInput{Prompt: "hi"}, nil); err != nil {
		t.Fatalf("RunTextGenerationStream() failed: %v", err)
	}
	if !gotStream {
		t.Errorf("inputs.stream sent to run() = %v, want true", gotStream)
	}
}

// TestAi_RunTextEmbeddings verifies RunTextEmbeddings passes inputs.text
// and decodes shape/data, including the generated [][]float64 decode of
// the nested "data" field (see ai.yaml; this exercises cfgen's
// array-of-array codegen end to end, via the real generated
// aiTextEmbeddingsOutputFromJS, not just the cfgen/gen golden tests).
func TestAi_RunTextEmbeddings(t *testing.T) {
	var gotTexts []string
	fake := js.ValueOf(map[string]any{})
	fake.Set("run", js.FuncOf(func(this js.Value, args []js.Value) any {
		text := args[1].Get("text")
		for i := 0; i < text.Length(); i++ {
			gotTexts = append(gotTexts, text.Index(i).String())
		}
		return resolve(map[string]any{
			"shape": []any{2.0, 3.0},
			"data": []any{
				[]any{0.1, 0.2, 0.3},
				[]any{0.4, 0.5, 0.6},
			},
		})
	}))

	a := AiFromJS(fake)
	out, err := a.RunTextEmbeddings("@cf/baai/bge-base-en-v1.5", AiTextEmbeddingsInput{Text: []string{"a", "b"}}, nil)
	if err != nil {
		t.Fatalf("RunTextEmbeddings() failed: %v", err)
	}
	if len(gotTexts) != 2 || gotTexts[0] != "a" || gotTexts[1] != "b" {
		t.Errorf("inputs.text sent to run() = %v, want [a b]", gotTexts)
	}
	if len(out.Shape) != 2 || out.Shape[0] != 2 || out.Shape[1] != 3 {
		t.Errorf("out.Shape = %v, want [2 3]", out.Shape)
	}
	data := out.Data
	if len(data) != 2 || len(data[0]) != 3 || len(data[1]) != 3 {
		t.Fatalf("out.Data = %v, want a 2x3 matrix", data)
	}
	if data[0][0] != 0.1 || data[0][2] != 0.3 || data[1][1] != 0.5 {
		t.Errorf("out.Data = %v, want rows [0.1 0.2 0.3] [0.4 0.5 0.6]", data)
	}
}

// TestAi_Models verifies the generated Models method (Ai.models) decodes
// the resolved array of AiModelsSearchObject.
func TestAi_Models(t *testing.T) {
	var gotAuthor string
	fake := js.ValueOf(map[string]any{})
	fake.Set("models", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotAuthor = args[0].Get("author").String()
		return resolve([]any{
			map[string]any{
				"id":          "some-model",
				"source":      1.0,
				"name":        "Some Model",
				"description": "a model",
				"task":        map[string]any{"id": "text-generation", "name": "Text Generation", "description": ""},
				"tags":        []any{},
				"properties":  []any{},
			},
		})
	}))

	a := AiFromJS(fake)
	got, err := a.Models(AiModelsSearchParams{Author: "cf"})
	if err != nil {
		t.Fatalf("Models() failed: %v", err)
	}
	if gotAuthor != "cf" {
		t.Errorf("params.author sent to models() = %q, want %q", gotAuthor, "cf")
	}
	if len(got) != 1 || got[0].ID != "some-model" || got[0].Name != "Some Model" {
		t.Errorf("Models() = %+v, want one AiModelsSearchObject{ID: some-model, Name: Some Model}", got)
	}
}

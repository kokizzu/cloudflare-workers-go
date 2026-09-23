//go:build js && wasm

package email

import (
	"errors"
	"strings"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/exp/internal/jsrt"
	"github.com/syumai/workers-go/internal/jsutil"
)

// withEmailMessageClass installs a fake EmailMessage constructor at
// jsutil.RuntimeContext.EmailMessage (mirroring what
// cmd/workers-assets-gen/assets/runtimes/cloudflare/runtime.mjs adds to the runtime
// context for the real "cloudflare:email" module class) for the duration
// of the test, and restores the previous runtime context afterwards.
func withEmailMessageClass(t *testing.T, class js.Value) {
	t.Helper()
	prev := jsutil.RuntimeContext
	ctx := jsrt.NewObject()
	ctx.Set("EmailMessage", class)
	jsutil.RuntimeContext = ctx
	t.Cleanup(func() { jsutil.RuntimeContext = prev })
}

// TestNewEmailMessage_PassesStream verifies NewEmailMessage looks up the
// EmailMessage class via the runtime context, constructs it with `new`,
// and passes raw as a JS ReadableStream (not a string).
func TestNewEmailMessage_PassesStream(t *testing.T) {
	var gotFrom, gotTo string
	var gotRawIsStream bool
	class := js.FuncOf(func(this js.Value, args []js.Value) any {
		gotFrom = args[0].String()
		gotTo = args[1].String()
		gotRawIsStream = !args[2].Get("getReader").IsUndefined()
		this.Set("from", args[0])
		this.Set("to", args[1])
		return nil
	})
	defer class.Release()
	withEmailMessageClass(t, class.Value)

	msg, err := NewEmailMessage("a@example.com", "b@example.com", strings.NewReader("Subject: hi\r\n\r\nbody"))
	if err != nil {
		t.Fatalf("NewEmailMessage() failed: %v", err)
	}
	if msg == nil || msg.JSValue().IsUndefined() {
		t.Fatal("NewEmailMessage() returned a nil/undefined instance")
	}
	if gotFrom != "a@example.com" || gotTo != "b@example.com" {
		t.Errorf("constructor got (from, to) = (%q, %q), want (a@example.com, b@example.com)", gotFrom, gotTo)
	}
	if !gotRawIsStream {
		t.Errorf("constructor's raw argument was not a ReadableStream")
	}
}

// TestNewEmailMessageString_PassesString verifies NewEmailMessageString
// passes raw straight through as a plain JS string.
func TestNewEmailMessageString_PassesString(t *testing.T) {
	var gotRaw string
	class := js.FuncOf(func(this js.Value, args []js.Value) any {
		gotRaw = args[2].String()
		return nil
	})
	defer class.Release()
	withEmailMessageClass(t, class.Value)

	if _, err := NewEmailMessageString("a@example.com", "b@example.com", "raw mime"); err != nil {
		t.Fatalf("NewEmailMessageString() failed: %v", err)
	}
	if gotRaw != "raw mime" {
		t.Errorf("constructor's raw argument = %q, want %q", gotRaw, "raw mime")
	}
}

// TestNewEmailMessage_NoRuntimeSupport verifies a clear error (rather than a
// panic) when the current runtime shim doesn't provide EmailMessage, as
// browser.mjs's createRuntimeContext does not.
func TestNewEmailMessage_NoRuntimeSupport(t *testing.T) {
	prev := jsutil.RuntimeContext
	jsutil.RuntimeContext = jsrt.NewObject() // no "EmailMessage" key set
	t.Cleanup(func() { jsutil.RuntimeContext = prev })

	if _, err := NewEmailMessage("a@example.com", "b@example.com", strings.NewReader("x")); err == nil {
		t.Fatal("NewEmailMessage() succeeded, want an error")
	}
}

// fakeForwardableEmailMessage builds a fake JS ForwardableEmailMessage:
// from/to/rawSize as plain values, headers as a Headers-shaped object with
// an empty entries() (enough for jshttp.ToHeader), and forward() recording
// its arguments and resolving with an EmailSendResult-shaped object.
func fakeForwardableEmailMessage(t *testing.T, forward func(rcptTo string)) js.Value {
	t.Helper()
	headers := js.ValueOf(map[string]any{})
	headers.Set("entries", js.FuncOf(func(this js.Value, args []js.Value) any {
		return js.ValueOf([]any{})
	}))
	fake := js.ValueOf(map[string]any{
		"from":    "sender@example.com",
		"to":      "recipient@example.com",
		"rawSize": 42.0,
		"headers": headers,
	})
	fake.Set("forward", js.FuncOf(func(this js.Value, args []js.Value) any {
		forward(args[0].String())
		return js.Global().Get("Promise").Call("resolve", map[string]any{"messageId": "id-1"})
	}))
	return fake
}

// TestRegisterHandler_InvokesHandlerAndForwards verifies that the function
// registerHandler wires up onto jsutil.Binding.handleEmail decodes the JS
// message into a *ForwardableEmailMessage, invokes the Handler with it, and
// that calling Forward from within the handler reaches the fake's forward()
// with the given rcptTo — the same path Handle uses, without Handle's
// ready()/select{} tail (see registerHandler's doc comment).
func TestRegisterHandler_InvokesHandlerAndForwards(t *testing.T) {
	var gotRcptTo string
	fake := fakeForwardableEmailMessage(t, func(rcptTo string) { gotRcptTo = rcptTo })

	var handlerCalled bool
	registerHandler(func(msg *ForwardableEmailMessage) error {
		handlerCalled = true
		if msg.From() != "sender@example.com" {
			t.Errorf("msg.From() = %q, want %q", msg.From(), "sender@example.com")
		}
		_, err := msg.Forward("forward-to@example.com", nil)
		return err
	})

	handleEmail := jsutil.Binding.Get("handleEmail")
	if handleEmail.IsUndefined() {
		t.Fatal("registerHandler did not register \"handleEmail\" on jsutil.Binding")
	}
	promise := handleEmail.Invoke(fake)
	if _, err := jsrt.Await(promise); err != nil {
		t.Fatalf("handleEmail promise rejected: %v", err)
	}
	if !handlerCalled {
		t.Fatal("registered Handler was not invoked")
	}
	if gotRcptTo != "forward-to@example.com" {
		t.Errorf("forward() got rcptTo = %q, want %q", gotRcptTo, "forward-to@example.com")
	}
}

// TestRegisterHandler_ErrorRejectsPromise verifies a Handler error surfaces
// as a rejected Promise instead of panicking.
func TestRegisterHandler_ErrorRejectsPromise(t *testing.T) {
	fake := fakeForwardableEmailMessage(t, func(string) {})

	registerHandler(func(msg *ForwardableEmailMessage) error {
		return errors.New("boom")
	})

	promise := jsutil.Binding.Get("handleEmail").Invoke(fake)
	if _, err := jsrt.Await(promise); err == nil {
		t.Fatal("handleEmail promise resolved, want a rejection")
	}
}

// TestSendEmail_SendBuilder exercises the tmp/06-codegen-spec.md 6.2 item 2
// builder-object overload of SendEmail.send: EmailMessageBuilder (an
// intersection-flattened merge of EmailReplyMessageBuilder &
// EmailDestinations) round-tripping through its generated toJS(), with a
// From value built as a plain js.Value (since from/replyTo/to/cc/bcc all
// fall back to js.Value per exp/internal/gen/overrides/email.yaml's
// documented union overrides).
func TestSendEmail_SendBuilder(t *testing.T) {
	var gotFrom, gotSubject, gotTo string
	fake := js.ValueOf(map[string]any{})
	fake.Set("send", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotFrom = args[0].Get("from").String()
		gotSubject = args[0].Get("subject").String()
		gotTo = args[0].Get("to").String()
		result := js.ValueOf(map[string]any{"messageId": "id-1"})
		return js.Global().Get("Promise").Call("resolve", result)
	}))

	se := SendEmailFromJS(fake)
	result, err := se.SendBuilder(EmailMessageBuilder{
		From:    js.ValueOf("sender@example.com"),
		Subject: "hello",
		Text:    "body",
		To:      js.ValueOf("recipient@example.com"),
	})
	if err != nil {
		t.Fatalf("SendBuilder() failed: %v", err)
	}
	if gotFrom != "sender@example.com" {
		t.Errorf("builder.from sent to JS = %q, want %q", gotFrom, "sender@example.com")
	}
	if gotSubject != "hello" {
		t.Errorf("builder.subject sent to JS = %q, want %q", gotSubject, "hello")
	}
	if gotTo != "recipient@example.com" {
		t.Errorf("builder.to sent to JS = %q, want %q", gotTo, "recipient@example.com")
	}
	if result.MessageID != "id-1" {
		t.Errorf("result.MessageID = %q, want %q", result.MessageID, "id-1")
	}
}

// TestForwardableEmailMessage_ReplyBuilder exercises the builder-object
// overload of ForwardableEmailMessage.reply (EmailReplyMessageBuilder,
// which has no to/cc/bcc fields).
func TestForwardableEmailMessage_ReplyBuilder(t *testing.T) {
	var gotSubject string
	fake := fakeForwardableEmailMessage(t, func(string) {})
	fake.Set("reply", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotSubject = args[0].Get("subject").String()
		result := js.ValueOf(map[string]any{"messageId": "id-2"})
		return js.Global().Get("Promise").Call("resolve", result)
	}))

	msg := ForwardableEmailMessageFromJS(fake)
	result, err := msg.ReplyBuilder(EmailReplyMessageBuilder{
		From:    js.ValueOf("sender@example.com"),
		Subject: "re: hello",
		Text:    "body",
	})
	if err != nil {
		t.Fatalf("ReplyBuilder() failed: %v", err)
	}
	if gotSubject != "re: hello" {
		t.Errorf("builder.subject sent to JS = %q, want %q", gotSubject, "re: hello")
	}
	if result.MessageID != "id-2" {
		t.Errorf("result.MessageID = %q, want %q", result.MessageID, "id-2")
	}
}

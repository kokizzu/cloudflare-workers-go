package main

import (
	"log"

	"github.com/syumai/workers-go/exp/cloudflare/email"
)

// forwardTo is the destination address inbound mail gets forwarded to. It
// must already be a *verified* destination address on this zone's Email
// Routing settings (Cloudflare requires proving control of an address
// before Workers may forward mail to it) — see
// https://developers.cloudflare.com/email-routing/email-workers/#forward-to-a-verified-address.
const forwardTo = "you@example.com"

func main() {
	// email.Handle registers the Worker's email(message, env, ctx) handler
	// and blocks: this Worker's only job is handling inbound mail (see
	// exp/cloudflare/email's Handle doc comment for the HTTP-plus-email
	// case, which needs workers.Serve instead).
	email.Handle(func(msg *email.ForwardableEmailMessage) error {
		log.Printf("email: received from=%s to=%s size=%d bytes", msg.From(), msg.To(), msg.RawSize())

		if _, err := msg.Forward(forwardTo, nil); err != nil {
			log.Printf("email: forwarding to %s failed: %v", forwardTo, err)
			// setReject tells the connecting SMTP client the message was
			// not accepted (a permanent failure), instead of silently
			// dropping it.
			return msg.SetReject("forwarding failed")
		}
		return nil
	})
}

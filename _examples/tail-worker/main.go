package main

import (
	"log"

	"github.com/syumai/workers-go/exp/cloudflare/tail"
)

func main() {
	// tail.Handle registers the Worker's tail(events, env, ctx) handler and
	// blocks: this Worker's only job is consuming another ("producer")
	// Worker's trace events (see exp/cloudflare/tail's Handle doc comment
	// for the case where a Worker also serves other triggers, which needs
	// workers.Serve or another blocking entry point instead).
	tail.Handle(func(items []tail.TraceItem) error {
		for _, item := range items {
			log.Printf("tail: script=%s outcome=%s executionModel=%s truncated=%t cpuTime=%.2fms wallTime=%.2fms",
				item.ScriptName, item.Outcome, item.ExecutionModel, item.Truncated, item.CpuTime, item.WallTime)

			// EventKind()/typed accessors decode TraceItem.event, a
			// discriminated union cfgen leaves as js.Value (see
			// exp/cloudflare/tail's package doc comment) — switch on the
			// kind to reach the fields specific to that trigger.
			switch item.EventKind() {
			case "fetch":
				if fe, ok := item.FetchEvent(); ok {
					log.Printf("tail:   fetch %s %s", fe.Request.Method(), fe.Request.URL())
				}
			case "scheduled":
				if se, ok := item.ScheduledEvent(); ok {
					log.Printf("tail:   scheduled cron=%q", se.Cron)
				}
			case "queue":
				if qe, ok := item.QueueEvent(); ok {
					log.Printf("tail:   queue=%s batchSize=%d", qe.Queue, int(qe.BatchSize))
				}
			case "email":
				if ee, ok := item.EmailEvent(); ok {
					log.Printf("tail:   email from=%s to=%s", ee.MailFrom, ee.RcptTo)
				}
			}

			for _, l := range item.Logs {
				log.Printf("tail:   log[%s] %v", l.Level, l.Message)
			}
			for _, e := range item.Exceptions {
				log.Printf("tail:   exception %s: %s", e.Name, e.Message)
			}
		}
		return nil
	})
}

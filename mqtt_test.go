package main

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type published struct {
	topic    string
	payload  string
	retained bool
}

// recordingClient is a dummyClient that remembers what was published and subscribed.
type recordingClient struct {
	dummyClient
	mu         sync.Mutex
	msgs       []published
	subscribed map[string]mqtt.MessageHandler
}

func (r *recordingClient) Publish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token {
	r.mu.Lock()
	defer r.mu.Unlock()
	var s string
	switch p := payload.(type) {
	case string:
		s = p
	case []byte:
		s = string(p)
	}
	r.msgs = append(r.msgs, published{topic: topic, payload: s, retained: retained})
	return &dummyToken{}
}

func (r *recordingClient) Subscribe(topic string, qos byte, callback mqtt.MessageHandler) mqtt.Token {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.subscribed == nil {
		r.subscribed = map[string]mqtt.MessageHandler{}
	}
	r.subscribed[topic] = callback
	return &dummyToken{}
}

// find returns the last message published to topic.
func (r *recordingClient) find(topic string) (published, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.msgs) - 1; i >= 0; i-- {
		if r.msgs[i].topic == topic {
			return r.msgs[i], true
		}
	}
	return published{}, false
}

func (r *recordingClient) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.msgs)
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(log.Writer()) })
	return &buf
}

func newTestApp(t *testing.T, sys System) *app {
	t.Helper()
	return newApp(&config{DryRun: false}, sys, "h")
}

func TestPublishRecordsTopicAndPayload(t *testing.T) {
	sys, _ := newSystem()
	a := newTestApp(t, sys)
	rc := &recordingClient{}

	a.publish(rc, "win2mqtt/h/status/x", false, "1")

	msg, ok := rc.find("win2mqtt/h/status/x")
	if !ok {
		t.Fatal("message not published")
	}
	if msg.payload != "1" || msg.retained {
		t.Fatalf("got %+v, want payload 1 not retained", msg)
	}
}

func TestWarnOnceLogsOnce(t *testing.T) {
	buf := captureLog(t)
	sys, _ := newSystem()
	a := newTestApp(t, sys)

	a.warnOnce("volume", errors.New("boom"))
	a.warnOnce("volume", errors.New("boom"))

	if n := strings.Count(buf.String(), "Warning: volume"); n != 1 {
		t.Fatalf("expected one warning, got %d:\n%s", n, buf.String())
	}
}

func TestNewMQTTClientDryRunUsesDummy(t *testing.T) {
	captureLog(t)
	sys, _ := newSystem()
	a := newApp(&config{DryRun: true}, sys, "h")

	client := a.newMQTTClient()

	if _, ok := client.(*dummyClient); !ok {
		t.Fatalf("expected *dummyClient, got %T", client)
	}
}

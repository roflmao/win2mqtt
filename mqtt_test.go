package main

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

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

// syncBuffer is a bytes.Buffer safe for use as a log sink read by tests while
// background goroutines are still logging.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

func captureLog(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	previous := log.Writer()
	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(previous) })
	return buf
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

// fakeMessage satisfies mqtt.Message for subscription callbacks.
type fakeMessage struct {
	topic    string
	payload  string
	retained bool
}

func (m fakeMessage) Duplicate() bool   { return false }
func (m fakeMessage) Qos() byte         { return 0 }
func (m fakeMessage) Retained() bool    { return m.retained }
func (m fakeMessage) Topic() string     { return m.topic }
func (m fakeMessage) MessageID() uint16 { return 0 }
func (m fakeMessage) Payload() []byte   { return []byte(m.payload) }
func (m fakeMessage) Ack()              {}

func TestRetainedCommandsAreIgnored(t *testing.T) {
	buf := captureLog(t)
	sys := newFakeSystem()
	a := newTestApp(t, sys)
	rc := &recordingClient{}
	a.subscribeCommands(rc)
	handler := rc.subscribed["win2mqtt/h/command/#"]
	if handler == nil {
		t.Fatal("command topic not subscribed")
	}

	handler(rc, fakeMessage{topic: "win2mqtt/h/command/shutdown", payload: "shutdown", retained: true})
	if !strings.Contains(buf.String(), "Ignoring retained command") {
		t.Fatalf("expected log line, got:\n%s", buf.String())
	}
	time.Sleep(50 * time.Millisecond)
	if n := sys.callCount(); n != 0 {
		t.Fatalf("retained command executed: %v", sys.calls)
	}

	handler(rc, fakeMessage{topic: "win2mqtt/h/command/shutdown", payload: "shutdown"})
	waitFor(t, func() bool { return sys.callCount() == 1 })
	if sys.calls[0] != "shutdown" {
		t.Fatalf("live command not executed: %v", sys.calls)
	}
}

// stuckToken never completes, like a publish whose TCP write has stalled.
type stuckToken struct{ dummyToken }

func (t *stuckToken) Wait() bool                       { select {} }
func (t *stuckToken) WaitTimeout(d time.Duration) bool { time.Sleep(d); return false }
func (t *stuckToken) Done() <-chan struct{}            { return make(chan struct{}) }

type stuckClient struct{ dummyClient }

func (c *stuckClient) Publish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token {
	return &stuckToken{}
}

func TestPublishReturnsWhenBrokerWriteStalls(t *testing.T) {
	buf := captureLog(t)
	old := publishTimeout
	publishTimeout = 50 * time.Millisecond
	t.Cleanup(func() { publishTimeout = old })
	sys, _ := newSystem()
	a := newTestApp(t, sys)

	done := make(chan struct{})
	go func() {
		a.publish(&stuckClient{}, "win2mqtt/h/status/x", false, "1")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Millisecond):
		t.Fatal("publish must not block the caller while a token is outstanding")
	}
	waitFor(t, func() bool { return strings.Contains(buf.String(), "timed out") })
}

func TestCommandsRunInArrivalOrder(t *testing.T) {
	captureLog(t)
	sys := newFakeSystem()
	sys.blockLock = make(chan struct{})
	a := newTestApp(t, sys)
	rc := &recordingClient{}
	a.subscribeCommands(rc)
	handler := rc.subscribed["win2mqtt/h/command/#"]

	handler(rc, fakeMessage{topic: "win2mqtt/h/command/lock", payload: "lock"})
	handler(rc, fakeMessage{topic: "win2mqtt/h/command/volume", payload: "70"})

	time.Sleep(50 * time.Millisecond)
	if v, _ := sys.Volume(); v != 42 { // still 42: volume must wait for the lock command ahead of it
		t.Fatalf("volume applied out of order: %d", v)
	}
	close(sys.blockLock)
	waitFor(t, func() bool { v, _ := sys.Volume(); return v == 70 })
}

func TestCommandCallbackReturnsBeforeCommandFinishes(t *testing.T) {
	captureLog(t)
	sys := newFakeSystem()
	sys.blockLock = make(chan struct{})
	a := newTestApp(t, sys)
	rc := &recordingClient{}
	a.subscribeCommands(rc)
	handler := rc.subscribed["win2mqtt/h/command/#"]

	returned := make(chan struct{})
	go func() {
		handler(rc, fakeMessage{topic: "win2mqtt/h/command/lock", payload: "lock"})
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("paho message handler blocked while the command ran")
	}
	close(sys.blockLock)
	waitFor(t, func() bool { return sys.callCount() == 1 })
}

// waitFor polls cond for up to two seconds.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

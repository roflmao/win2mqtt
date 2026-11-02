package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const topicRoot = "win2mqtt"

// publishTimeout is how long an outstanding publish is given before its
// failure is logged. Waiting happens off the update loop, so a stalled broker
// write never delays other sensors.
var publishTimeout = 5 * time.Second

// commandQueueSize bounds commands waiting for the single command worker.
const commandQueueSize = 32

// app wires the config, the operating system and the MQTT client together.
type app struct {
	cfg           *config
	sys           System
	hostname      string
	prefix        string // topicRoot + "/" + hostname
	idleThreshold int
	warned        sync.Map // sensor name -> struct{}; warnings already logged once
	net           networkStats
	commands      chan queuedCommand
	workerOnce    sync.Once
}

type queuedCommand struct {
	client  mqtt.Client
	topic   string
	payload string
}

func newApp(cfg *config, sys System, hostname string) *app {
	return &app{
		cfg:           cfg,
		sys:           sys,
		hostname:      hostname,
		prefix:        topicRoot + "/" + hostname,
		idleThreshold: cfg.getIdleThreshold(),
	}
}

// publish sends a QoS 0 message and waits for it, logging in debug or dry-run mode.
func (a *app) publish(client mqtt.Client, topic string, retained bool, payload string) {
	if a.cfg.DryRun || a.cfg.Debug {
		prefix := "[DEBUG]"
		if a.cfg.DryRun {
			prefix = "[DRY-RUN]"
		}
		log.Printf("%s Publishing to topic '%s': %s (Retained=%v)", prefix, topic, payload, retained)
	}

	token := client.Publish(topic, 0, retained, payload)
	// QoS 0: nothing to wait for on success. Report failures without holding
	// up the caller, so one stalled write cannot block the whole update cycle.
	go func() {
		if !token.WaitTimeout(publishTimeout) {
			log.Printf("Publish to %s timed out after %v", topic, publishTimeout)
			return
		}
		if err := token.Error(); err != nil {
			log.Printf("Error publishing to %s: %v", topic, err)
		}
	}()
}

// warnOnce logs the first failure of a sensor; later failures only appear in debug mode.
func (a *app) warnOnce(key string, err error) {
	if _, seen := a.warned.LoadOrStore(key, struct{}{}); !seen {
		log.Printf("Warning: %s: %v", key, err)
		return
	}
	if a.cfg.Debug {
		log.Printf("[DEBUG] %s: %v", key, err)
	}
}

func (a *app) onConnect(client mqtt.Client) {
	log.Println("Connected to MQTT")

	a.publish(client, a.prefix+"/status/alive", true, "true")

	a.publishDiscoveryMessages(client)

	a.updateFast(client)
	a.updateSlow(client)

	a.subscribeCommands(client)
}

// startCommandWorker runs commands one at a time, in arrival order, off the
// Paho callback: commands can take seconds (audio) or suspend the machine.
func (a *app) startCommandWorker() {
	a.workerOnce.Do(func() {
		a.commands = make(chan queuedCommand, commandQueueSize)
		go func() {
			for cmd := range a.commands {
				a.handleCommand(cmd.client, cmd.topic, cmd.payload)
			}
		}()
	})
}

func (a *app) subscribeCommands(client mqtt.Client) {
	a.startCommandWorker()

	topic := a.prefix + "/command/#"
	token := client.Subscribe(topic, 0, func(c mqtt.Client, m mqtt.Message) {
		// A retained command would replay (e.g. shut the PC down) on every reconnect.
		if m.Retained() {
			log.Printf("Ignoring retained command %s", m.Topic())
			return
		}
		select {
		case a.commands <- queuedCommand{client: c, topic: m.Topic(), payload: string(m.Payload())}:
		default:
			log.Printf("Command queue full, dropping %s", m.Topic())
		}
	})
	token.Wait()
	if err := token.Error(); err != nil {
		log.Printf("Error subscribing to %s: %v", topic, err)
	}
}

func (a *app) onConnectionLost(client mqtt.Client, err error) {
	log.Printf("Disconnected from MQTT: %v", err)
}

// newMQTTClient connects to the broker, retrying forever, or returns a dummy in dry-run mode.
func (a *app) newMQTTClient() mqtt.Client {
	if a.cfg.DryRun {
		log.Println("Dry-run mode: simulating MQTT connection")
		client := &dummyClient{}
		a.onConnect(client)
		return client
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%s", a.cfg.IP, a.cfg.Port))
	opts.SetUsername(a.cfg.User)
	opts.SetPassword(a.cfg.Password)
	opts.OnConnect = a.onConnect
	opts.OnConnectionLost = a.onConnectionLost
	opts.SetWill(a.prefix+"/status/alive", "false", 0, true)
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(60 * time.Second)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetConnectRetry(true)

	client := mqtt.NewClient(opts)

	retryDelay := 2 * time.Second
	const maxRetryDelay = 60 * time.Second
	for attempt := 1; ; attempt++ {
		log.Printf("Connecting to MQTT broker at %s:%s (attempt %d)", a.cfg.IP, a.cfg.Port, attempt)
		token := client.Connect()
		token.Wait()
		if token.Error() == nil {
			return client
		}
		log.Printf("Failed to connect to MQTT: %v. Retrying in %v", token.Error(), retryDelay)
		time.Sleep(retryDelay)
		retryDelay *= 2
		if retryDelay > maxRetryDelay {
			retryDelay = maxRetryDelay
		}
	}
}

// dummyToken is a no-op token for dry-run mode.
type dummyToken struct{}

func (t *dummyToken) Wait() bool                     { return true }
func (t *dummyToken) WaitTimeout(time.Duration) bool { return true }
func (t *dummyToken) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
func (t *dummyToken) Error() error { return nil }

// dummyClient is a no-op MQTT client for dry-run mode.
type dummyClient struct{}

func (c *dummyClient) IsConnected() bool       { return true }
func (c *dummyClient) IsConnectionOpen() bool  { return true }
func (c *dummyClient) Connect() mqtt.Token     { return &dummyToken{} }
func (c *dummyClient) Disconnect(quiesce uint) {}
func (c *dummyClient) Publish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token {
	return &dummyToken{}
}
func (c *dummyClient) Subscribe(topic string, qos byte, callback mqtt.MessageHandler) mqtt.Token {
	return &dummyToken{}
}
func (c *dummyClient) SubscribeMultiple(filters map[string]byte, callback mqtt.MessageHandler) mqtt.Token {
	return &dummyToken{}
}
func (c *dummyClient) Unsubscribe(topics ...string) mqtt.Token             { return &dummyToken{} }
func (c *dummyClient) AddRoute(topic string, callback mqtt.MessageHandler) {}
func (c *dummyClient) OptionsReader() mqtt.ClientOptionsReader {
	return mqtt.ClientOptionsReader{}
}

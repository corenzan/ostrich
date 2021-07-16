package main

import (
	"encoding/json"
	"io"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
)

const (
	catchUpReply = "ostrich/catch-up/reply"
	catchUpReq   = "ostrich/catch-up/request"
)

// Envelope wraps a message with a sender and the destination channel.
type Envelope struct {
	Sender    *Client
	Recipient *Channel
	Message   *Message
}

// Message encodes/decodes exchanged messages.
type Message struct {
	Type    string                 `json:"type"`
	Meta    map[string]interface{} `json:"meta,omitempty"`
	Payload interface{}            `json:"payload,omitempty"`
}

// -
// -
// -

// Client wraps a socket connection.
type Client struct {
	Conn *websocket.Conn
}

// Write writes given message to the client's underlying connection.
func (c *Client) Write(m *Message) error {
	if m.Meta == nil {
		m.Meta = map[string]interface{}{}
	}
	m.Meta["remote"] = true

	return c.Conn.WriteJSON(m)
}

// Read reads JSON messages from the client's underlying connection.
func (c *Client) Read() (*Message, error) {
	var m *Message

	for {
		err := c.Conn.ReadJSON(m)
		if err != nil {
			if err == io.ErrUnexpectedEOF {
				continue
			} else if _, ok := err.(*json.UnmarshalTypeError); ok {
				continue
			} else if _, ok := err.(*json.SyntaxError); ok {
				continue
			}
			return nil, err
		}
		return m, nil
	}
}

// Closes closes underlying connection.
func (c *Client) Close() error {
	return c.Conn.Close()
}

// -
// -
// -

// Channel ...
type Channel struct {
	Name    string
	Clients map[*Client]struct{}
	Stale   map[*Client]struct{}
}

// Deliverable tells if given envelope should be delivered to given recipient.
func (ch *Channel) Deliverable(recipient *Client, envelope *Envelope) bool {
	if recipient == envelope.Sender {
		return false
	}
	if _, stale := ch.Stale[recipient]; stale {
		return envelope.Message.Type != catchUpReply
	}
	return envelope.Message.Type == catchUpReply
}

// Broadcast given envelope to the whole channel.
func (ch *Channel) Broadcast(envelope *Envelope) error {
	for client := range ch.Clients {
		if ch.Deliverable(client, envelope) {
			continue
		}

		if err := client.Write(envelope.Message); err != nil {
			return err
		}
		delete(ch.Stale, client)

		// Halts after delivering one catch-up request.
		if envelope.Message.Type == catchUpReq {
			break
		}
	}
	return nil
}

// Client registers and returns a new client in the channel.
func (ch *Channel) Client(conn *websocket.Conn) *Client {
	cl := &Client{conn}

	ch.Clients[cl] = struct{}{}
	ch.Stale[cl] = struct{}{}

	return cl
}

// Count ...
func (ch *Channel) Count() int {
	return len(ch.Clients)
}

// Unregister drops given client.
func (ch *Channel) Unregister(cl *Client) {
	delete(ch.Clients, cl)
	delete(ch.Stale, cl)
}

// -
// -
// -

// Broker manages the channels and handles the massaging.
type Broker struct {
	Channels map[string]*Channel
	Dispatch chan *Envelope
}

// Channel registers and returns a new channel.
func (b *Broker) Channel(name string) *Channel {
	if channel, ok := b.Channels[name]; ok {
		return channel
	}
	channel := &Channel{
		Name:    name,
		Clients: make(map[*Client]struct{}),
		Stale:   make(map[*Client]struct{}),
	}
	b.Channels[name] = channel
	return channel
}

// Register handles incoming connections.
func (b *Broker) Register(conn *websocket.Conn) (*Channel, *Client) {
	ch := b.Channel(conn.Params("+1"))
	cl := ch.Client(conn)

	b.Dispatch <- b.Envelop(nil, ch, nil) // TODO

	return ch, cl
}

// Envelop ...
func (b *Broker) Envelop(cl *Client, ch *Channel, m *Message) *Envelope {
	return &Envelope{cl, ch, m}
}

// Unregister drops given client.
func (b *Broker) Unregister(ch *Channel, cl *Client) {
	ch.Unregister(cl)

	if ch.Count() < 1 {
		delete(b.Channels, ch.Name)
	}
}

// Listen listens to dispatch requests.
func (b *Broker) Listen() {
	for {
		e := <-b.Dispatch
		e.Recipient.Broadcast(e)
	}
}

// -
// -
// -

func main() {
	f := fiber.New()

	f.Use(func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return c.SendStatus(fiber.StatusUpgradeRequired)
	})

	b := &Broker{
		Channels: make(map[string]*Channel),
	}

	go b.Listen()

	f.Get("/+", websocket.New(func(conn *websocket.Conn) {
		ch, cl := b.Register(conn)

		defer b.Unregister(ch, cl)

		for {
			m, err := cl.Read()

			if err != nil {
				break
			}

			b.Dispatch <- b.Envelop(cl, ch, m)
		}
	}))

	f.Listen(":" + os.Getenv("PORT"))
}

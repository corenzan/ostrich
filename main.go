package main

import (
	"encoding/json"
	"io"
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
)

const (
	messageTypeSyncReply   = "ostrich/sync/reply"
	messageTypeSyncRequest = "ostrich/sync/request"
)

type message struct {
	Type    string                 `json:"type"`
	Meta    map[string]interface{} `json:"meta,omitempty"`
	Payload interface{}            `json:"payload,omitempty"`
}

type client struct {
	channel string
	id      string
	conn    *websocket.Conn
}

type envelope struct {
	sender  client
	message message
}

type broker struct {
	clients   map[string]map[client]struct{}
	stale     map[string]map[client]struct{}
	broadcast chan envelope
}

func (b *broker) client(conn *websocket.Conn) client {
	c := client{
		channel: conn.Params("+1"),
		conn:    conn,
	}

	if b.clients[c.channel] == nil {
		b.clients[c.channel] = map[client]struct{}{
			c: {},
		}
		b.stale[c.channel] = map[client]struct{}{}
	} else {
		b.broadcast <- envelope{
			sender:  client{c.channel, "", nil},
			message: message{messageTypeSyncRequest, nil, nil},
		}
		b.clients[c.channel][c] = struct{}{}
		b.stale[c.channel][c] = struct{}{}
	}
	return c
}

func (b *broker) drop(c client) {
	c.conn.Close()

	delete(b.clients[c.channel], c)
	delete(b.stale[c.channel], c)

	if l := len(b.clients[c.channel]); l == 0 {
		delete(b.clients, c.channel)
		delete(b.stale, c.channel)
	} else if l == len(b.stale[c.channel]) {
		for c := range b.stale[c.channel] {
			delete(b.stale[c.channel], c)
			break
		}
	}
}

func (b *broker) listen() {
	for {
		e := <-b.broadcast

		if e.message.Meta == nil {
			e.message.Meta = map[string]interface{}{}
		}
		e.message.Meta["remote"] = true

		for c := range b.clients[e.sender.channel] {
			if c == e.sender {
				continue
			}

			_, stale := b.stale[e.sender.channel][c]

			if stale {
				if e.message.Type != messageTypeSyncReply {
					continue
				}
			} else {
				if e.message.Type == messageTypeSyncReply {
					continue
				}
			}

			if err := c.conn.WriteJSON(e.message); err == nil {
				delete(b.stale[c.channel], c)
			} else {
				log.Println("failed to send message:", err)
			}

			if e.message.Type == messageTypeSyncRequest {
				break
			}
		}
	}
}

func main() {
	app := fiber.New()

	app.Use(func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return c.SendStatus(fiber.StatusUpgradeRequired)
	})

	b := &broker{
		clients:   make(map[string]map[client]struct{}),
		stale:     make(map[string]map[client]struct{}),
		broadcast: make(chan envelope),
	}

	go b.listen()

	app.Get("/+", websocket.New(func(conn *websocket.Conn) {
		c := b.client(conn)
		defer b.drop(c)

		for {
			var m message

			if err := conn.ReadJSON(&m); err != nil {
				log.Println("failed to read message:", err)

				if err == io.ErrUnexpectedEOF {
					continue
				} else if _, ok := err.(*json.UnmarshalTypeError); ok {
					continue
				} else if _, ok := err.(*json.SyntaxError); ok {
					continue
				}

				return
			}
			log.Println("message received:", m)

			b.broadcast <- envelope{
				sender:  c,
				message: m,
			}
		}
	}))

	app.Listen(":" + os.Getenv("PORT"))
}

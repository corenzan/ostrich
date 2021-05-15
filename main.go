package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
)

const (
	messageTypeSyncReply   = "ostrich/sync/reply"
	messageTypeSyncRequest = "ostrich/sync/request"
)

func newId() string {
	return fmt.Sprintf("%x", rand.Uint32())
}

type message struct {
	Type    string                 `json:"type"`
	Meta    map[string]interface{} `json:"meta"`
	Payload interface{}            `json:"payload"`
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
	clients   map[string]map[string]client
	synced    map[string]map[string]struct{}
	broadcast chan envelope
}

func (b *broker) client(conn *websocket.Conn) client {
	c := client{
		channel: conn.Params("+1"),
		id:      newId(),
		conn:    conn,
	}
	if b.clients[c.channel] == nil {
		b.clients[c.channel] = map[string]client{}
		b.synced[c.channel] = map[string]struct{}{
			c.id: {},
		}
	} else {
		b.broadcast <- envelope{
			sender:  client{c.channel, "", nil},
			message: message{messageTypeSyncRequest, nil, nil},
		}
	}
	b.clients[c.channel][c.id] = c
	return c
}

func (b *broker) drop(c client) {
	c.conn.Close()

	delete(b.clients[c.channel], c.id)
	delete(b.synced[c.channel], c.id)

	if len(b.clients[c.channel]) == 0 {
		delete(b.clients, c.channel)
		delete(b.synced, c.channel)
	} else if len(b.synced[c.channel]) == 0 {
		for id := range b.clients[c.channel] {
			b.synced[c.channel][id] = struct{}{}
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

		for id, c := range b.clients[e.sender.channel] {
			if id == e.sender.id {
				continue
			}

			_, synced := b.synced[e.sender.channel][id]

			if synced {
				if e.message.Type == messageTypeSyncReply {
					continue
				}
			} else {
				if e.message.Type != messageTypeSyncReply {
					continue
				}
			}

			if err := c.conn.WriteJSON(e.message); err == nil {
				b.synced[e.sender.channel][id] = struct{}{}
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
		clients:   make(map[string]map[string]client),
		synced:    make(map[string]map[string]struct{}),
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

package main

import (
	"encoding/json"
	"io"
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/websocket/v2"
	"github.com/rs/xid"
)

type message struct {
	Type    string                 `json:"type"`
	Meta    map[string]interface{} `json:"meta"`
	Payload interface{}            `json:"payload"`
}

type client struct {
	channel string
	id      string
}

type envelope struct {
	message message
	client  client
}

type broker struct {
	clients   map[string]map[string]*websocket.Conn
	synced    map[string]map[string]struct{}
	broadcast chan envelope
}

func (b *broker) newClient(conn *websocket.Conn) *client {
	c := &client{
		channel: conn.Params("+1"),
		id:      xid.New().String(),
	}
	if b.clients[c.channel] == nil {
		b.clients[c.channel] = map[string]*websocket.Conn{}
		b.synced[c.channel] = map[string]struct{}{
			c.id: {},
		}
	} else {
		b.broadcast <- envelope{message{syncReqType, nil, nil}, client{c.channel, ""}}
	}
	b.clients[c.channel][c.id] = conn
	return c
}

func (b *broker) dropClient(c *client) {
	b.clients[c.channel][c.id].Close()

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

const syncRepType = "ostrich/sync/reply"
const syncReqType = "ostrich/sync/request"

func (b *broker) listen() {
	for {
		e := <-b.broadcast

		if e.message.Meta == nil {
			e.message.Meta = map[string]interface{}{}
		}
		e.message.Meta["remote"] = true

		for id, conn := range b.clients[e.client.channel] {
			if id == e.client.id {
				continue
			}

			_, synced := b.synced[e.client.channel][id]

			if synced {
				if e.message.Type == syncRepType {
					continue
				}
			} else {
				if e.message.Type != syncRepType {
					continue
				}
			}

			if err := conn.WriteJSON(e.message); err == nil {
				b.synced[e.client.channel][id] = struct{}{}
			} else {
				log.Println("failed to send message:", err)
			}

			if e.message.Type == syncReqType {
				break
			}
		}
	}
}

func main() {
	app := fiber.New()

	app.Use(logger.New())

	app.Use(func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return c.SendStatus(fiber.StatusUpgradeRequired)
	})

	b := &broker{
		clients:   make(map[string]map[string]*websocket.Conn),
		synced:    make(map[string]map[string]struct{}),
		broadcast: make(chan envelope),
	}

	go b.listen()

	app.Get("/+", websocket.New(func(conn *websocket.Conn) {
		c := b.newClient(conn)

		defer b.dropClient(c)

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

			b.broadcast <- envelope{m, *c}
		}
	}))

	app.Listen(":" + os.Getenv("PORT"))
}

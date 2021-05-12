package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/websocket/v2"
	"github.com/rs/xid"
)

type message struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
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
	registry  map[string]map[string]*websocket.Conn
	stale     map[string]map[string]struct{}
	broadcast chan envelope
}

func (b *broker) newClient(conn *websocket.Conn) *client {
	c := &client{
		channel: conn.Params("+1"),
		id:      xid.New().String(),
	}
	if b.registry[c.channel] == nil {
		b.registry[c.channel] = map[string]*websocket.Conn{}
		b.stale[c.channel] = map[string]struct{}{}
	} else {
		b.stale[c.channel][c.id] = struct{}{}
	}
	b.registry[c.channel][c.id] = conn
	return c
}

func (b *broker) dropClient(c *client) {
	b.registry[c.channel][c.id].Close()

	delete(b.registry[c.channel], c.id)
	delete(b.stale[c.channel], c.id)

	if len(b.registry[c.channel]) == 0 {
		delete(b.registry, c.channel)
		delete(b.stale, c.channel)
	}

	log.Printf("%+v", b)
}

const syncMessageType = "ostrich/sync"

func (b *broker) listen() {
	for {
		e := <-b.broadcast

		sync := e.message.Type == syncMessageType

		for id, conn := range b.registry[e.client.channel] {
			_, stale := b.stale[e.client.channel][id]

			if id == e.client.id {
				continue
			}

			if stale && !sync {
				continue
			}

			if err := conn.WriteJSON(e.message); err == nil {
				delete(b.stale[e.client.channel], id)
			} else {
				log.Println("failed to send message:", err)
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
		registry:  make(map[string]map[string]*websocket.Conn),
		stale:     make(map[string]map[string]struct{}),
		broadcast: make(chan envelope),
	}

	go b.listen()

	app.Get("/+", websocket.New(func(conn *websocket.Conn) {
		c := b.newClient(conn)

		defer b.dropClient(c)

		var m message

		for {
			if err := conn.ReadJSON(&m); err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Println("failed to read message:", err)
					return
				}
				continue
			}
			log.Println("message received:", m)

			b.broadcast <- envelope{m, *c}
		}

	}))

	app.Listen(":" + os.Getenv("PORT"))
}

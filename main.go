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
	Protocol  string            `json:"protocol"`
	Namespace string            `json:"namespace"`
	Operation string            `json:"operation"`
	Meta      map[string]string `json:"meta"`
	Payload   interface{}       `json:"payload"`
}

type namespace map[string]*websocket.Conn

type broker struct {
	clients  map[string]namespace
	dispatch chan message
}

func (b *broker) push(conn *websocket.Conn) (string, string) {
	ns := conn.Params("+1")
	id := xid.New().String()
	if b.clients[ns] == nil {
		b.clients[ns] = map[string]*websocket.Conn{}
	}
	b.clients[ns][id] = conn
	return ns, id
}

func (b *broker) drop(ns, id string) {
	b.clients[ns][id].Close()

	delete(b.clients[ns], id)

	if len(b.clients[ns]) == 0 {
		delete(b.clients, ns)
	}
}

func (b *broker) listen() {
	for {
		msg := <-b.dispatch

		if recipient, ok := msg.Meta["recipient"]; ok {
			if conn, ok := b.clients[msg.Namespace][recipient]; ok {
				if err := conn.WriteJSON(msg); err != nil {
					log.Println("failed to send message:", err)
				}
			}
		} else {
			for id, conn := range b.clients[msg.Namespace] {
				if id != msg.Meta["sender"] {
					if err := conn.WriteJSON(msg); err != nil {
						log.Println("failed to send message:", err)
					}
				}
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
		clients:  make(map[string]namespace),
		dispatch: make(chan message),
	}

	go b.listen()

	app.Get("/+", websocket.New(func(conn *websocket.Conn) {
		ns, id := b.push(conn)

		defer b.drop(ns, id)

		var msg message

		for {
			if err := conn.ReadJSON(&msg); err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Println("failed to read message:", err)
				}
				return
			}
			msg.Namespace = ns
			msg.Meta["sender"] = id
			log.Println("message received:", msg)
			b.dispatch <- msg
		}
	}))

	app.Listen(":" + os.Getenv("PORT"))
}

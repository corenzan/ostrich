package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/etag"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/logger"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

func exists(db *pgxpool.Pool, path string) (bool, error) {
	sql := `SELECT 1 FROM documents WHERE path = $1;`
	if err := db.QueryRow(context.Background(), sql, path).Scan(nil); err != pgx.ErrNoRows {
		if err == nil {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

func create(db *pgxpool.Pool, path string, contents interface{}) error {
	sql := `INSERT INTO documents (path, contents) VALUES ($1, $2);`
	if _, err := db.Exec(context.Background(), sql, path, contents); err != nil {
		return err
	}
	return nil
}

func main() {
	db, err := pgxpool.Connect(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	app := fiber.New()

	app.Use(logger.New())

	app.Use(limiter.New(limiter.Config{
		Max:      100,
		Duration: 60 * time.Second,
	}))

	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
	}))

	app.Use(etag.New())

	app.Put("/d/+", func(c *fiber.Ctx) error {
		c.Type("json", "utf-8")

		var contents interface{}
		if err := c.BodyParser(&contents); err != nil {
			return c.Status(http.StatusBadRequest).Send(nil)
		}

		if conflict, err := exists(db, c.Params("+")); err != nil {
			return err
		} else if conflict {
			return c.Status(http.StatusConflict).Send(nil)
		}

		if err := create(db, c.Params("+"), contents); err != nil {
			return err
		}

		return c.Status(http.StatusCreated).JSON(contents)
	})

	app.Listen(":" + os.Getenv("PORT"))
}

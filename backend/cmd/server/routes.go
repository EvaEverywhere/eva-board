package main

import "github.com/gofiber/fiber/v2"

func registerPublicRoutes(app *fiber.App) {
	app.Get("/hello", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"message": "hello world"})
	})
}

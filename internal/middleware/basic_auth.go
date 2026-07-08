package middleware

import "github.com/gin-gonic/gin"

// PlaygroundBasicAuth returns a middleware that protects playground routes
// with HTTP Basic Authentication. The browser will prompt for credentials
// before serving any embedded Svelte assets.
func PlaygroundBasicAuth(user, pass string) gin.HandlerFunc {
	return gin.BasicAuth(gin.Accounts{
		user: pass,
	})
}

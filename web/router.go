package web

import "github.com/gin-gonic/gin"

func InitRouter() *gin.Engine {
	r := gin.New()

	r.Use(gin.Logger())

	r.Use(gin.Recovery())

	r.POST("/login", Login)

	apiV1 := r.Group("/api/v1")
	apiV1.Use(JWT())
	{
		apiV1.POST("/config/update", UpdateConfig)
	}
	return r
}

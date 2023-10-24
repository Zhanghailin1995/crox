package crox

import (
	"crox/pkg/logging"
	"crox/web"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

type WebServerBootstrap struct {
}

func initRouter() *gin.Engine {
	r := gin.New()

	r.Use(gin.Logger())

	r.Use(gin.Recovery())

	r.POST("/login", Login)

	apiV1 := r.Group("/api/v1")
	apiV1.Use(web.JWT())
	{
		apiV1.POST("/config/update", UpdateConfig)
		apiV1.POST("/config/addClient", AddClient)
		apiV1.POST("/config/addProxy", AddProxy)
		apiV1.POST("/config/deleteProxy", DeleteProxy)
	}
	return r
}

type LoginForm struct {
	Username string `form:"username" json:"username" binding:"required"`
	Password string `form:"password" json:"password" binding:"required"`
}

func Login(c *gin.Context) {
	var form LoginForm
	if err := c.ShouldBind(&form); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}
	token, err := web.GenerateToken(form.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": web.SUCCESS,
		"data": token,
	})
}

func UpdateConfig(c *gin.Context) {
	P().UpdateConfig()
	c.JSON(http.StatusOK, gin.H{
		"code": web.SUCCESS,
		"msg":  "success",
	})
}

type AddClientPostBody struct {
	ClientId   string `json:"clientId"`
	ClientName string `json:"clientName"`
}

func AddClient(c *gin.Context) {
	var body AddClientPostBody
	err := c.ShouldBindJSON(&body)
	if err != nil {
		logging.Errorf("AddClient error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"code": web.INVALID_PARAMS,
			"msg":  err.Error(),
		})
		return
	}

	err = P().AddClient(body.ClientId, body.ClientName)
	if err != nil {
		logging.Errorf("AddClient error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": web.SYSTEM_ERROR,
			"msg":  err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": web.SUCCESS,
		"msg":  "success",
	})
}

type AddProxyPostBody struct {
	ClientId string `json:"clientId"`
	InetPort int    `json:"inetPort"`
	Lan      string `json:"lan"`
	Name     string `json:"name"`
}

func AddProxy(c *gin.Context) {
	var body AddProxyPostBody
	err := c.ShouldBindJSON(&body)
	if err != nil {
		logging.Errorf("AddProxy error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"code": web.INVALID_PARAMS,
			"msg":  err.Error(),
		})
		return
	}

	err = P().AddProxy(body.ClientId, &ProxyMapping{InetPort: body.InetPort, Lan: body.Lan, Name: body.Name})
	if err != nil {
		logging.Errorf("AddProxy error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": web.SYSTEM_ERROR,
			"msg":  err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": web.SUCCESS,
		"msg":  "success",
	})
}

type DeleteProxyPostBody struct {
	ClientId string `json:"clientId"`
	InetPort int    `json:"inetPort"`
}

func DeleteProxy(c *gin.Context) {
	var body DeleteProxyPostBody
	err := c.ShouldBindJSON(&body)
	if err != nil {
		logging.Errorf("DeleteProxy error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"code": web.INVALID_PARAMS,
			"msg":  err.Error(),
		})
		return
	}

	err = P().DeleteProxy(body.ClientId, body.InetPort)
	if err != nil {
		logging.Errorf("DeleteProxy error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": web.SYSTEM_ERROR,
			"msg":  err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": web.SUCCESS,
		"msg":  "success",
	})
}

func (boot *WebServerBootstrap) Boot() {
	routersInit := initRouter()
	readTimeout := 60 * time.Second
	writeTimeout := 60 * time.Second
	endPoint := fmt.Sprintf(":%d", 7999)
	maxHeaderBytes := 1 << 20

	server := &http.Server{
		Addr:           endPoint,
		Handler:        routersInit,
		ReadTimeout:    readTimeout,
		WriteTimeout:   writeTimeout,
		MaxHeaderBytes: maxHeaderBytes,
	}

	logging.Infof("start http server listening %s", endPoint)
	err := server.ListenAndServe()
	if err != nil {
		logging.Errorf("listen and serve error: %s", err.Error())
	}
}

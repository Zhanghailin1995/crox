package web

import (
	"crox"
	"github.com/gin-gonic/gin"
	"net/http"
)

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
	token, err := GenerateToken(form.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": SUCCESS,
		"data": token,
	})
}

func UpdateConfig(c *gin.Context) {
	crox.P().UpdateConfig()
	c.JSON(http.StatusOK, gin.H{
		"code": SUCCESS,
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
		c.JSON(http.StatusBadRequest, gin.H{
			"code": INVALID_PARAMS,
			"msg":  err.Error(),
		})
		return
	}

	err = crox.P().AddClient(body.ClientId, body.ClientName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": SYSTEM_ERROR,
			"msg":  err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": SUCCESS,
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
		c.JSON(http.StatusBadRequest, gin.H{
			"code": INVALID_PARAMS,
			"msg":  err.Error(),
		})
		return
	}

	err = crox.P().AddProxy(body.ClientId, &crox.ProxyMapping{InetPort: body.InetPort, Lan: body.Lan, Name: body.Name})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": SYSTEM_ERROR,
			"msg":  err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": SUCCESS,
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
		c.JSON(http.StatusBadRequest, gin.H{
			"code": INVALID_PARAMS,
			"msg":  err.Error(),
		})
		return
	}

	err = crox.P().DeleteProxy(body.ClientId, body.InetPort)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": SYSTEM_ERROR,
			"msg":  err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": SUCCESS,
		"msg":  "success",
	})
}

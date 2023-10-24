package crox

import (
	"crox/pkg/logging"
	"crox/web"
	"fmt"
	"net/http"
	"time"
)

type WebServerBootstrap struct {
}

func (boot *WebServerBootstrap) Boot() {
	routersInit := web.InitRouter()
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

package main

import (
	"log"
	"net/http"
	"time"
)

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		log.Println("[WEB]", request.RemoteAddr, request.RequestURI)
		next.ServeHTTP(writer, request)
	})
}

func profilingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		startTime := time.Now()
		next.ServeHTTP(writer, request)
		endTime := time.Now()
		timeTakenMs := (endTime.UnixNano() - startTime.UnixNano()) / 1000
		log.Println("[PROFILE]", "took", timeTakenMs, "ms")
	})
}

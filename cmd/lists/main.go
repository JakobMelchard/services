// Command lists serves the lists app on loopback with the UI embedded.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/JakobMelchard/services/internal/lists/handler"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8082", "listen address")
	data := flag.String("data", ".data", "data directory, lists under <data>/lists")
	flag.Parse()
	if err := serve(*addr, *data); err != nil {
		log.Fatal(err)
	}
}

// serve runs the app on addr until SIGINT or SIGTERM; in-flight requests get
// 5s to finish. Loopback only: a reverse proxy fronts this on the tailnet.
func serve(addr, data string) error {
	app, err := handler.New(data, time.Now)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	srv := &http.Server{Handler: app, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute, BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		grace, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(grace); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()
	// Logs the bound address, not the requested one: scripts pass :0 and read it from here.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("lists on %s, data in %s", ln.Addr(), data)
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-done
	return nil
}

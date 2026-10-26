package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	m "github.com/zahidhasann88/meshlink/pkg/meshlink"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	n, e := m.NewNode(m.Config{ListenAddress: "127.0.0.1:0"})
	if e != nil {
		panic(e)
	}
	if e = n.Start(ctx); e != nil {
		panic(e)
	}
	defer n.Close()
	fmt.Println(n.ID(), n.Address())
	<-ctx.Done()
}

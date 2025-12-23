package netconf_test

import (
	"context"
	"log"
	"time"

	"golang.org/x/crypto/ssh"
	"nemith.io/netconf"
	"nemith.io/netconf/rpc"
	ncssh "nemith.io/netconf/transport/ssh"
)

const sshAddr = "myrouter.example.com:830"

func Example_ssh() {
	config := &ssh.ClientConfig{
		User: "admin",
		Auth: []ssh.AuthMethod{
			ssh.Password("secret"),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	transport, err := ncssh.Dial(ctx, "tcp", sshAddr, config)
	if err != nil {
		panic(err)
	}
	defer transport.Close() // nolint:errcheck

	session, err := netconf.Open(transport)
	if err != nil {
		panic(err)
	}

	// timeout for the call itself.
	ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	deviceConfig, err := rpc.GetConfig{Source: rpc.Running}.Exec(ctx, session)
	if err != nil {
		log.Fatalf("failed to get config: %v", err)
	}

	log.Printf("Config:\n%s\n", deviceConfig)

	if err := session.Close(context.Background()); err != nil {
		log.Print(err)
	}
}

package main

import (
	"context"
	"github.com/nemith/netconf"
	"golang.org/x/crypto/ssh"
	"log"
	"time"
)

func main() {
	config := &ssh.ClientConfig{
		User: "admin",
		Auth: []ssh.AuthMethod{
			ssh.Password("secret"),
		},
		// as specified in rfc8071 3.1 C5 netconf client must validate host keys
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	addr := "0.0.0.0:4334"
	ch, err := netconf.NewCallHome(netconf.WithConfigSSH(config), netconf.WithAddress(addr))
	if err != nil {
		panic(err)
	}
	log.Printf("callhome server listening on: %s", addr)
	err = ch.Listen()
	if err != nil {
		panic(err)
	}
	session := ch.Session()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deviceConfig, err := session.GetConfig(ctx, "running")
	if err != nil {
		log.Fatalf("failed to get config: %v", err)
	}

	log.Printf("Config:\n%s\n", deviceConfig)

	if err := session.Close(context.Background()); err != nil {
		log.Print(err)
	}
}

package main

import (
	"context"
	"github.com/nemith/netconf"
	"golang.org/x/crypto/ssh"
	"log"
	"time"
)

func main() {
	chc := &netconf.CallHomeClient{
		Transport: &netconf.SSHCallHomeTransport{
			Config: &ssh.ClientConfig{
				User: "test",
				Auth: []ssh.AuthMethod{
					ssh.Password("test"),
				},
				// as specified in rfc8071 3.1 C5 netconf client must validate host keys
				HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			},
		},
		Address: "192.168.121.230",
	}

	chs, err := netconf.NewCallHomeServer(netconf.WithCallHomeClient(chc), netconf.WithAddress("0.0.0.0:4339"))
	if err != nil {
		panic(err)
	}
	log.Printf("callhome server listening on: %s", "0.0.0.0:4339")
	go func() {
		err := chs.Listen()
		if err != nil {
			panic(err)
		}
	}()
	time.Sleep(10 * time.Second)
	session, err := chs.ClientSession("192.168.121.230")
	if err != nil {
		panic(err)
	}

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

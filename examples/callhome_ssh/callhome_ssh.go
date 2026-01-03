package main

import (
	"context"
	"fmt"
	"github.com/nemith/netconf"
	"golang.org/x/crypto/ssh"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	sigChannel := make(chan os.Signal, 1)
	signal.Notify(sigChannel, os.Interrupt, syscall.SIGTERM)

	chcList := []*netconf.CallHomeClientConfig{
		{
			Transport: &netconf.SSHCallHomeTransport{
				Config: &ssh.ClientConfig{
					User: "foo",
					Auth: []ssh.AuthMethod{
						ssh.Password("bar"),
					},
					// as specified in rfc8071 3.1 C5 netconf client must validate host keys
					HostKeyCallback: ssh.InsecureIgnoreHostKey(),
				},
			},
			Address: "192.168.121.17",
		},
	}

	chs, err := netconf.NewCallHomeServer(netconf.WithCallHomeClientConfig(chcList...), netconf.WithAddress("0.0.0.0:4339"))
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

	go func() {
		for {
			select {
			case e := <-chs.ErrorChannel():
				fmt.Println(e.Error())
			case chc := <-chs.CallHomeClientChannel():
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				deviceConfig, err := chc.Session().GetConfig(ctx, "running")
				cancel()
				if err != nil {
					log.Fatalf("failed to get config: %v", err)
				}
				log.Printf("Config:\n%s\n", deviceConfig)
			}
		}
	}()
	select {
	case <-sigChannel:
		if err := chs.Close(); err != nil {
			log.Print(err)
		}
		os.Exit(0)
	}
}

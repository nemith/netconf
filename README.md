# Go `netconf` client library

[![Go Reference](https://pkg.go.dev/badge/nemith.io/netconf.svg)](https://pkg.go.dev/nemith.io/netconf)
[![Report Card](https://goreportcard.com/badge/nemith.io/netconf)](https://goreportcard.com/report/nemith.io/netconf)
[![Validate](https://github.com/nemith/netconf/actions/workflows/validate.yaml/badge.svg?branch=main&event=push)](https://github.com/nemith/netconf/actions/workflows/validate.yaml)
[![coverage](https://raw.githubusercontent.com/nemith/netconf/coverage/badge.svg)](http://htmlpreview.github.io/?https://github.com/nemith/netconf/blob/coverage/coverage.html)

A performant and complete implementation of the NETCONF network device management protocol in Go.

Like Go itself, only the latest two Go versions are tested and supported (Go 1.23 or Go 1.24).

NOTICE: This library has a pretty stable API however pre-1.0.0 means that there may be some minor renames and changes to the API on the road to stabilization. Release notes should include the changes required. Once 1.0.0 gets released the API will only change in major versions.

## Installation

```bash
go get nemith.io/netconf
```

## Quick Start

### SSH Connection with get-config

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"golang.org/x/crypto/ssh"
	"nemith.io/netconf"
	"nemith.io/netconf/rpc"
	ncssh "nemith.io/netconf/transport/ssh"
)

func main() {
	// Configure SSH client
	config := &ssh.ClientConfig{
		User: "admin",
		Auth: []ssh.AuthMethod{
			ssh.Password("secret"),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // Don't use in production
	}

	// Connect with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	transport, err := ncssh.Dial(ctx, "tcp", "router.example.com:830", config)
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer transport.Close()

	// Create NETCONF session
	session, err := netconf.NewSession(transport)
	if err != nil {
		log.Fatalf("failed to create session: %v", err)
	}
	defer session.Close(context.Background())

	// Get the running configuration
	cfg, err := rpc.GetConfig{Source: rpc.Running}.Exec(ctx, session)
	if err != nil {
		log.Fatalf("get-config failed: %v", err)
	}

	fmt.Printf("Running config:\n%s\n", cfg)
}
```

### NETCONF Notifications 

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"golang.org/x/crypto/ssh"
	"nemith.io/netconf"
	"nemith.io/netconf/rpc"
	ncssh "nemith.io/netconf/transport/ssh"
)

func main() {
	config := &ssh.ClientConfig{
		User:            "admin",
		Auth:            []ssh.AuthMethod{ssh.Password("secret")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	ctx := context.Background()

	transport, err := ncssh.Dial(ctx, "tcp", "router.example.com:830", config)
	if err != nil {
		log.Fatal(err)
	}
	defer transport.Close()

	// Create session with notification handler
	session, err := netconf.NewSession(transport,
		netconf.WithNotifHandlerFunc(func(ctx context.Context, msg *netconf.Message) {
			defer msg.Close()

			var notif netconf.Notification
			if err := msg.Decode(&notif); err != nil {
				log.Printf("failed to decode notification: %v", err)
				return
			}

			fmt.Printf("[%s] Notification received\n", notif.EventTime.Format(time.RFC3339))
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer session.Close(ctx)

	// Subscribe to notifications
	if err := rpc.CreateSubscription{}.Exec(ctx, session); err != nil {
		log.Fatalf("failed to subscribe: %v", err)
	}

	fmt.Println("Subscribed to notifications. Press Ctrl+C to exit.")

	// Keep the program running to receive notifications
	select {}
}
```

## RFC Support

| RFC                                                                               | Support                      |
| --------------------------------------------------------------------------------- | ---------------------------- |
| [RFC6241 Network Configuration Protocol (NETCONF)][RFC6241]                       | :white_check_mark: supported |
| [RFC6242 Using the NETCONF Protocol over Secure Shell (SSH)][RFC6242]             | :white_check_mark: supported |
| [RFC7589 Using the NETCONF Protocol over Transport Layer Security (TLS)][RFC7589] | :white_check_mark: supported |
| [RFC5277 NETCONF Event Notifications][RFC5277]                                    | :white_check_mark: supported |
| [RFC5717 Partial Lock Remote Procedure Call (RPC) for NETCONF][RFC5717]           | :white_check_mark: supported |
| [RFC8071 NETCONF Call Home and RESTCONF Call Home][RFC8071]                       | :bulb: planned               |
| [RFC6243 With-defaults Capability for NETCONF][RFC6243]                           | :white_check_mark: supported |
| [RFC4743 Using NETCONF over the Simple Object Access Protocol (SOAP)][RFC4743]    | :x: not planned              |
| [RFC4744 Using the NETCONF Protocol over the BEEP][RFC4744]                       | :x: not planned              |

There are other RFC around YANG integration that will be looked at later.

[RFC4743]: https://www.rfc-editor.org/rfc/rfc4743.html
[RFC4744]: https://www.rfc-editor.org/rfc/rfc4744.html
[RFC5277]: https://www.rfc-editor.org/rfc/rfc5277.html
[RFC5717]: https://www.rfc-editor.org/rfc/rfc5717.html
[RFC6241]: https://www.rfc-editor.org/rfc/rfc6241.html
[RFC6242]: https://www.rfc-editor.org/rfc/rfc6242.html
[RFC6243]: https://www.rfc-editor.org/rfc/rfc6243.html
[RFC7589]: https://www.rfc-editor.org/rfc/rfc7589.html
[RFC8071]: https://www.rfc-editor.org/rfc/rfc8071.html

See [TODO.md](TODO.md) for a list of what is left to implement these features.

## Comparison

### Differences from [`github.com/juniper/go-netconf/netconf`](https://pkg.go.dev/github.com/Juniper/go-netconf)

- **Much cleaner, idomatic API, less dumb** I, @nemith, was the original creator of the netconf package and it was my very first Go project and it shows. There are number of questionable API design, code, and a lot of odd un tested bugs. Really this rewrite was created to fix this.
- **No impled vendor ownership** Moving the project out of the `Juniper` organization allowes better control over the project, less implied support (or lack there of), and hopefully more contributions.
- **Transports are implemented in their own packages.** This means if you are not using SSH or TLS you don't need to bring in the underlying depdendencies into your binary.
- **Stream based transports.** Should mean less memory usage and much less allocation bringing overall performance higher.

### Differences from [`github.com/scrapli/scrapligo/driver/netconf`](https://pkg.go.dev/github.com/scrapli/scrapligo/driver/netconf)

Scrapligo driver is quite good and way better than the original juniper project. However this new package concentrates more on RFC correctness and implementing some of the more advanced RFC features like call-home and event notifications. If there is a desire there could be some callaboration with scrapligo in the future.

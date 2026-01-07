//go:build inttest
// +build inttest

package inttest

import (
	"context"
	"encoding/xml"
	"errors"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/carlmjohnson/be"
	"golang.org/x/crypto/ssh"
	"nemith.io/netconf"
	"nemith.io/netconf/rpc"
	ncssh "nemith.io/netconf/transport/ssh"
)

func onlyFlavor(t *testing.T, flavors ...string) {
	t.Helper()
	for _, flavor := range flavors {
		if os.Getenv("NETCONF_DUT_FLAVOR") == flavor {
			return
		}
	}
	t.Skipf("test only for flavors '%s'.  Skipping", strings.Join(flavors, ","))
}

func sshAuth(t *testing.T) ssh.AuthMethod {
	t.Helper()

	switch {
	case os.Getenv("NETCONF_DUT_SSHPASS") != "":
		return ssh.Password(os.Getenv("NETCONF_DUT_SSHPASS"))
	case os.Getenv("NETCONF_DUT_SSHKEYFILE") != "":
		keyFile := os.Getenv("NETCONF_DUT_SSHKEYFILE")
		key, err := os.ReadFile(keyFile)
		be.NilErr(t, err)

		signer, err := ssh.ParsePrivateKey(key)
		be.NilErr(t, err)
		return ssh.PublicKeys(signer)
	}
	t.Fatal("NETCONF_DUT_SSHADDR tests require NETCONF_DUT_SSHPASS or NETCONF_DUT_SSHKEYFILE")
	return nil
}

func setupSSH(t *testing.T) *netconf.Session {
	t.Helper()

	host := os.Getenv("NETCONF_DUT_SSHHOST")
	if host == "" {
		t.Skip("NETCONF_DUT_SSHHOST not set, skipping test")
	}

	port := os.Getenv("NETCONF_DUT_SSHPORT")
	if port == "" {
		port = "830"
	}

	user := os.Getenv("NETCONF_DUT_SSHUSER")
	if user == "" {
		t.Fatal("NETCONF_DUT_SSHADDR set but NETCONF_DUT_SSHUSER is not set")
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{sshAuth(t)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	addr := net.JoinHostPort(host, port)
	t.Logf("connecting to %s", addr)

	ctx := context.Background()
	tr, err := ncssh.Dial(ctx, "tcp", addr, config)
	be.NilErr(t, err)

	// capture the framed communication
	inCap := newLogWriter("S: ", t)
	outCap := newLogWriter("C: ", t)

	tr.DebugCapture(inCap, outCap)

	session, err := netconf.NewSession(tr)
	be.NilErr(t, err)
	return session
}

func TestSSHDial(t *testing.T) {
	session := setupSSH(t)
	be.Nonzero(t, session.SessionID())
	be.Nonzero(t, session.ServerCaps().Len())
	err := session.Close(context.Background())
	be.NilErr(t, err)
}

func TestSSHGetConfig(t *testing.T) {
	session := setupSSH(t)

	ctx := context.Background()
	config, err := rpc.GetConfig{Source: rpc.Running}.Exec(ctx, session)
	be.NilErr(t, err)
	t.Logf("configuration: %s", config)

	_ = session.Close(ctx)
	// TODO: investigate why this fails on some devices
	//be.NilErr(t, err)
}

func TestBadGetConfig(t *testing.T) {
	session := setupSSH(t)

	ctx := context.Background()
	cfg, err := rpc.GetConfig{Source: "non-exist"}.Exec(ctx, session)
	be.Zero(t, cfg)
	var rpcErr netconf.RPCError
	be.True(t, errors.As(err, &rpcErr))
}

func TestJunosCommand(t *testing.T) {
	onlyFlavor(t, "junos")
	session := setupSSH(t)

	cmd := struct {
		XMLName xml.Name `xml:"command"`
		Command string   `xml:",innerxml"`
	}{
		Command: "show version",
	}

	var reply struct {
		netconf.RPCReply
		Result string `xml:"command-output>result"`
	}

	ctx := context.Background()
	err := session.Exec(ctx, &cmd, &reply)
	be.NilErr(t, err)
	be.Equal(t, 0, len(reply.RPCErrors))
}

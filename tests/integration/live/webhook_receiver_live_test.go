//go:build live

package live_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"testing"
)

// newContainerReachableReceiver starts an HTTP receiver the Bitbucket instance
// can actually deliver to, and returns the URL to register with it.
//
// httptest.NewServer is not usable for this. It binds loopback and reports a
// 127.0.0.1 URL, and inside the container that address is the container itself
// -- so a webhook registered with it is delivered to nothing, and a test that
// only logs the outcome passes without ever having tested delivery. Both of
// this suite's delivery tests did exactly that, in different ways.
func newContainerReachableReceiver(t *testing.T, handler http.HandlerFunc) (*httptest.Server, string) {
	t.Helper()

	listener, err := net.Listen("tcp", webhookReceiverAddress())
	if err != nil {
		t.Fatalf("listen for the instance to deliver to: %v", err)
	}

	receiver := &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: handler},
	}
	receiver.Start()
	t.Cleanup(receiver.Close)

	// The instance runs in a container, so it reaches the host by name rather
	// than by the address the listener reports. docker/compose.yml maps the
	// name; see webhookReceiverAddress for which side of it binds where.
	return receiver, "http://host.docker.internal:" + portOf(t, listener)
}

func portOf(t *testing.T, listener net.Listener) string {
	t.Helper()

	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("expected a TCP listener, got %T", listener.Addr())
	}

	return strconv.Itoa(address.Port)
}

// webhookReceiverAddress is where a receiver listens for the instance's
// delivery, and it differs by what supplies host.docker.internal.
//
// Docker Desktop -- Windows and macOS -- proxies that name, and the connection
// arrives on the host's loopback, so binding loopback is enough. Docker Engine
// resolves it to the bridge gateway through the extra_hosts entry in
// docker/compose.yml, and the connection arrives on the bridge interface, which
// a loopback socket will not accept.
//
// The distinction earns its branch: binding beyond loopback is what makes
// Windows Defender prompt, and it prompts per executable path -- with a git
// worktree per branch, that is a fresh prompt for every worktree that ever runs
// this suite. Nothing prompts on the CI runners, so they bind wide.
func webhookReceiverAddress() string {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return "127.0.0.1:0"
	}

	return "0.0.0.0:0"
}

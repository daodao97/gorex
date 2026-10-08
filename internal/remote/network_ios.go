//go:build ios && cgo

package remote

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation
#include <stdlib.h>
void gorex_network_start(long long token, const char *url);
void gorex_network_cancel(long long token);
*/
import "C"

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/tailscale/tailcat"
)

var networkPrimed atomic.Bool
var networkRequests struct {
	sync.Mutex
	next    int64
	pending map[int64]chan error
}

// A Foundation request lets iOS establish the app's network access before
// Go opens raw sockets. It also uses the device's DNS64/VPN resolver.
func prepareNetwork(ctx context.Context, addr tailcat.Addr) error {
	if networkPrimed.Load() {
		return nil
	}
	endpoint := tailcat.DefaultDERPMapURL
	if ci, err := tailcat.ParseAddr(addr); err == nil {
		for _, region := range ci.Region {
			for _, node := range region.Nodes {
				if node.STUNOnly || node.HostName == "" || node.CertName != "" || node.InsecureForTests {
					continue
				}
				host := node.HostName
				if node.DERPPort != 0 {
					host = net.JoinHostPort(host, strconv.Itoa(node.DERPPort))
				}
				endpoint = (&url.URL{Scheme: "https", Host: host, Path: "/"}).String()
				break
			}
			if endpoint != tailcat.DefaultDERPMapURL {
				break
			}
		}
	}
	ch := make(chan error, 1)
	networkRequests.Lock()
	networkRequests.next++
	token := networkRequests.next
	if networkRequests.pending == nil {
		networkRequests.pending = make(map[int64]chan error)
	}
	networkRequests.pending[token] = ch
	networkRequests.Unlock()
	text := C.CString(endpoint)
	C.gorex_network_start(C.longlong(token), text)
	C.free(unsafe.Pointer(text))
	select {
	case err := <-ch:
		if err == nil {
			networkPrimed.Store(true)
		}
		return err
	case <-ctx.Done():
		networkRequests.Lock()
		delete(networkRequests.pending, token)
		networkRequests.Unlock()
		C.gorex_network_cancel(C.longlong(token))
		return ctx.Err()
	}
}

//export gorexNetworkResult
func gorexNetworkResult(token C.longlong, message *C.char) {
	networkRequests.Lock()
	ch := networkRequests.pending[int64(token)]
	delete(networkRequests.pending, int64(token))
	networkRequests.Unlock()
	if ch != nil {
		var err error
		if message != nil {
			err = fmt.Errorf("手机网络不可用：%s", C.GoString(message))
		}
		ch <- err
	}
}

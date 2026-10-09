package remote

import (
	"context"
	"net"
	"net/url"
	"strconv"

	"github.com/tailscale/tailcat"
)

// A Foundation request lets iOS establish the app's network access before
// Go opens raw sockets. It also uses the device's DNS64/VPN resolver. MyGo
// returns at once on desktops and once a request has succeeded.
func prepareNetwork(ctx context.Context, addr tailcat.Addr) error {
	return prepareSystemNetwork(ctx, derpURL(addr))
}

// derpURL is the first public relay of the link's region map, the server the
// tunnel connects to first.
func derpURL(addr tailcat.Addr) string {
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
				return (&url.URL{Scheme: "https", Host: host, Path: "/"}).String()
			}
		}
	}
	return tailcat.DefaultDERPMapURL
}

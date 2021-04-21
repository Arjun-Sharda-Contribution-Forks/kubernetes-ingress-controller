//+build integration_tests

package integration

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kongv1alpha1 "github.com/kong/kubernetes-ingress-controller/railgun/apis/configuration/v1alpha1"
	"github.com/kong/kubernetes-ingress-controller/railgun/pkg/clientset"
)

func TestMinimalUDPIngress(t *testing.T) {
	// TODO: once KIC 2.0 lands and pre v2 is gone, we can remove this check
	if useLegacyKIC() {
		t.Skip("legacy KIC does not support UDPIngress, skipping")
	}

	// test setup
	namespace := "default"
	testName := "minudp"
	ctx, cancel := context.WithTimeout(context.Background(), ingressWait)
	defer cancel()

	// build a kong kubernetes clientset
	c, err := clientset.NewForConfig(cluster.Config())
	assert.NoError(t, err)

	// configure a net.Resolver that will go through our proxy
	p := proxyReady()
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: time.Second * 5,
			}
			return d.DialContext(ctx, network, fmt.Sprintf("%s:9999", p.ProxyUDPUrl.Hostname()))
		},
	}

	// create the UDPIngress record
	udp := &kongv1alpha1.UDPIngress{
		ObjectMeta: metav1.ObjectMeta{
			Name: testName,
			Annotations: map[string]string{
				"kubernetes.io/ingress.class": "kong",
			},
		},
		Spec: kongv1alpha1.UDPIngressSpec{
			Host:       "9.9.9.9",
			ListenPort: 9999,
			TargetPort: 53,
		},
	}
	udp, err = c.ConfigurationV1alpha1().UDPIngresses(namespace).Create(ctx, udp, metav1.CreateOptions{})
	assert.NoError(t, err)

	// ensure cleanup of the UDPIngress
	defer func() {
		if err := c.ConfigurationV1alpha1().UDPIngresses(namespace).Delete(ctx, udp.Name, metav1.DeleteOptions{}); err != nil {
			if !errors.IsNotFound(err) {
				require.NoError(t, err)
			}
		}
	}()

	// ensure that we can eventually make a successful DNS request through the proxy
	assert.Eventually(t, func() bool {
		_, err := resolver.LookupHost(ctx, "kernel.org")
		if err != nil {
			return false
		}
		return true
	}, ingressWait, waitTick)

	// cleanup and ensure the UDP ingress is cleaned up
	assert.NoError(t, c.ConfigurationV1alpha1().UDPIngresses(namespace).Delete(ctx, udp.Name, metav1.DeleteOptions{}))
	assert.Eventually(t, func() bool {
		_, err := resolver.LookupHost(ctx, "kernel.org")
		if err != nil {
			if strings.Contains(err.Error(), "i/o timeout") {
				return true
			}
		}
		return false
	}, ingressWait, waitTick)
}
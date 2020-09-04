package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	wh "kube-failover-webhook/webhook"

	"k8s.io/klog"
)

func main() {
	var port int
	var certFile string
	var keyFile string

	flag.IntVar(&port, "port", 8443, "kube-failover webhook server port")
	flag.StringVar(&certFile, "tlsCertFile", "/etc/webhook/certs/cert.pem", "x509 Certificate file for TLS connection")
	flag.StringVar(&keyFile, "tlsKeyFile", "/etc/webhook/certs/key.pem", "x509 Private key file for TLS connection")
	flag.Parse()

	keyPair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		klog.Errorf("Failed to load key pair: %s", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", wh.HandleMutate)

	webhookServer := &http.Server{
		Addr:      fmt.Sprintf(":%d", port),
		Handler:   mux,
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{keyPair}},
	}

	klog.Info("Starting kube-failover webhook server...")

	go func() {
		if err := webhookServer.ListenAndServeTLS("", ""); err != nil {
			klog.Errorf("Failed to listen and serve webhook server: %s", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	klog.Info("OS shutdown signal received...")
	webhookServer.Shutdown(context.Background())
}

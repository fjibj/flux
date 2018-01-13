package client

import (
	"fmt"

	"k8s.io/client-go/kubernetes"

	"k8s.io/helm/pkg/helm"
)

// Helm client
//		setup configuration
type Client struct {
	KubeClient *kubernetes.Clientset
	HelmClient *helm.Client
	Host       string
}

type Options struct {
	IP   string
	Port string
}

// New creates a new helm client
func New(kubeClient *kubernetes.Clientset, opts Options) (*Client, error) {
	port := "44134"
	var ip string
	var host string

	if opts.IP != "" {
		ip = opts.IP
	}
	if opts.Port != "" {
		port = opts.Port
	}
	host = fmt.Sprintf("%s:%s", ip, port)

	hcl := &Client{
		KubeClient: kubeClient,
		HelmClient: helm.NewClient(helm.Host(host)),
		Host:       host,
	}

	return hcl, nil
}

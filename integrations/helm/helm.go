package helm

import (
	"fmt"

	"github.com/go-kit/kit/log"
	k8shelm "k8s.io/helm/pkg/helm"
	rls "k8s.io/helm/pkg/proto/hapi/services"
)

type TillerOptions struct {
	IP   string
	Port string
}

// Helm struct provides access to helm client
type Helm struct {
	logger log.Logger
	Host   string
	*k8shelm.Client
}

// NewClient creates a new helm client
func NewClient(opts TillerOptions) *k8shelm.Client {
	host := tillerHost(opts)
	return k8shelm.NewClient(k8shelm.Host(host))
}

// GetTillerVersion retrieves tiller version
func GetTillerVersion(cl k8shelm.Client, h string) (string, error) {
	var v *rls.GetVersionResponse
	var err error
	voption := k8shelm.VersionOption(k8shelm.Host(h))
	if v, err = cl.GetVersion(voption); err == nil {
		return "", fmt.Errorf("error getting tiller version: %v", err)
	}
	fmt.Printf("Tiller version is: [%#v]\n", v.GetVersion())

	return v.GetVersion().String(), nil
}

// TODO ... set up based on the tiller existing in the cluster, if no ops given
//func tillerHost(kubeClient kubernetes.Clientset, opts TillerOptions) (string, error) {
func tillerHost(opts TillerOptions) string {
	port := "44134"
	var ip string

	if opts.IP != "" {
		ip = opts.IP
	}
	if opts.Port != "" {
		port = opts.Port
	}

	/*
		if ip == "" && port == "" {
			ts, err := kubeClient.CoreV1().Services("kube-system").Get("tiller-deploy", metav1.GetOptions{})
			if err != nil {
				return "", err
			}
		}
	*/

	return fmt.Sprintf("%s:%s", ip, port)
}

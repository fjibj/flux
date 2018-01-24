package helm

import (
	"fmt"

	"github.com/go-kit/kit/log"
	k8shelm "k8s.io/helm/pkg/helm"
	rls "k8s.io/helm/pkg/proto/hapi/services"
)

type Options struct {
	IP   string
	Port string
}

// Helm struct provides access to helm client
type Helm struct {
	Logger log.Logger
	Host   string
	*k8shelm.Client
}

// New creates a new helm client
func New(logger log.Logger, opts Options) *Helm {
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
	cl := k8shelm.NewClient(k8shelm.Host(host))

	return &Helm{
		Logger: log.With(logger, "component", "helm"),
		Host:   host,
		Client: cl,
	}
}

// GetTillerVersion retrieves tiller version
func (helm Helm) GetTillerVersion() (string, error) {
	var v *rls.GetVersionResponse
	var err error
	voption := k8shelm.VersionOption(k8shelm.Host(helm.Host))
	if v, err = helm.Client.GetVersion(voption); err == nil {
		helm.Logger.Log("error", err)
		return "", fmt.Errorf("error getting tiller version: %v", err)
	}

	helm.Logger.Log("info", fmt.Sprintf("Tiller version is: [%#v]\n", v.GetVersion()))

	return v.GetVersion().String(), nil
}

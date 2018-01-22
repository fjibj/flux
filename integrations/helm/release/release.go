package release

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/helm/pkg/helm"

	hapi_release "k8s.io/helm/pkg/proto/hapi/release"

	"github.com/go-kit/kit/log"
	ifv1 "github.com/weaveworks/flux/apis/integrations.flux/v1"
	ifclientset "github.com/weaveworks/flux/integrations/client/clientset/versioned"
)

// Release contains clients needed to provide functionality related to helm releases
type Release struct {
	logger     log.Logger
	KubeClient *kubernetes.Clientset
	IfClient   *ifclientset.Clientset // client for integration.flux, ie for custom resources
	HelmClient *helm.Client
	TillerHost string
}

type options struct {
	ip   string
	port string
}

// New creates a new helm client
func New(logger log.Logger, kubeClient *kubernetes.Clientset, ifClient *ifclientset.Clientset, opts options) (*Release, error) {
	port := "44134"
	var ip string

	var host string
	if opts.ip != "" {
		ip = opts.ip
	}
	if opts.port != "" {
		port = opts.port
	}
	host = fmt.Sprintf("%s:%s", ip, port)

	r := &Release{
		logger:     log.With(logger, "component", "release"),
		KubeClient: kubeClient,
		IfClient:   ifClient,
		HelmClient: helm.NewClient(helm.Host(host)),
		TillerHost: host,
	}

	return r, nil
}

// Exists detects if a particular Chart release exists
func (release Release) Exists(fhr ifv1.FluxHelmResource) bool {
	namespace := fhr.Namespace
	if namespace == "" {
		namespace = "default"
	}
	releaseName := fhr.Spec.ReleaseName
	if releaseName == "" {
		releaseName = fmt.Sprintf("%s-%s", namespace, fhr.Name)
	}

	//opts := helm.ReleaseListOption{}
	//opts := helm.ReleaseListNamespace(namespace)
	//rls := release.HelmClient.ListReleases().
	return false
}

// Create installs a Chart
func (release Release) Create(fhr ifv1.FluxHelmResource) (hapi_release.Release, error) {
	return hapi_release.Release{}, nil
}

// Update updates Chart release
func (release Release) Update(current hapi_release.Release) (hapi_release.Release, error) {
	return hapi_release.Release{}, nil
}

// Delete deletes Chart release
func (release Release) Delete() error {
	return nil
}

// GetAll provides Chart releases (stored in tiller ConfigMaps)
func (release Release) GetAll() ([]*hapi_release.Release, error) {
	response, err := release.HelmClient.ListReleases()
	if err != nil {
		return nil, release.logger.Log("error", err)
	}
	fmt.Printf("Number of helm releases is %d\n", response.GetCount())

	for i, r := range response.GetReleases() {
		fmt.Printf("\t==> %d : %#v\n\n\t\t\tin namespace %#v\n\n\t\tChartMetadata: %v\n\n\n", i, r.Name, r.Namespace, r.GetChart().GetMetadata())
	}

	return response.GetReleases(), nil
}

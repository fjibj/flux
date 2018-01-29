package release

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"path/filepath"
	"regexp"
	"sync"

	//	"k8s.io/client-go/kubernetes"

	"k8s.io/helm/pkg/chartutil"
	k8shelm "k8s.io/helm/pkg/helm"
	hapi_release "k8s.io/helm/pkg/proto/hapi/release"

	"github.com/go-kit/kit/log"
	ifv1 "github.com/weaveworks/flux/apis/integrations.flux/v1"
	//ifclientset "github.com/weaveworks/flux/integrations/client/clientset/versioned"
)

var (
	ErrChartGitPathMissing = "Chart deploy configuration (%q) has empty Chart git path"
)

// Release contains clients needed to provide functionality related to helm releases
type Release struct {
	logger log.Logger
	//KubeClient *kubernetes.Clientset
	//IfClient   *ifclientset.Clientset // client for integration.flux, ie for custom resources
	HelmClient *k8shelm.Client
	sync.RWMutex
}

// New creates a new Release instance
func New(logger log.Logger, helmClient *k8shelm.Client) *Release {
	r := &Release{
		logger: log.With(logger, "component", "release"),
		//KubeClient: kubeClient,
		//IfClient:   ifClient,
		HelmClient: helmClient,
	}

	return r
}

// GetReleaseName either retrieves the release name from the Custom Resource or constructs a new one
//  in the form : $Namespace-$CustomResourceName
func GetReleaseName(fhr ifv1.FluxHelmResource) string {
	namespace := fhr.Namespace
	if namespace == "" {
		namespace = "default"
	}
	releaseName := fhr.Spec.ReleaseName
	if releaseName == "" {
		releaseName = fmt.Sprintf("%s-%s", namespace, fhr.Name)
	}
	return releaseName
}

// Exists detects if a particular Chart release exists
func (r *Release) Exists(name string) (bool, error) {
	rls, err := r.HelmClient.ReleaseContent(name)
	if err != nil {
		r.logger.Log("error", fmt.Sprintf("%#v", err))
		return false, err
	}
	/*
		"UNKNOWN":          0,
		"DEPLOYED":         1,
		"DELETED":          2,
		"SUPERSEDED":       3,
		"FAILED":           4,
		"DELETING":         5,
		"PENDING_INSTALL":  6,
		"PENDING_UPGRADE":  7,
		"PENDING_ROLLBACK": 8,
	*/
	rst := rls.Release.Info.Status.GetCode()
	if rst != 1 {
		r.logger.Log("error", fmt.Sprintf("Release (%q) status: %#v", name, rst.String()))
		return false, nil
	}
	return true, nil
}

// Create installs a Chart
func (r *Release) Create(fhr ifv1.FluxHelmResource) (hapi_release.Release, error) {
	r.Lock()
	defer r.Unlock()

	chartPath := fhr.Spec.ChartGitPath
	if chartPath == "" {
		r.logger.Log("error")
		return hapi_release.Release{}, fmt.Errorf(ErrChartGitPathMissing, fhr.GetName())
	}

	namespace := fhr.GetNamespace()
	if namespace == "" {
		namespace = "default"
	}
	fhrName := fhr.GetName()
	releaseName := fmt.Sprintf("%s-%s", namespace, fhrName)

	// set up the git repo:
	//		clone - or do just once? ...
	//    go to the repo root
	//		-----
	//		checkout the latest changes

	// load the chart to turn it into a Chart object
	chart, err := chartutil.Load(filepath.Join(chartPath))
	if err != nil {
		return hapi_release.Release{}, fmt.Errorf("Chart release failed: %q: %#v", chartPath, err)
	}

	// Set up values
	rawVals, err := collectValues(fhr.Spec.Customizations)
	if err != nil {
		return hapi_release.Release{}, err
	}

	// Install the Chart
	res, err := r.HelmClient.InstallReleaseFromChart(
		chart,
		namespace,
		k8shelm.ValueOverrides(rawVals),
		k8shelm.ReleaseName(releaseName),
	//		helm.InstallDryRun(i.dryRun),
	//		helm.InstallReuseName(i.replace),
	//		helm.InstallDisableHooks(i.disableHooks),
	//		helm.InstallTimeout(i.timeout),
	//		helm.InstallWait(i.wait)
	)

	if err != nil {
		return hapi_release.Release{}, err
	}

	return *res.Release, nil
}

// Update updates Chart release
func (r *Release) Update(current hapi_release.Release) (hapi_release.Release, error) {
	return hapi_release.Release{}, nil
}

// Delete deletes Chart release
func (r *Release) Delete(name string) error {
	r.Lock()
	defer r.Unlock()

	res, err := r.HelmClient.DeleteRelease(name)
	fmt.Printf("Tiller delete response: %#v\n\n", res)
	if err != nil {
		fmt.Printf("ERROR Tiller delete response: %#v\n\n", err)
		notFound, _ := regexp.MatchString("not found", err.Error())
		if notFound {
			fmt.Println("NOT FOUND")
			return nil
		}
		return err
	}
	r.logger.Log("info", fmt.Sprintf("Release deleted: %q", res.Info))
	return nil
}

// GetAll provides Chart releases (stored in tiller ConfigMaps)
func (r *Release) GetAll() ([]*hapi_release.Release, error) {
	response, err := r.HelmClient.ListReleases()
	if err != nil {
		return nil, r.logger.Log("error", err)
	}
	fmt.Printf("Number of helm releases is %d\n", response.GetCount())

	for i, r := range response.GetReleases() {
		fmt.Printf("\t==> %d : %#v\n\n\t\t\tin namespace %#v\n\n\t\tChartMetadata: %v\n\n\n", i, r.Name, r.Namespace, r.GetChart().GetMetadata())
	}

	return response.GetReleases(), nil
}

func collectValues(params []ifv1.HelmChartParam) ([]byte, error) {
	customValues := []byte{}
	if params == nil || len(params) == 0 {
		return customValues, nil
	}

	customValuesMap := make(map[string]interface{})
	for _, v := range params {
		customValuesMap[v.Name] = v.Value
	}
	b := new(bytes.Buffer)
	encoder := gob.NewEncoder(b)
	if err := encoder.Encode(customValuesMap); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

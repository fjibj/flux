package release

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	//	"k8s.io/client-go/kubernetes"

	yaml "gopkg.in/yaml.v2"
	k8shelm "k8s.io/helm/pkg/helm"
	hapi_release "k8s.io/helm/pkg/proto/hapi/release"

	"github.com/go-kit/kit/log"
	ifv1 "github.com/weaveworks/flux/apis/integrations.flux/v1"
	helmgit "github.com/weaveworks/flux/integrations/helm/git"
	//ifclientset "github.com/weaveworks/flux/integrations/client/clientset/versioned"
)

var (
	ErrChartGitPathMissing = "Chart deploy configuration (%s) has empty Chart git path"
)

// ReleaseType determines whether we are making a new Chart release or updating an existing one
type ReleaseType string

// Release contains clients needed to provide functionality related to helm releases
type Release struct {
	logger     log.Logger
	HelmClient *k8shelm.Client
	Repo       repo
	sync.RWMutex
}

type repo struct {
	fhrChange   *helmgit.Checkout
	chartChange *helmgit.Checkout
}

// New creates a new Release instance
func New(logger log.Logger, helmClient *k8shelm.Client, fhrChangeCheckout *helmgit.Checkout, chartChangeCheckout *helmgit.Checkout) *Release {
	repo := repo{
		fhrChange:   fhrChangeCheckout,
		chartChange: chartChangeCheckout,
	}
	r := &Release{
		logger:     log.With(logger, "component", "release"),
		HelmClient: helmClient,
		Repo:       repo,
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
	fmt.Printf("---> release name from fhr.Spec.ReleaseName: %s\n", releaseName)
	if releaseName == "" {
		releaseName = fmt.Sprintf("%s-%s", namespace, fhr.Name)
	}
	fmt.Printf("---> final release name: %s\n", releaseName)

	return releaseName
}

// Get ... detects if a particular Chart release exists
// 		release name must match regex ^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])+$
func (r *Release) Get(name string) (*hapi_release.Release, error) {
	rls, err := r.HelmClient.ReleaseContent(name)

	// TODO: see what errors can be returned
	r.logger.Log("info", fmt.Sprintf("+++ Getting release %s", name))

	if err != nil {
		notFound, _ := regexp.MatchString("not found", err.Error())

		r.logger.Log("info", fmt.Sprintf("+++ Get release (%s) error: error = %v", name, err.Error()))
		r.logger.Log("info", fmt.Sprintf("+++ Get release error: notFound = %v", notFound))

		if notFound {
			r.logger.Log("info", fmt.Sprintf("Release [%s]: %s", name, err.Error()))
			return &hapi_release.Release{}, errors.New("NOT EXISTS")
		}
		r.logger.Log("error", fmt.Sprintf("Release name [%s] : %#v", name, err))
		return &hapi_release.Release{}, err
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
		r.logger.Log("error", fmt.Sprintf("Release [%s] status: %#v", name, rst.String()))
		return &hapi_release.Release{}, fmt.Errorf("Release [%s] already exists with status: %s", name, rst.String())
	}
	return rls.Release, nil
}

// Install ... performs Chart release. Depending on the release type, this is either a new release,
// or an upgrade of an existing one
func (r *Release) Install(releaseName string, fhr ifv1.FluxHelmResource, releaseType ReleaseType) (hapi_release.Release, error) {
	r.Lock()
	defer r.Unlock()

	r.logger.Log("info", fmt.Sprintf("     input: releaseName= %s, releaseType=%s", releaseName, releaseType))

	chartPath := fhr.Spec.ChartGitPath
	if chartPath == "" {
		r.logger.Log("error", fmt.Sprintf(ErrChartGitPathMissing, fhr.GetName()))
		return hapi_release.Release{}, fmt.Errorf(ErrChartGitPathMissing, fhr.GetName())
	}

	namespace := fhr.GetNamespace()
	if namespace == "" {
		namespace = "default"
	}

	err := r.Repo.fhrChange.Pull()
	if err != nil {
		r.logger.Log("error", fmt.Sprintf("Failure to do git pull: %#v", err))
		return hapi_release.Release{}, err
	}

	chartDir := filepath.Join(r.Repo.fhrChange.Dir, chartPath)

	rawVals, err := collectValues(fhr.Spec.Customizations)
	if err != nil {
		r.logger.Log("error", fmt.Sprintf("Problem with supplied customizations for Chart release [%s]: %#v", releaseName, err))
		return hapi_release.Release{}, err
	}

	// INSTALLATION ----------------------------------------------------------------------
	switch releaseType {
	case "CREATE":
		res, err := r.HelmClient.InstallRelease(
			chartDir,
			namespace,
			k8shelm.ValueOverrides(rawVals),
			k8shelm.ReleaseName(releaseName),
			/*
				helm.InstallDryRun(i.dryRun),
				helm.InstallReuseName(i.replace),
				helm.InstallDisableHooks(i.disableHooks),
				helm.InstallTimeout(i.timeout),
				helm.InstallWait(i.wait)
			*/
		)
		if err != nil {
			r.logger.Log("error", fmt.Sprintf("Chart release failed: %s: %#v", releaseName, err))
			return hapi_release.Release{}, err
		}
		return *res.Release, nil
	case "UPDATE":
		res, err := r.HelmClient.UpdateRelease(
			releaseName,
			chartDir,
			k8shelm.UpdateValueOverrides(rawVals),
			/*
				helm.UpgradeDryRun(u.dryRun),
				helm.UpgradeRecreate(u.recreate),
				helm.UpgradeForce(u.force),
				helm.UpgradeDisableHooks(u.disableHooks),
				helm.UpgradeTimeout(u.timeout),
				helm.ResetValues(u.resetValues),
				helm.ReuseValues(u.reuseValues),
				helm.UpgradeWait(u.wait))
			*/

		)
		if err != nil {
			r.logger.Log("error", fmt.Sprintf("Chart upgrade release failed: %s: %#v", releaseName, err))
			return hapi_release.Release{}, err
		}
		return *res.Release, nil
	default:
		r.logger.Log("error", fmt.Sprintf("Incorrect ReleaseType option provided: %#v", releaseType))
		return hapi_release.Release{}, err
	}
}

// Delete ... deletes Chart release
func (r *Release) Delete(name string) error {
	r.Lock()
	defer r.Unlock()

	_, err := r.HelmClient.DeleteRelease(name)

	if err != nil {
		notFound, _ := regexp.MatchString("not found", err.Error())
		if notFound {
			r.logger.Log("info", fmt.Sprintf("Release not found, deletion is a noop: %#v", err))
			return nil
		}
		return err
	}
	r.logger.Log("info", fmt.Sprintf("Release deleted: [%s]", name))
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
	base := map[string]interface{}{}
	if params == nil || len(params) == 0 {
		return yaml.Marshal(base)
	}

	for _, p := range params {
		k := strings.TrimSpace(p.Name)
		k = strings.Trim(k, "\n")
		if k == "" {
			continue
		}
		v := strings.TrimSpace(p.Value)
		v = strings.Trim(v, "\n")
		base[k] = v
	}

	fmt.Printf("Values string slice ... %#v\n\n", base)
	return yaml.Marshal(base)
}

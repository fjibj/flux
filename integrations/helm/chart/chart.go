package chart

import (
	"fmt"

	"k8s.io/helm/pkg/helm"
	helm_env "k8s.io/helm/pkg/helm/environment"
	rls "k8s.io/helm/pkg/proto/hapi/services"

	helmclient "github.com/weaveworks/flux/integrations/helm/client"
)

var (
	hc       helm.Client
	options  []helm.Option
	voption  helm.VersionOption
	settings helm_env.EnvSettings
)

type Chart struct {
	Client helmclient.Client
}

func init() {
	/*
		settings.TillerHost = "10.96.124.128:44134"
		options = []helm.Option{helm.Host(settings.TillerHost)}
	*/
}

// Charts
//		IsChartReleased
func (ch Chart) ChartReleased() bool {
	return false
}

//		DoRelease
//		UpdateRelease
//		DeleteRelease
//

func (ch Chart) GetReleases() {
	var v *rls.GetVersionResponse
	var err error
	voption = helm.VersionOption(helm.Host(ch.Client.Host))
	if v, err = ch.Client.HelmClient.GetVersion(voption); err == nil {
		fmt.Printf("Error: %#v\n", err)
	}
	fmt.Printf("Tiller version is: [%#v]\n", v.GetVersion())

	response, _ := ch.Client.HelmClient.ListReleases()

	fmt.Printf("Number of helm releases is %d\n", response.GetCount())

	for i, r := range response.GetReleases() {
		fmt.Printf("\t==> %d : %#v\n\n\t\t\tin namespace %#v\n\n", i, r.Name, r.Namespace)
	}
}
